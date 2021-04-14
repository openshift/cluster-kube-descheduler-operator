package v1beta1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KubeDescheduler is the Schema for the deschedulers API
// +k8s:openapi-gen=true
// +genclient
// +kubebuilder:subresource:status
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
	// DEPRECATED: This field for configuring strategies has been deprecated in favor of profiles.
	// Setting it will not do anything, it is only provided to allow transition of existing objects.
	// +optional
	Strategies []Strategy `json:"strategies,omitempty"`

	// Profiles sets which descheduler strategy profiles are enabled
	Profiles []DeschedulerProfile `json:"profiles"`

	// DeschedulingIntervalSeconds is the number of seconds between descheduler runs
	// +optional
	DeschedulingIntervalSeconds *int32 `json:"deschedulingIntervalSeconds,omitempty"`

	// Flags for descheduler.
	// DEPRECATED: This field is no longer supported
	// +optional
	Flags []string `json:"flags,omitempty"`
	// Image of the deschduler being managed. This includes the version of the operand(descheduler).
	// DEPRECATED: This field is no longer supported
	// +optional
	Image string `json:"image,omitempty"`
}

// DeschedulerProfile allows configuring the enabled strategy profiles for the descheduler
// it allows multiple profiles to be enabled at once, which will have cumulative effects on the cluster.
// +kubebuilder:validation:Enum=AffinityAndTaints;TopologyAndDuplicates;LifecycleAndUtilization
type DeschedulerProfile string

var (
	// AffinityAndTaints enables descheduling strategies that balance pods based on affinity and
	// node taint violations.
	AffinityAndTaints DeschedulerProfile = "AffinityAndTaints"

	// TopologyAndDuplicates attempts to spread pods evenly among nodes based on topology spread
	// constraints and duplicate replicas on the same node.
	TopologyAndDuplicates DeschedulerProfile = "TopologyAndDuplicates"

	// LifecycleAndUtilization attempts to balance pods based on node resource usage, pod age, and pod restarts
	LifecycleAndUtilization DeschedulerProfile = "LifecycleAndUtilization"
)

// Strategy supported by descheduler
// DEPRECATED
type Strategy struct {
	Name   string  `json:"name,omitempty"`
	Params []Param `json:"params,omitempty"`
}

// Param is a key/value pair representing the parameters in strategy or flags.
// DEPRECATED
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
