/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrav1 "github.com/ferrix/capturing/api/v1beta1"
)

func TestClusterReconcileReady(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	obj := infraCluster(ns, "infra", board.URL())
	c := fakeClient(t, obj, credSecret(ns), capiCluster(ns, "infra"))
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiCluster{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "infra"}, got); err != nil {
		t.Fatal(err)
	}
	if !controllerutil.ContainsFinalizer(got, infrav1.ClusterFinalizer) {
		t.Fatal("missing finalizer")
	}
	if !ptr.Deref(got.Status.Initialization.Provisioned, false) {
		t.Fatal("expected provisioned")
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, clusterv1.ReadyCondition)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != reasonBMCReachable {
		t.Fatalf("ready = %+v", cond)
	}
}

func TestClusterReconcileNotFound(t *testing.T) {
	r := &ClusterReconciler{Client: fakeClient(t)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "missing", Namespace: "ns"}}); err != nil {
		t.Fatal(err)
	}
}

func TestClusterMissingSecret(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	obj := infraCluster(ns, "infra", board.URL())
	c := fakeClient(t, obj)
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}})
	if err == nil {
		t.Fatal("expected secret error")
	}
	got := &infrav1.TuringPiCluster{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "infra"}, got)
	cond := meta.FindStatusCondition(got.Status.Conditions, clusterv1.ReadyCondition)
	if cond == nil || cond.Reason != reasonMissingSecret {
		t.Fatalf("cond = %+v", cond)
	}
}

func TestClusterEmptyCredentials(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	secret := credSecret(ns)
	secret.Data = map[string][]byte{"username": []byte("root")}
	obj := infraCluster(ns, "infra", board.URL())
	c := fakeClient(t, obj, secret)
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestClusterBMCDown(t *testing.T) {
	ns := "default"
	obj := infraCluster(ns, "infra", "http://127.0.0.1:1")
	c := fakeClient(t, obj, credSecret(ns))
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err == nil {
		t.Fatal("expected bmc error")
	}
	got := &infrav1.TuringPiCluster{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "infra"}, got)
	cond := meta.FindStatusCondition(got.Status.Conditions, clusterv1.ReadyCondition)
	if cond == nil || cond.Reason != reasonBMCUnreachable {
		t.Fatalf("cond = %+v", cond)
	}
}

func TestClusterPausedAnnotation(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	obj := infraCluster(ns, "infra", board.URL())
	obj.Annotations = map[string]string{clusterv1.PausedAnnotation: "true"}
	c := fakeClient(t, obj, credSecret(ns))
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err != nil {
		t.Fatal(err)
	}
	if board.PowerGetCalls() != 0 {
		t.Fatalf("paused still called BMC (%d)", board.PowerGetCalls())
	}
	got := &infrav1.TuringPiCluster{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "infra"}, got)
	cond := meta.FindStatusCondition(got.Status.Conditions, clusterv1.ReadyCondition)
	if cond == nil || cond.Reason != reasonPaused {
		t.Fatalf("cond = %+v", cond)
	}
}

func TestClusterPausedSpec(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	cluster := capiCluster(ns, "infra")
	cluster.Spec.Paused = ptr.To(true)
	obj := infraCluster(ns, "infra", board.URL())
	c := fakeClient(t, obj, credSecret(ns), cluster)
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err != nil {
		t.Fatal(err)
	}
	if board.PowerGetCalls() != 0 {
		t.Fatal("expected no BMC calls")
	}
}

func TestClusterCopyControlPlaneEndpoint(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	cluster := capiCluster(ns, "infra")
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{Host: "10.88.0.21", Port: 6443}
	obj := infraCluster(ns, "infra", board.URL())
	c := fakeClient(t, obj, credSecret(ns), cluster)
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiCluster{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "infra"}, got)
	if got.Spec.ControlPlaneEndpoint.Host != "10.88.0.21" || got.Spec.ControlPlaneEndpoint.Port != 6443 {
		t.Fatalf("endpoint = %+v", got.Spec.ControlPlaneEndpoint)
	}
}

func TestClusterDeleteRemovesFinalizer(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	obj := infraCluster(ns, "infra", board.URL())
	obj.Finalizers = []string{infrav1.ClusterFinalizer}
	now := metav1.Now()
	obj.DeletionTimestamp = &now
	c := fakeClient(t, obj, credSecret(ns))
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiCluster{}
	err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "infra"}, got)
	if client.IgnoreNotFound(err) != nil {
		t.Fatal(err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, infrav1.ClusterFinalizer) {
		t.Fatal("finalizer still present")
	}
}

func TestClusterMissingBMCURL(t *testing.T) {
	ns := "default"
	obj := infraCluster(ns, "infra", "")
	c := fakeClient(t, obj, credSecret(ns))
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestClusterMissingCredentialsRef(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	obj := infraCluster(ns, "infra", board.URL())
	obj.Spec.BMC.CredentialsRef = corev1.LocalObjectReference{}
	c := fakeClient(t, obj)
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestClusterInsecureSkipVerify(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	obj := infraCluster(ns, "infra", board.URL())
	obj.Spec.BMC.InsecureSkipVerify = true
	c := fakeClient(t, obj, credSecret(ns))
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err != nil {
		t.Fatal(err)
	}
}

func TestClusterDefaultBMCOptions(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	obj := infraCluster(ns, "infra", board.URL())
	c := fakeClient(t, obj, credSecret(ns))
	r := &ClusterReconciler{Client: c}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err != nil {
		t.Fatal(err)
	}
}

func TestClusterOwnerRefWithoutLabel(t *testing.T) {
	board := startBMC(t)
	ns := "default"
	obj := infraCluster(ns, "infra", board.URL())
	obj.Labels = nil
	obj.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       "cluster",
		UID:        "c",
	}}
	cluster := capiCluster(ns, "infra")
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{Host: "10.0.0.1", Port: 6443}
	c := fakeClient(t, obj, credSecret(ns), cluster)
	r := &ClusterReconciler{Client: c, BMCOptions: pollOpt()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "infra"}}); err != nil {
		t.Fatal(err)
	}
	got := &infrav1.TuringPiCluster{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "infra"}, got)
	if got.Spec.ControlPlaneEndpoint.Host != "10.0.0.1" {
		t.Fatalf("endpoint host = %q", got.Spec.ControlPlaneEndpoint.Host)
	}
}

func TestIsPausedClusterAnnotation(t *testing.T) {
	cluster := capiCluster("ns", "infra")
	cluster.Annotations = map[string]string{clusterv1.PausedAnnotation: "true"}
	obj := infraCluster("ns", "infra", "http://x")
	if !isPaused(cluster, obj) {
		t.Fatal("expected paused")
	}
	if isPaused(nil, obj) {
		t.Fatal("object without annotation is not paused")
	}
}
