package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type DeschedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Descheduler `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Descheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              DeschedulerSpec   `json:"spec"`
	Status            DeschedulerStatus `json:"status,omitempty"`
}

type DeschedulerSpec struct {
	// BaseImage is the image to be used for descheduler
	BaseImage string `json:"baseImage,omitempty"`
	Time   time.Duration `json:"time,omitempty"`
}

type DeschedulerStatus struct {
	// State of the descheduler if it is running, or inactive etc.
	State string `json:"state"`
}
