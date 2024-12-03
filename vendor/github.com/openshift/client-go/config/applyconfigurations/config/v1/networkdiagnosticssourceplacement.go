// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

import (
	v1 "k8s.io/api/core/v1"
)

// NetworkDiagnosticsSourcePlacementApplyConfiguration represents a declarative configuration of the NetworkDiagnosticsSourcePlacement type for use
// with apply.
type NetworkDiagnosticsSourcePlacementApplyConfiguration struct {
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	Tolerations  []v1.Toleration   `json:"tolerations,omitempty"`
}

// NetworkDiagnosticsSourcePlacementApplyConfiguration constructs a declarative configuration of the NetworkDiagnosticsSourcePlacement type for use with
// apply.
func NetworkDiagnosticsSourcePlacement() *NetworkDiagnosticsSourcePlacementApplyConfiguration {
	return &NetworkDiagnosticsSourcePlacementApplyConfiguration{}
}

// WithNodeSelector puts the entries into the NodeSelector field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, the entries provided by each call will be put on the NodeSelector field,
// overwriting an existing map entries in NodeSelector field with the same key.
func (b *NetworkDiagnosticsSourcePlacementApplyConfiguration) WithNodeSelector(entries map[string]string) *NetworkDiagnosticsSourcePlacementApplyConfiguration {
	if b.NodeSelector == nil && len(entries) > 0 {
		b.NodeSelector = make(map[string]string, len(entries))
	}
	for k, v := range entries {
		b.NodeSelector[k] = v
	}
	return b
}

// WithTolerations adds the given value to the Tolerations field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the Tolerations field.
func (b *NetworkDiagnosticsSourcePlacementApplyConfiguration) WithTolerations(values ...v1.Toleration) *NetworkDiagnosticsSourcePlacementApplyConfiguration {
	for i := range values {
		b.Tolerations = append(b.Tolerations, values[i])
	}
	return b
}