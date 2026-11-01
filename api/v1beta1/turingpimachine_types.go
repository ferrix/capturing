/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const (
	// MachineFinalizer allows TuringPiMachineReconciler to power off a node
	// before removing the object from the API server.
	MachineFinalizer = "turingpimachine.infrastructure.cluster.x-k8s.io"

	// AdoptedAnnotation marks a TuringPiMachine as an existing node. Delete
	// removes the finalizer only; the BMC is not powered off. Flash is skipped.
	AdoptedAnnotation = "capturing.infrastructure.cluster.x-k8s.io/adopted"
)

// TuringPiMachineSpec defines the desired state of TuringPiMachine.
type TuringPiMachineSpec struct {
	// ProviderID is the BMC identity of this slot in the form turingpi://<bmc-host>/node/<n>.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	ProviderID string `json:"providerID,omitempty"`

	// Node is the Turing Pi slot, 1–4, matching `tpi` numbering.
	// +required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4
	Node int `json:"node"`

	// ImageURL is an optional raw disk image to flash onto the module before power-on.
	// Flashing runs once, before the machine is marked provisioned.
	// +optional
	ImageURL string `json:"imageURL,omitempty"`
}

// TuringPiMachineStatus defines the observed state of TuringPiMachine.
type TuringPiMachineStatus struct {
	// Conditions represents the observations of a TuringPiMachine's current state.
	// Known condition types are Ready and Paused.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Initialization provides observations of the TuringPiMachine initialization process.
	// +optional
	Initialization TuringPiMachineInitializationStatus `json:"initialization,omitempty"`

	// Addresses contains associated addresses for the machine.
	// +optional
	Addresses []clusterv1.MachineAddress `json:"addresses,omitempty"`
}

// TuringPiMachineInitializationStatus provides observations of machine infrastructure provisioning.
type TuringPiMachineInitializationStatus struct {
	// Provisioned is true when the slot is powered on and the provider ID is set.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=turingpimachines,scope=Namespaced,categories=cluster-api
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta1"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels['cluster\\.x-k8s\\.io/cluster-name']",description="Cluster"
// +kubebuilder:printcolumn:name="Node",type="integer",JSONPath=".spec.node",description="Turing Pi slot"
// +kubebuilder:printcolumn:name="Provider ID",type="string",JSONPath=".spec.providerID",description="Provider ID"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="TuringPiMachine ready"
// +kubebuilder:printcolumn:name="Provisioned",type="string",JSONPath=".status.initialization.provisioned",description="Machine provisioned"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// TuringPiMachine is the Schema for the turingpimachines API.
type TuringPiMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TuringPiMachineSpec   `json:"spec,omitempty"`
	Status TuringPiMachineStatus `json:"status,omitempty"`
}

// GetConditions returns the set of conditions for this object.
func (c *TuringPiMachine) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets conditions for an API object.
func (c *TuringPiMachine) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// TuringPiMachineList contains a list of TuringPiMachine.
type TuringPiMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TuringPiMachine `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &TuringPiMachine{}, &TuringPiMachineList{})
}
