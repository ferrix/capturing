/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrav1 "github.com/ferrix/capturing/api/v1beta1"
	"github.com/ferrix/capturing/pkg/bmc"
)

func TestMachineReconcileNotFound(t *testing.T) {
	r := &MachineReconciler{Client: fakeClient(t)}
	if _, err := r.Reconcile(context.Background(), req("ns", "missing")); err != nil {
		t.Fatal(err)
	}
}

func TestMachinePowerOn(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	infra := infraCluster(ns, "infra", board.URL())
	machine := tpiMachine(ns, "n1", 1)
	c := fakeClient(t, infra, credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiMachine{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "n1"}, got); err != nil {
		t.Fatal(err)
	}
	if !controllerutil.ContainsFinalizer(got, infrav1.MachineFinalizer) {
		t.Fatal("missing finalizer")
	}
	wantID := bmc.ProviderID(board.URL(), 1)
	if got.Spec.ProviderID != wantID {
		t.Fatalf("providerID = %q want %q", got.Spec.ProviderID, wantID)
	}
	if !ptr.Deref(got.Status.Initialization.Provisioned, false) {
		t.Fatal("expected provisioned")
	}
	if !board.Power(1) {
		t.Fatal("node not powered on")
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, clusterv1.ReadyCondition)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("ready = %+v", cond)
	}

	// Idempotent second reconcile.
	sets := board.PowerSetCalls()
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	if board.PowerSetCalls() != sets+1 {
		t.Fatalf("expected another power set, got %d then %d", sets, board.PowerSetCalls())
	}
}

func TestMachineWaitingForOwner(t *testing.T) {
	ns := "default"
	m := tpiMachine(ns, "n1", 1)
	m.OwnerReferences = nil
	c := fakeClient(t, m)
	r := &MachineReconciler{Client: c}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
}

func TestMachineDeleteWithoutOwner(t *testing.T) {
	ns := "default"
	m := tpiMachine(ns, "n1", 1)
	m.OwnerReferences = nil
	m.Finalizers = []string{infrav1.MachineFinalizer}
	now := metav1.Now()
	m.DeletionTimestamp = &now
	c := fakeClient(t, m)
	r := &MachineReconciler{Client: c}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiMachine{}
	err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "n1"}, got)
	if err == nil && controllerutil.ContainsFinalizer(got, infrav1.MachineFinalizer) {
		t.Fatal("finalizer remains")
	}
}

func TestMachinePaused(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	machine := tpiMachine(ns, "n1", 1)
	machine.Annotations = map[string]string{clusterv1.PausedAnnotation: "true"}
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	if board.PowerSetCalls() != 0 {
		t.Fatal("paused machine powered a node")
	}
}

func TestMachineInvalidNode(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	machine := tpiMachine(ns, "n1", 0)
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiMachine{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "n1"}, got)
	cond := meta.FindStatusCondition(got.Status.Conditions, clusterv1.ReadyCondition)
	if cond == nil || cond.Reason != reasonInvalidNode {
		t.Fatalf("cond = %+v", cond)
	}
}

func TestMachineSlotConflict(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	first := tpiMachine(ns, "n1", 2)
	second := tpiMachine(ns, "n2", 2)
	second.OwnerReferences[0].Name = "n2"
	c := fakeClient(t,
		infraCluster(ns, "infra", board.URL()),
		credSecret(ns),
		capiCluster(ns, "infra"),
		capiMachine(ns, "n1"),
		capiMachine(ns, "n2"),
		first,
		second,
	)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), req(ns, "n2")); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiMachine{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "n2"}, got)
	cond := meta.FindStatusCondition(got.Status.Conditions, clusterv1.ReadyCondition)
	if cond == nil || cond.Reason != reasonSlotConflict {
		t.Fatalf("cond = %+v", cond)
	}
	if board.Power(2) && got.Spec.ProviderID != "" {
		t.Fatal("conflicting machine should not provision")
	}
}

func TestMachineFlashThenPower(t *testing.T) {
	board := startBMC(t)
	image := []byte("tiny-talos-img")
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(image)
	}))
	t.Cleanup(imgSrv.Close)

	ns := "default"
	machine := tpiMachine(ns, "n1", 4)
	machine.Spec.ImageURL = imgSrv.URL + "/talos.img"
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	if board.LastFlashSHA(4) != hex.EncodeToString(sum[:]) {
		t.Fatalf("flash sha = %s", board.LastFlashSHA(4))
	}
	if !board.Power(4) {
		t.Fatal("expected power on after flash")
	}

	flashes := board.FlashCalls()
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	if board.FlashCalls() != flashes {
		t.Fatal("must not re-flash a provisioned machine")
	}
}

func TestMachineFlashDownloadFailure(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	machine := tpiMachine(ns, "n1", 1)
	machine.Spec.ImageURL = "http://127.0.0.1:1/nope.img"
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err == nil {
		t.Fatal("expected download error")
	}
	got := &infrav1.TuringPiMachine{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "n1"}, got)
	cond := meta.FindStatusCondition(got.Status.Conditions, clusterv1.ReadyCondition)
	if cond == nil || cond.Reason != reasonFlashFailed {
		t.Fatalf("cond = %+v", cond)
	}
}

func TestMachineFlashHTTPStatus(t *testing.T) {
	board := startBMC(t)
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(imgSrv.Close)
	ns := "default"
	machine := tpiMachine(ns, "n1", 1)
	machine.Spec.ImageURL = imgSrv.URL
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err == nil {
		t.Fatal("expected error")
	}
}

func TestMachineDeletePowersOff(t *testing.T) {
	board := startBMC(t)
	board.SetPower(3, true)
	ns := "default"
	machine := tpiMachine(ns, "n1", 3)
	machine.Finalizers = []string{infrav1.MachineFinalizer}
	now := metav1.Now()
	machine.DeletionTimestamp = &now
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	if board.Power(3) {
		t.Fatal("expected power off")
	}
	got := &infrav1.TuringPiMachine{}
	err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "n1"}, got)
	if err == nil && controllerutil.ContainsFinalizer(got, infrav1.MachineFinalizer) {
		t.Fatal("finalizer remains")
	}
}

func TestMachineDeletePowerFailure(t *testing.T) {
	board := startBMC(t)
	board.FailPowerSet = true
	ns := "default"
	machine := tpiMachine(ns, "n1", 1)
	machine.Finalizers = []string{infrav1.MachineFinalizer}
	now := metav1.Now()
	machine.DeletionTimestamp = &now
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err == nil {
		t.Fatal("expected power error")
	}
}

func TestMachinePowerFailure(t *testing.T) {
	board := startBMC(t)
	board.FailPowerSet = true
	ns := "default"
	machine := tpiMachine(ns, "n1", 1)
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err == nil {
		t.Fatal("expected error")
	}
	got := &infrav1.TuringPiMachine{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "n1"}, got)
	cond := meta.FindStatusCondition(got.Status.Conditions, clusterv1.ReadyCondition)
	if cond == nil || cond.Reason != reasonPowerFailed {
		t.Fatalf("cond = %+v", cond)
	}
}

func TestMachineMissingSecret(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	machine := tpiMachine(ns, "n1", 1)
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err == nil {
		t.Fatal("expected error")
	}
}

func TestMachineMissingInfraRef(t *testing.T) {
	ns := "default"
	cluster := capiCluster(ns, "infra")
	cluster.Spec.InfrastructureRef = clusterv1.ContractVersionedObjectReference{}
	machine := tpiMachine(ns, "n1", 1)
	c := fakeClient(t, cluster, capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
}

func TestMachineMissingOwnerObject(t *testing.T) {
	ns := "default"
	machine := tpiMachine(ns, "n1", 1)
	c := fakeClient(t, machine)
	r := &MachineReconciler{Client: c}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err == nil {
		t.Fatal("expected missing owner error")
	}
}

func TestMachineDeleteOwnerGone(t *testing.T) {
	ns := "default"
	machine := tpiMachine(ns, "n1", 1)
	machine.Finalizers = []string{infrav1.MachineFinalizer}
	now := metav1.Now()
	machine.DeletionTimestamp = &now
	c := fakeClient(t, machine)
	r := &MachineReconciler{Client: c}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
}

func TestSlotTakenIgnoresDeleting(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	dying := tpiMachine(ns, "old", 1)
	dying.Finalizers = []string{infrav1.MachineFinalizer}
	now := metav1.Now()
	dying.DeletionTimestamp = &now
	machine := tpiMachine(ns, "n1", 1)
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), dying, machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiMachine{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "n1"}, got)
	if !ptr.Deref(got.Status.Initialization.Provisioned, false) {
		t.Fatal("deleting peer should not block the slot")
	}
}

func TestMachineSlotDifferentClusterIgnored(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	other := tpiMachine(ns, "other", 1)
	other.Labels = map[string]string{clusterv1.ClusterNameLabel: "elsewhere"}
	machine := tpiMachine(ns, "n1", 1)
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), other, machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiMachine{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "n1"}, got)
	if !ptr.Deref(got.Status.Initialization.Provisioned, false) {
		t.Fatal("other cluster's slot must not conflict")
	}
}

func TestMachineFlashInvalidURL(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	machine := tpiMachine(ns, "n1", 1)
	machine.Spec.ImageURL = "://bad"
	c := fakeClient(t, infraCluster(ns, "infra", board.URL()), credSecret(ns), capiCluster(ns, "infra"), capiMachine(ns, "n1"), machine)
	r := &MachineReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), req(ns, "n1")); err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestHelpersGetConditions(t *testing.T) {
	c := infraCluster("ns", "infra", "http://x")
	c.SetConditions([]metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}})
	if len(c.GetConditions()) != 1 {
		t.Fatal("cluster conditions")
	}
	m := tpiMachine("ns", "n1", 1)
	m.SetConditions([]metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}})
	if len(m.GetConditions()) != 1 {
		t.Fatal("machine conditions")
	}
	_ = corev1.Namespace{}
}

func req(ns, name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}}
}
