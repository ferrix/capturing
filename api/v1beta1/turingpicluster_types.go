/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const (
	// ClusterFinalizer allows TuringPiClusterReconciler to clean up resources
	// associated with TuringPiCluster before removing it from the API server.
	ClusterFinalizer = "turingpicluster.infrastructure.cluster.x-k8s.io"
)

// TuringPiClusterSpec defines the desired state of TuringPiCluster.
type TuringPiClusterSpec struct {
	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// BMC is the Turing Pi board management controller this cluster runs on.
	// +required
	BMC BMCReference `json:"bmc"`
}

// BMCReference identifies a Turing Pi BMC and how to authenticate to it.
type BMCReference struct {
	// URL is the BMC base URL, for example https://10.88.0.11 or https://turingpi.local.
	// +required
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`

	// CredentialsRef is a Secret in the same namespace with keys username and password.
	// +required
	CredentialsRef corev1.LocalObjectReference `json:"credentialsRef"`

	// InsecureSkipVerify skips TLS certificate verification. Turing Pi BMCs commonly
	// present a self-signed certificate.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// TuringPiClusterStatus defines the observed state of TuringPiCluster.
type TuringPiClusterStatus struct {
	// Conditions represents the observations of a TuringPiCluster's current state.
	// Known condition types are Ready and Paused.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Initialization provides observations of the TuringPiCluster initialization process.
	// +optional
	Initialization TuringPiClusterInitializationStatus `json:"initialization,omitempty"`
}

// TuringPiClusterInitializationStatus provides observations of cluster infrastructure provisioning.
type TuringPiClusterInitializationStatus struct {
	// Provisioned is true when the BMC is reachable and cluster infrastructure is ready.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=turingpiclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta1"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels['cluster\\.x-k8s\\.io/cluster-name']",description="Cluster"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="TuringPiCluster ready"
// +kubebuilder:printcolumn:name="Provisioned",type="string",JSONPath=".status.initialization.provisioned",description="Infrastructure provisioned"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// TuringPiCluster is the Schema for the turingpiclusters API.
type TuringPiCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TuringPiClusterSpec   `json:"spec,omitempty"`
	Status TuringPiClusterStatus `json:"status,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (c *TuringPiCluster) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets conditions for an API object.
func (c *TuringPiCluster) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// TuringPiClusterList contains a list of TuringPiCluster.
type TuringPiClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TuringPiCluster `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &TuringPiCluster{}, &TuringPiClusterList{})
}
