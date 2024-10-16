// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// KubeSchedulerSpecApplyConfiguration represents a declarative configuration of the KubeSchedulerSpec type for use
// with apply.
type KubeSchedulerSpecApplyConfiguration struct {
	StaticPodOperatorSpecApplyConfiguration `json:",inline"`
}

// KubeSchedulerSpecApplyConfiguration constructs a declarative configuration of the KubeSchedulerSpec type for use with
// apply.
func KubeSchedulerSpec() *KubeSchedulerSpecApplyConfiguration {
	return &KubeSchedulerSpecApplyConfiguration{}
}

// WithManagementState sets the ManagementState field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ManagementState field is set to the value of the last call.
func (b *KubeSchedulerSpecApplyConfiguration) WithManagementState(value operatorv1.ManagementState) *KubeSchedulerSpecApplyConfiguration {
	b.ManagementState = &value
	return b
}

// WithLogLevel sets the LogLevel field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the LogLevel field is set to the value of the last call.
func (b *KubeSchedulerSpecApplyConfiguration) WithLogLevel(value operatorv1.LogLevel) *KubeSchedulerSpecApplyConfiguration {
	b.LogLevel = &value
	return b
}

// WithOperatorLogLevel sets the OperatorLogLevel field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the OperatorLogLevel field is set to the value of the last call.
func (b *KubeSchedulerSpecApplyConfiguration) WithOperatorLogLevel(value operatorv1.LogLevel) *KubeSchedulerSpecApplyConfiguration {
	b.OperatorLogLevel = &value
	return b
}

// WithUnsupportedConfigOverrides sets the UnsupportedConfigOverrides field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the UnsupportedConfigOverrides field is set to the value of the last call.
func (b *KubeSchedulerSpecApplyConfiguration) WithUnsupportedConfigOverrides(value runtime.RawExtension) *KubeSchedulerSpecApplyConfiguration {
	b.UnsupportedConfigOverrides = &value
	return b
}

// WithObservedConfig sets the ObservedConfig field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ObservedConfig field is set to the value of the last call.
func (b *KubeSchedulerSpecApplyConfiguration) WithObservedConfig(value runtime.RawExtension) *KubeSchedulerSpecApplyConfiguration {
	b.ObservedConfig = &value
	return b
}

// WithForceRedeploymentReason sets the ForceRedeploymentReason field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ForceRedeploymentReason field is set to the value of the last call.
func (b *KubeSchedulerSpecApplyConfiguration) WithForceRedeploymentReason(value string) *KubeSchedulerSpecApplyConfiguration {
	b.ForceRedeploymentReason = &value
	return b
}

// WithFailedRevisionLimit sets the FailedRevisionLimit field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the FailedRevisionLimit field is set to the value of the last call.
func (b *KubeSchedulerSpecApplyConfiguration) WithFailedRevisionLimit(value int32) *KubeSchedulerSpecApplyConfiguration {
	b.FailedRevisionLimit = &value
	return b
}

// WithSucceededRevisionLimit sets the SucceededRevisionLimit field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the SucceededRevisionLimit field is set to the value of the last call.
func (b *KubeSchedulerSpecApplyConfiguration) WithSucceededRevisionLimit(value int32) *KubeSchedulerSpecApplyConfiguration {
	b.SucceededRevisionLimit = &value
	return b
}
