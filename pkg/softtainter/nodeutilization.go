package softtainter

import (
	"maps"
	"slices"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization/normalizer"
)

// filterResourceNamesFromNodeUsage removes from the node usage slice all keys
// that are not present in the resourceNames slice.
func filterResourceNames(
	from map[string]api.ReferencedResourceList, resourceNames []v1.ResourceName,
) map[string]api.ReferencedResourceList {
	newNodeUsage := make(map[string]api.ReferencedResourceList)
	for nodeName, usage := range from {
		newNodeUsage[nodeName] = api.ReferencedResourceList{}
		for _, resourceName := range resourceNames {
			if _, exists := usage[resourceName]; exists {
				newNodeUsage[nodeName][resourceName] = usage[resourceName]
			}
		}
	}
	return newNodeUsage
}

// getResourceNames returns list of resource names in resource thresholds
func getResourceNames(thresholds api.ResourceThresholds) []v1.ResourceName {
	resourceNames := make([]v1.ResourceName, 0, len(thresholds))
	for name := range thresholds {
		resourceNames = append(resourceNames, name)
	}
	return resourceNames
}

// assessNodesUsagesAndStaticThresholds converts the raw usage data into
// percentage. Returns the usage (pct) and the thresholds (pct) for each
// node.
func assessNodesUsagesAndStaticThresholds(
	rawUsages, rawCapacities map[string]api.ReferencedResourceList,
	lowSpan, highSpan api.ResourceThresholds,
) (map[string]api.ResourceThresholds, map[string][]api.ResourceThresholds) {
	// first we normalize the node usage from the raw data (Mi, Gi, etc)
	// into api.Percentage values.
	usage := normalizer.Normalize(
		rawUsages, rawCapacities, nodeutilization.ResourceUsageToResourceThreshold,
	)

	// we are not taking the average and applying deviations to it we can
	// simply replicate the same threshold across all nodes and return.
	thresholds := normalizer.Replicate(
		slices.Collect(maps.Keys(usage)),
		[]api.ResourceThresholds{lowSpan, highSpan},
	)
	return usage, thresholds
}

// referencedResourceListForNodeCapacity returns a ReferencedResourceList for
// the capacity of a node. If allocatable resources are present, they are used
// instead of capacity.
func referencedResourceListForNodeCapacity(node *v1.Node) api.ReferencedResourceList {
	capacity := node.Status.Capacity
	if len(node.Status.Allocatable) > 0 {
		capacity = node.Status.Allocatable
	}

	referenced := api.ReferencedResourceList{}
	for name, quantity := range capacity {
		referenced[name] = ptr.To(quantity)
	}

	// XXX the descheduler also manages monitoring queries that are
	// supposed to return a value representing a percentage of the
	// resource usage. In this case we need to provide a value for
	// the MetricResource, which is not present in the node capacity.
	referenced[nodeutilization.MetricResource] = resource.NewQuantity(
		100, resource.DecimalSI,
	)

	return referenced
}

// assessNodesUsagesAndRelativeThresholds converts the raw usage data into
// percentage. Thresholds are calculated based on the average usage. Returns
// the usage (pct) and the thresholds (pct) for each node.
func assessNodesUsagesAndRelativeThresholds(
	rawUsages, rawCapacities map[string]api.ReferencedResourceList,
	lowSpan, highSpan api.ResourceThresholds,
) (map[string]api.ResourceThresholds, map[string][]api.ResourceThresholds) {
	// first we normalize the node usage from the raw data (Mi, Gi, etc)
	// into api.Percentage values.
	usage := normalizer.Normalize(
		rawUsages, rawCapacities, nodeutilization.ResourceUsageToResourceThreshold,
	)

	// calculate the average usage and then deviate it according to the
	// user provided thresholds.
	average := normalizer.Average(usage)

	// calculate the average usage and then deviate it according to the
	// user provided thresholds. We also ensure that the value after the
	// deviation is at least 1%. this call also replicates the thresholds
	// across all nodes.
	thresholds := normalizer.Replicate(
		slices.Collect(maps.Keys(usage)),
		normalizer.Map(
			[]api.ResourceThresholds{
				normalizer.Sum(average, normalizer.Negate(lowSpan)),
				normalizer.Sum(average, highSpan),
			},
			func(thresholds api.ResourceThresholds) api.ResourceThresholds {
				return normalizer.Clamp(thresholds, 0, 100)
			},
		),
	)

	return usage, thresholds
}

// isNodeAboveThreshold checks if a node is over a threshold
// At least one resource has to be above the threshold
func isNodeAboveThreshold(usage, threshold api.ResourceThresholds) bool {
	for name := range threshold {
		if threshold[name] < usage[name] {
			return true
		}
	}
	return false
}

// isNodeBelowThreshold checks if a node is under a threshold
// All resources have to be below the threshold
func isNodeBelowThreshold(usage, threshold api.ResourceThresholds) bool {
	for name := range threshold {
		if threshold[name] < usage[name] {
			return false
		}
	}
	return true
}

// referencedResourceListForNodesCapacity returns a ReferencedResourceList for
// the capacity of a list of nodes. If allocatable resources are present, they
// are used instead of capacity.
func referencedResourceListForNodesCapacity(nodes []*v1.Node) map[string]api.ReferencedResourceList {
	capacities := map[string]api.ReferencedResourceList{}
	for _, node := range nodes {
		capacities[node.Name] = referencedResourceListForNodeCapacity(node)
	}
	return capacities
}
