// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RouteIngressConditionApplyConfiguration represents a declarative configuration of the RouteIngressCondition type for use
// with apply.
type RouteIngressConditionApplyConfiguration struct {
	Type               *v1.RouteIngressConditionType `json:"type,omitempty"`
	Status             *corev1.ConditionStatus       `json:"status,omitempty"`
	Reason             *string                       `json:"reason,omitempty"`
	Message            *string                       `json:"message,omitempty"`
	LastTransitionTime *metav1.Time                  `json:"lastTransitionTime,omitempty"`
}

// RouteIngressConditionApplyConfiguration constructs a declarative configuration of the RouteIngressCondition type for use with
// apply.
func RouteIngressCondition() *RouteIngressConditionApplyConfiguration {
	return &RouteIngressConditionApplyConfiguration{}
}

// WithType sets the Type field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Type field is set to the value of the last call.
func (b *RouteIngressConditionApplyConfiguration) WithType(value v1.RouteIngressConditionType) *RouteIngressConditionApplyConfiguration {
	b.Type = &value
	return b
}

// WithStatus sets the Status field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Status field is set to the value of the last call.
func (b *RouteIngressConditionApplyConfiguration) WithStatus(value corev1.ConditionStatus) *RouteIngressConditionApplyConfiguration {
	b.Status = &value
	return b
}

// WithReason sets the Reason field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Reason field is set to the value of the last call.
func (b *RouteIngressConditionApplyConfiguration) WithReason(value string) *RouteIngressConditionApplyConfiguration {
	b.Reason = &value
	return b
}

// WithMessage sets the Message field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Message field is set to the value of the last call.
func (b *RouteIngressConditionApplyConfiguration) WithMessage(value string) *RouteIngressConditionApplyConfiguration {
	b.Message = &value
	return b
}

// WithLastTransitionTime sets the LastTransitionTime field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the LastTransitionTime field is set to the value of the last call.
func (b *RouteIngressConditionApplyConfiguration) WithLastTransitionTime(value metav1.Time) *RouteIngressConditionApplyConfiguration {
	b.LastTransitionTime = &value
	return b
}