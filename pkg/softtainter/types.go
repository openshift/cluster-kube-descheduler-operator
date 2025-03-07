package softtainter

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/descheduler/pkg/api"
)

// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NodeClassifierArgs struct {
	metav1.TypeMeta        `json:",inline"`
	UseDeviationThresholds bool                   `json:"useDeviationThresholds,omitempty"`
	Thresholds             api.ResourceThresholds `json:"thresholds"`
	TargetThresholds       api.ResourceThresholds `json:"targetThresholds"`
	NumberOfNodes          int                    `json:"numberOfNodes,omitempty"`
	MetricsUtilization     MetricsUtilization     `json:"metricsUtilization,omitempty"`
	SoftTainter            SoftTainter            `json:"softTainter,omitempty"`

	// Naming this one differently since namespaces are still
	// considered while considering resources used by pods
	// but then filtered out before eviction
	EvictableNamespaces *api.Namespaces `json:"evictableNamespaces,omitempty"`
}

// MetricsUtilization allow to consume actual resource utilization from metrics
type MetricsUtilization struct {
	// metricsServer enables metrics from a kubernetes metrics server.
	// Please see https://kubernetes-sigs.github.io/metrics-server/ for more.
	MetricsServer bool `json:"metricsServer,omitempty"`
}

// SoftTainter allow to dynamically set/remove a soft taint to hint the scheduler to avoid overutilized nodes
type SoftTainter struct {
	// ApplySoftTaints enables the node soft tainter
	ApplySoftTaints bool `json:"applySoftTaints,omitempty"`

	// +default="nodeutilization.descheduler.kubernetes.io/overutilized"
	SoftTaintKey string `json:"softTaintKey,omitempty"`

	// +default="true"
	SoftTaintValue string `json:"softTaintValue,omitempty"`
}
