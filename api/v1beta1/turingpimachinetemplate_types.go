/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TuringPiMachineTemplateSpec defines the desired state of TuringPiMachineTemplate.
type TuringPiMachineTemplateSpec struct {
	Template TuringPiMachineTemplateResource `json:"template"`
}

// TuringPiMachineTemplateResource describes the data needed to create a TuringPiMachine from a template.
type TuringPiMachineTemplateResource struct {
	// Standard object's metadata.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the desired behavior of the machine.
	Spec TuringPiMachineSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=turingpimachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1beta1"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta1"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

// TuringPiMachineTemplate is the Schema for the turingpimachinetemplates API.
type TuringPiMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TuringPiMachineTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TuringPiMachineTemplateList contains a list of TuringPiMachineTemplate.
type TuringPiMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TuringPiMachineTemplate `json:"items"`
}

func init() {
	objectTypes = append(objectTypes, &TuringPiMachineTemplate{}, &TuringPiMachineTemplateList{})
}
