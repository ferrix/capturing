/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrav1 "github.com/ferrix/capturing/api/v1beta1"
	"github.com/ferrix/capturing/pkg/bmc"
)

// ClusterReconciler reconciles a TuringPiCluster.
type ClusterReconciler struct {
	client.Client
	BMCOptions []bmc.Option
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=turingpiclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=turingpiclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=turingpiclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile handles TuringPiCluster events.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cluster := &infrav1.TuringPiCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	original := cluster.DeepCopy()
	res, err := r.reconcile(ctx, cluster)
	if patchErr := patch(ctx, r.Client, original, cluster); patchErr != nil {
		return res, fmt.Errorf("patch: %w", patchErr)
	}
	return res, err
}

func (r *ClusterReconciler) reconcile(ctx context.Context, cluster *infrav1.TuringPiCluster) (ctrl.Result, error) {
	owner, _ := r.ownerCluster(ctx, cluster)
	if isPaused(owner, cluster) {
		setReady(&cluster.Status.Conditions, cluster.Generation, false, reasonPaused, "reconciliation is paused")
		return ctrl.Result{}, nil
	}

	if !cluster.DeletionTimestamp.IsZero() {
		controllerutil.RemoveFinalizer(cluster, infrav1.ClusterFinalizer)
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(cluster, infrav1.ClusterFinalizer) {
		controllerutil.AddFinalizer(cluster, infrav1.ClusterFinalizer)
	}

	if owner != nil && cluster.Spec.ControlPlaneEndpoint.Host == "" && !owner.Spec.ControlPlaneEndpoint.IsZero() {
		cluster.Spec.ControlPlaneEndpoint = owner.Spec.ControlPlaneEndpoint
	}

	client, err := bmcClientFor(ctx, r.Client, cluster, defaultBMCOptions(r.BMCOptions)...)
	if err != nil {
		setReady(&cluster.Status.Conditions, cluster.Generation, false, reasonMissingSecret, err.Error())
		cluster.Status.Initialization.Provisioned = ptr.To(false)
		return ctrl.Result{}, err
	}
	if _, err := client.PowerGet(ctx); err != nil {
		setReady(&cluster.Status.Conditions, cluster.Generation, false, reasonBMCUnreachable, err.Error())
		cluster.Status.Initialization.Provisioned = ptr.To(false)
		return ctrl.Result{}, err
	}
	cluster.Status.Initialization.Provisioned = ptr.To(true)
	setReady(&cluster.Status.Conditions, cluster.Generation, true, reasonBMCReachable, "BMC accepted power status query")
	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) ownerCluster(ctx context.Context, obj *infrav1.TuringPiCluster) (*clusterv1.Cluster, error) {
	name := obj.Labels[clusterv1.ClusterNameLabel]
	if name == "" {
		for _, ref := range obj.OwnerReferences {
			if ref.Kind == "Cluster" {
				name = ref.Name
				break
			}
		}
	}
	if name == "" {
		return nil, nil
	}
	cluster := &clusterv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: obj.Namespace, Name: name}, cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}
