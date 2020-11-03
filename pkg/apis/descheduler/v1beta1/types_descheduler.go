package v1beta1

import (
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KubeDescheduler is the Schema for the deschedulers API
// +k8s:openapi-gen=true
// +genclient
type KubeDescheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec holds user settable values for configuration
	// +required
	Spec KubeDeschedulerSpec `json:"spec"`
	// status holds observed values from the cluster. They may not be overridden.
	// +optional
	Status KubeDeschedulerStatus `json:"status"`
}

// KubeDeschedulerSpec defines the desired state of KubeDescheduler
type KubeDeschedulerSpec struct {
	operatorv1.OperatorSpec `json:",inline"`

	// Strategies contain list of strategies that should be enabled in descheduler.
	// DEPRECATED: This field for configuring strategies has been deprecated in favor of an upstream policy.
	// Note that it may not support all strategies, please use "policy" instead.
	// +optional
	Strategies []Strategy `json:"strategies,omitempty"`

	// Policy is a reference to a configmap containing an upstream DeschedulerPolicy
	// +option
	Policy configv1.ConfigMapNameReference `json:"policy,omitempty"`

	// DeschedulingIntervalSeconds is the number of seconds between descheduler runs
	// +optional
	DeschedulingIntervalSeconds *int32 `json:"deschedulingIntervalSeconds,omitempty"`
	// Flags for descheduler.
	// +optional
	Flags []string `json:"flags,omitempty"`
	// Image of the deschduler being managed. This includes the version of the operand(descheduler).
	Image string `json:"image,omitempty"`
}

// Strategy supported by descheduler
type Strategy struct {
	Name   string  `json:"name,omitempty"`
	Params []Param `json:"params,omitempty"`
}

// Param is a key/value pair representing the parameters in strategy or flags.
type Param struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// KubeDeschedulerStatus defines the observed state of KubeDescheduler
type KubeDeschedulerStatus struct {
	operatorv1.OperatorStatus `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KubeDeschedulerList contains a list of KubeDescheduler
type KubeDeschedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeDescheduler `json:"items"`
}
