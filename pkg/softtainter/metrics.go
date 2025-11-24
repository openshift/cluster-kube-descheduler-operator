package softtainter

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/descheduler/pkg/api"
)

const (
	// Classification values for nodeClassification metric
	ClassificationUnderUtilized         = 0
	ClassificationAppropriatelyUtilized = 1
	ClassificationOverUtilized          = 2
)

var (
	// nodeUtilizationThreshold tracks the threshold values used for classification
	// For static thresholds, this will be the same across all nodes
	// For deviation-based thresholds this will vary over the time and potentially also per node
	nodeUtilizationThreshold = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "descheduler_softtainter_node_utilization_threshold",
			Help: "Threshold value for node utilization classification (0-1 ratio). " +
				"For deviation-based mode, this will vary over the time and potentially also per node.",
		},
		[]string{
			"node",           // Node name
			"resource",       // Resource type (cpu, memory, etc.)
			"threshold_type", // "low" or "high"
		},
	)

	// nodeUtilizationValue tracks the actual utilization percentage used for classification
	nodeUtilizationValue = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "descheduler_softtainter_node_utilization_value",
			Help: "Actual node utilization ratio (0-1) used for classification",
		},
		[]string{
			"node",     // Node name
			"resource", // Resource type (cpu, memory, etc.)
		},
	)

	// nodeClassification tracks the classification result for each node
	// 0 = under-utilized, 1 = appropriately-utilized, 2 = over-utilized
	nodeClassification = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "descheduler_softtainter_node_classification",
			Help: "Node classification: 0=under-utilized, 1=appropriately-utilized, 2=over-utilized",
		},
		[]string{
			"node", // Node name
		},
	)

	// thresholdMode tracks whether we're using static or deviation-based thresholds
	thresholdMode = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "descheduler_softtainter_threshold_mode",
			Help: "Threshold mode: 0=static thresholds, 1=deviation-based thresholds",
		},
	)
)

func init() {
	// Register custom metrics with the controller-runtime metrics registry
	metrics.Registry.MustRegister(
		nodeUtilizationThreshold,
		nodeUtilizationValue,
		nodeClassification,
		thresholdMode,
	)
}

// resetMetrics clears all node-specific metrics
func resetMetrics() {
	nodeUtilizationThreshold.Reset()
	nodeUtilizationValue.Reset()
	nodeClassification.Reset()
}

// recordThresholdMode records whether we're using static or deviation-based thresholds
func recordThresholdMode(useDeviationThresholds bool) {
	if useDeviationThresholds {
		thresholdMode.Set(1)
	} else {
		thresholdMode.Set(0)
	}
}

// recordNodeMetrics records all metrics for a node
func recordNodeMetrics(nodeName string, classification int, utilization, lowThreshold, highThreshold api.ResourceThresholds) {
	// Record classification
	nodeClassification.WithLabelValues(nodeName).Set(float64(classification))

	// Record utilization values (convert from 0-100 percentage to 0-1 ratio)
	for resource, value := range utilization {
		nodeUtilizationValue.WithLabelValues(nodeName, string(resource)).Set(float64(value) / 100.0)
	}

	// Record low thresholds (convert from 0-100 percentage to 0-1 ratio)
	for resource, value := range lowThreshold {
		nodeUtilizationThreshold.WithLabelValues(nodeName, string(resource), "low").Set(float64(value) / 100.0)
	}

	// Record high thresholds (convert from 0-100 percentage to 0-1 ratio)
	for resource, value := range highThreshold {
		nodeUtilizationThreshold.WithLabelValues(nodeName, string(resource), "high").Set(float64(value) / 100.0)
	}
}
