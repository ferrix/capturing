/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrav1 "github.com/ferrix/capturing/api/v1beta1"
	"github.com/ferrix/capturing/pkg/bmc"
)

// MachineReconciler reconciles a TuringPiMachine.
type MachineReconciler struct {
	client.Client
	BMCOptions []bmc.Option
	HTTP       *http.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=turingpimachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=turingpimachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=turingpimachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;machines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile handles TuringPiMachine events.
func (r *MachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	machine := &infrav1.TuringPiMachine{}
	if err := r.Get(ctx, req.NamespacedName, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	original := machine.DeepCopy()
	res, err := r.reconcile(ctx, machine)
	if patchErr := patch(ctx, r.Client, original, machine); patchErr != nil {
		return res, fmt.Errorf("patch: %w", patchErr)
	}
	return res, err
}

func (r *MachineReconciler) reconcile(ctx context.Context, machine *infrav1.TuringPiMachine) (ctrl.Result, error) {
	ownerName := ownerMachineName(machine)
	if ownerName == "" {
		if !machine.DeletionTimestamp.IsZero() {
			controllerutil.RemoveFinalizer(machine, infrav1.MachineFinalizer)
		}
		return ctrl.Result{}, nil
	}
	owner := &clusterv1.Machine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: ownerName}, owner); err != nil {
		if apierrors.IsNotFound(err) && !machine.DeletionTimestamp.IsZero() {
			controllerutil.RemoveFinalizer(machine, infrav1.MachineFinalizer)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	cluster := &clusterv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: owner.Spec.ClusterName}, cluster); err != nil {
		return ctrl.Result{}, err
	}
	if isPaused(cluster, machine) {
		setReady(&machine.Status.Conditions, machine.Generation, false, reasonPaused, "reconciliation is paused")
		return ctrl.Result{}, nil
	}

	if cluster.Spec.InfrastructureRef.Name == "" {
		return ctrl.Result{}, nil
	}
	infra := &infrav1.TuringPiCluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: cluster.Spec.InfrastructureRef.Name}, infra); err != nil {
		return ctrl.Result{}, err
	}

	if machine.DeletionTimestamp.IsZero() && !controllerutil.ContainsFinalizer(machine, infrav1.MachineFinalizer) {
		controllerutil.AddFinalizer(machine, infrav1.MachineFinalizer)
	}

	bmcClient, err := bmcClientFor(ctx, r.Client, infra, defaultBMCOptions(r.BMCOptions)...)
	if err != nil {
		setReady(&machine.Status.Conditions, machine.Generation, false, reasonMissingSecret, err.Error())
		return ctrl.Result{}, err
	}

	if !machine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, machine, bmcClient)
	}

	if machine.Spec.Node < 1 || machine.Spec.Node > 4 {
		setReady(&machine.Status.Conditions, machine.Generation, false, reasonInvalidNode, "node must be 1-4")
		return ctrl.Result{}, nil
	}

	conflict, err := r.slotTaken(ctx, machine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if conflict {
		setReady(&machine.Status.Conditions, machine.Generation, false, reasonSlotConflict,
			fmt.Sprintf("node %d is already claimed", machine.Spec.Node))
		return ctrl.Result{}, nil
	}

	provisioned := ptr.Deref(machine.Status.Initialization.Provisioned, false)
	if !provisioned && machine.Spec.ImageURL != "" && !isAdopted(machine) {
		if err := r.flash(ctx, bmcClient, machine); err != nil {
			setReady(&machine.Status.Conditions, machine.Generation, false, reasonFlashFailed, err.Error())
			return ctrl.Result{}, err
		}
	}

	if err := bmcClient.PowerSet(ctx, machine.Spec.Node, true); err != nil {
		setReady(&machine.Status.Conditions, machine.Generation, false, reasonPowerFailed, err.Error())
		return ctrl.Result{}, err
	}

	if machine.Spec.ProviderID == "" {
		machine.Spec.ProviderID = bmc.ProviderID(infra.Spec.BMC.URL, machine.Spec.Node)
	}
	machine.Status.Initialization.Provisioned = ptr.To(true)
	setReady(&machine.Status.Conditions, machine.Generation, true, reasonProvisioned, "node powered on")
	return ctrl.Result{}, nil
}

func (r *MachineReconciler) reconcileDelete(ctx context.Context, machine *infrav1.TuringPiMachine, bmcClient *bmc.HTTPClient) (ctrl.Result, error) {
	if isAdopted(machine) {
		setReady(&machine.Status.Conditions, machine.Generation, false, reasonDeleting, "adopted: skip power off")
		controllerutil.RemoveFinalizer(machine, infrav1.MachineFinalizer)
		return ctrl.Result{}, nil
	}
	setReady(&machine.Status.Conditions, machine.Generation, false, reasonDeleting, "powering off")
	if machine.Spec.Node >= 1 && machine.Spec.Node <= 4 {
		if err := bmcClient.PowerSet(ctx, machine.Spec.Node, false); err != nil {
			setReady(&machine.Status.Conditions, machine.Generation, false, reasonPowerFailed, err.Error())
			return ctrl.Result{}, err
		}
	}
	controllerutil.RemoveFinalizer(machine, infrav1.MachineFinalizer)
	return ctrl.Result{}, nil
}

func (r *MachineReconciler) slotTaken(ctx context.Context, machine *infrav1.TuringPiMachine) (bool, error) {
	list := &infrav1.TuringPiMachineList{}
	if err := r.List(ctx, list, client.InNamespace(machine.Namespace)); err != nil {
		return false, err
	}
	clusterName := machine.Labels[clusterv1.ClusterNameLabel]
	for i := range list.Items {
		other := &list.Items[i]
		if other.Name == machine.Name && other.Namespace == machine.Namespace {
			continue
		}
		if other.DeletionTimestamp != nil && !other.DeletionTimestamp.IsZero() {
			continue
		}
		if other.Spec.Node != machine.Spec.Node {
			continue
		}
		if clusterName != "" && other.Labels[clusterv1.ClusterNameLabel] != clusterName {
			continue
		}
		return true, nil
	}
	return false, nil
}

func (r *MachineReconciler) flash(ctx context.Context, bmcClient *bmc.HTTPClient, machine *infrav1.TuringPiMachine) error {
	httpClient := r.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, machine.Spec.ImageURL, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("image download status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return bmcClient.Flash(ctx, machine.Spec.Node, bytes.NewReader(data), int64(len(data)))
}
