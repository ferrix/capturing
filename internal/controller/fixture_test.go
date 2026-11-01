/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrav1 "github.com/ferrix/capturing/api/v1beta1"
	"github.com/ferrix/capturing/pkg/bmc"
	bmcfake "github.com/ferrix/capturing/pkg/bmc/fake"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := infrav1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := clusterv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func fakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithStatusSubresource(&infrav1.TuringPiCluster{}, &infrav1.TuringPiMachine{}).
		WithObjects(objs...).
		Build()
}

func credSecret(ns string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bmc", Namespace: ns},
		Data: map[string][]byte{
			"username": []byte("root"),
			"password": []byte("turing"),
		},
	}
}

func startBMC(t *testing.T) *bmcfake.BMC {
	t.Helper()
	board := bmcfake.New()
	t.Cleanup(board.Close)
	return board
}

func infraCluster(ns, name, bmcURL string) *infrav1.TuringPiCluster {
	return &infrav1.TuringPiCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "cluster"},
		},
		Spec: infrav1.TuringPiClusterSpec{
			BMC: infrav1.BMCReference{
				URL:            bmcURL,
				CredentialsRef: corev1.LocalObjectReference{Name: "bmc"},
			},
		},
	}
}

func capiCluster(ns, infraName string) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: ns},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: infrav1.GroupVersion.Group,
				Kind:     "TuringPiCluster",
				Name:     infraName,
			},
		},
	}
}

func capiMachine(ns, name string) *clusterv1.Machine {
	return &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: clusterv1.MachineSpec{
			ClusterName: "cluster",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: infrav1.GroupVersion.Group,
				Kind:     "TuringPiMachine",
				Name:     name,
			},
		},
	}
}

func tpiMachine(ns, name string, node int) *infrav1.TuringPiMachine {
	return &infrav1.TuringPiMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "cluster"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Machine",
				Name:       name,
				UID:        "machine-uid",
				Controller: ptr.To(true),
			}},
		},
		Spec: infrav1.TuringPiMachineSpec{Node: node},
	}
}

func pollOpt() []bmc.Option {
	return []bmc.Option{bmc.WithPollInterval(1)}
}
