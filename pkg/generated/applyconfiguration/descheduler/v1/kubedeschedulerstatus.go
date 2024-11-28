// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
)

// KubeDeschedulerStatusApplyConfiguration represents a declarative configuration of the KubeDeschedulerStatus type for use
// with apply.
type KubeDeschedulerStatusApplyConfiguration struct {
	v1.OperatorStatusApplyConfiguration `json:",inline"`
}

// KubeDeschedulerStatusApplyConfiguration constructs a declarative configuration of the KubeDeschedulerStatus type for use with
// apply.
func KubeDeschedulerStatus() *KubeDeschedulerStatusApplyConfiguration {
	return &KubeDeschedulerStatusApplyConfiguration{}
}

// WithObservedGeneration sets the ObservedGeneration field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ObservedGeneration field is set to the value of the last call.
func (b *KubeDeschedulerStatusApplyConfiguration) WithObservedGeneration(value int64) *KubeDeschedulerStatusApplyConfiguration {
	b.ObservedGeneration = &value
	return b
}

// WithConditions adds the given value to the Conditions field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the Conditions field.
func (b *KubeDeschedulerStatusApplyConfiguration) WithConditions(values ...*v1.OperatorConditionApplyConfiguration) *KubeDeschedulerStatusApplyConfiguration {
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithConditions")
		}
		b.Conditions = append(b.Conditions, *values[i])
	}
	return b
}

// WithVersion sets the Version field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Version field is set to the value of the last call.
func (b *KubeDeschedulerStatusApplyConfiguration) WithVersion(value string) *KubeDeschedulerStatusApplyConfiguration {
	b.Version = &value
	return b
}

// WithReadyReplicas sets the ReadyReplicas field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ReadyReplicas field is set to the value of the last call.
func (b *KubeDeschedulerStatusApplyConfiguration) WithReadyReplicas(value int32) *KubeDeschedulerStatusApplyConfiguration {
	b.ReadyReplicas = &value
	return b
}

// WithLatestAvailableRevision sets the LatestAvailableRevision field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the LatestAvailableRevision field is set to the value of the last call.
func (b *KubeDeschedulerStatusApplyConfiguration) WithLatestAvailableRevision(value int32) *KubeDeschedulerStatusApplyConfiguration {
	b.LatestAvailableRevision = &value
	return b
}

// WithGenerations adds the given value to the Generations field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the Generations field.
func (b *KubeDeschedulerStatusApplyConfiguration) WithGenerations(values ...*v1.GenerationStatusApplyConfiguration) *KubeDeschedulerStatusApplyConfiguration {
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithGenerations")
		}
		b.Generations = append(b.Generations, *values[i])
	}
	return b
}