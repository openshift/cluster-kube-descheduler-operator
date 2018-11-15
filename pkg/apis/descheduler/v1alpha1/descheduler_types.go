package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DeschedulerSpec defines the desired state of Descheduler
type DeschedulerSpec struct {
	// Strategies contain list of strategies that should be enabled in descheduler.
	Strategies []string `json:"strategies,omitempty"`
}

type strategy struct { //TODO: Make strategy a struct with some valid params.
	Name   string
	Params map[string][]string
}

// DeschedulerStatus defines the observed state of Descheduler
type DeschedulerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Represents the descheduler operator phase. As of now, limited to Updating, Running, could be expanded later.
	Phase string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Descheduler is the Schema for the deschedulers API
// +k8s:openapi-gen=true
type Descheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeschedulerSpec   `json:"spec,omitempty"`
	Status DeschedulerStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DeschedulerList contains a list of Descheduler
type DeschedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Descheduler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Descheduler{}, &DeschedulerList{})
}
