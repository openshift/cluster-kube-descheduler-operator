package softtainter

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"math"
	"sigs.k8s.io/descheduler/pkg/api"
	nodeutil "sigs.k8s.io/descheduler/pkg/descheduler/node"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization"
	"sort"
)

const (
	// MinResourcePercentage is the minimum value of a resource's percentage
	MinResourcePercentage = 0
	// MaxResourcePercentage is the maximum value of a resource's percentage
	MaxResourcePercentage = 100
)

type usageClient interface {
	// Both low/high node utilization plugins are expected to invoke sync right
	// after Balance method is invoked. There's no cache invalidation so each
	// Balance is expected to get the latest data by invoking sync.
	sync(nodes []*v1.Node) error
	nodeUtilization(node string) map[v1.ResourceName]*resource.Quantity
	pods(node string) []*v1.Pod
	podUsage(pod *v1.Pod) (map[v1.ResourceName]*resource.Quantity, error)
}

func normalizePercentage(percent api.Percentage) api.Percentage {
	if percent > MaxResourcePercentage {
		return MaxResourcePercentage
	}
	if percent < MinResourcePercentage {
		return MinResourcePercentage
	}
	return percent
}

// NodeUsage stores a node's info, pods on it, thresholds and its resource usage
type NodeUsage struct {
	node    *v1.Node
	usage   map[v1.ResourceName]*resource.Quantity
	allPods []*v1.Pod
}

type NodeThresholds struct {
	lowResourceThreshold  map[v1.ResourceName]*resource.Quantity
	highResourceThreshold map[v1.ResourceName]*resource.Quantity
}

type NodeInfo struct {
	NodeUsage
	thresholds NodeThresholds
}

func getNodeThresholds(
	nodes []*v1.Node,
	lowThreshold, highThreshold api.ResourceThresholds,
	resourceNames []v1.ResourceName,
	useDeviationThresholds bool,
	usageClient usageClient,
) map[string]NodeThresholds {
	nodeThresholdsMap := map[string]NodeThresholds{}

	averageResourceUsagePercent := api.ResourceThresholds{}
	if useDeviationThresholds {
		averageResourceUsagePercent = averageNodeBasicresources(nodes, usageClient)
	}

	for _, node := range nodes {
		nodeCapacity := node.Status.Capacity
		if len(node.Status.Allocatable) > 0 {
			nodeCapacity = node.Status.Allocatable
		}

		nodeThresholdsMap[node.Name] = NodeThresholds{
			lowResourceThreshold:  map[v1.ResourceName]*resource.Quantity{},
			highResourceThreshold: map[v1.ResourceName]*resource.Quantity{},
		}

		for _, resourceName := range resourceNames {
			if useDeviationThresholds {
				cap := nodeCapacity[resourceName]
				if lowThreshold[resourceName] == nodeutilization.MinResourcePercentage {
					nodeThresholdsMap[node.Name].lowResourceThreshold[resourceName] = &cap
					nodeThresholdsMap[node.Name].highResourceThreshold[resourceName] = &cap
				} else {
					nodeThresholdsMap[node.Name].lowResourceThreshold[resourceName] = resourceThreshold(nodeCapacity, resourceName, normalizePercentage(averageResourceUsagePercent[resourceName]-lowThreshold[resourceName]))
					nodeThresholdsMap[node.Name].highResourceThreshold[resourceName] = resourceThreshold(nodeCapacity, resourceName, normalizePercentage(averageResourceUsagePercent[resourceName]+highThreshold[resourceName]))
				}
			} else {
				nodeThresholdsMap[node.Name].lowResourceThreshold[resourceName] = resourceThreshold(nodeCapacity, resourceName, lowThreshold[resourceName])
				nodeThresholdsMap[node.Name].highResourceThreshold[resourceName] = resourceThreshold(nodeCapacity, resourceName, highThreshold[resourceName])
			}
		}

	}
	return nodeThresholdsMap
}

func getNodeUsage(
	nodes []*v1.Node,
	usageClient usageClient,
) []NodeUsage {
	var nodeUsageList []NodeUsage

	for _, node := range nodes {
		nodeUsageList = append(nodeUsageList, NodeUsage{
			node:    node,
			usage:   usageClient.nodeUtilization(node.Name),
			allPods: usageClient.pods(node.Name),
		})
	}

	return nodeUsageList
}

func resourceThreshold(nodeCapacity v1.ResourceList, resourceName v1.ResourceName, threshold api.Percentage) *resource.Quantity {
	defaultFormat := resource.DecimalSI
	if resourceName == v1.ResourceMemory {
		defaultFormat = resource.BinarySI
	}

	resourceCapacityFraction := func(resourceNodeCapacity int64) int64 {
		// A threshold is in percentages but in <0;100> interval.
		// Performing `threshold * 0.01` will convert <0;100> interval into <0;1>.
		// Multiplying it with capacity will give fraction of the capacity corresponding to the given resource threshold in Quantity units.
		return int64(float64(threshold) * 0.01 * float64(resourceNodeCapacity))
	}

	resourceCapacityQuantity := nodeCapacity.Name(resourceName, defaultFormat)

	if resourceName == v1.ResourceCPU {
		return resource.NewMilliQuantity(resourceCapacityFraction(resourceCapacityQuantity.MilliValue()), defaultFormat)
	}
	return resource.NewQuantity(resourceCapacityFraction(resourceCapacityQuantity.Value()), defaultFormat)
}

func roundTo2Decimals(percentage float64) float64 {
	return math.Round(percentage*100) / 100
}

func resourceUsagePercentages(nodeUsage NodeUsage) map[v1.ResourceName]float64 {
	nodeCapacity := nodeUsage.node.Status.Capacity
	if len(nodeUsage.node.Status.Allocatable) > 0 {
		nodeCapacity = nodeUsage.node.Status.Allocatable
	}

	resourceUsagePercentage := map[v1.ResourceName]float64{}
	for resourceName, resourceUsage := range nodeUsage.usage {
		cap := nodeCapacity[resourceName]
		if !cap.IsZero() {
			resourceUsagePercentage[resourceName] = 100 * float64(resourceUsage.MilliValue()) / float64(cap.MilliValue())
			resourceUsagePercentage[resourceName] = roundTo2Decimals(resourceUsagePercentage[resourceName])
		}
	}

	return resourceUsagePercentage
}

// classifyNodes classifies the nodes into low-utilization or high-utilization nodes. If a node lies between
// low and high thresholds, it is simply ignored.
func classifyNodes(
	nodeUsages []NodeUsage,
	nodeThresholds map[string]NodeThresholds,
	lowThresholdFilter, highThresholdFilter func(node *v1.Node, usage NodeUsage, threshold NodeThresholds) bool,
) ([]NodeInfo, []NodeInfo, []NodeInfo) {
	lowNodes, apprNodes, highNodes := []NodeInfo{}, []NodeInfo{}, []NodeInfo{}

	for _, nodeUsage := range nodeUsages {
		nodeInfo := NodeInfo{
			NodeUsage:  nodeUsage,
			thresholds: nodeThresholds[nodeUsage.node.Name],
		}
		if lowThresholdFilter(nodeUsage.node, nodeUsage, nodeThresholds[nodeUsage.node.Name]) {
			klog.InfoS("Node is underutilized", "node", klog.KObj(nodeUsage.node), "usage", nodeUsage.usage, "usagePercentage", resourceUsagePercentages(nodeUsage))
			lowNodes = append(lowNodes, nodeInfo)
		} else if highThresholdFilter(nodeUsage.node, nodeUsage, nodeThresholds[nodeUsage.node.Name]) {
			klog.InfoS("Node is overutilized", "node", klog.KObj(nodeUsage.node), "usage", nodeUsage.usage, "usagePercentage", resourceUsagePercentages(nodeUsage))
			highNodes = append(highNodes, nodeInfo)
		} else {
			klog.InfoS("Node is appropriately utilized", "node", klog.KObj(nodeUsage.node), "usage", nodeUsage.usage, "usagePercentage", resourceUsagePercentages(nodeUsage))
			apprNodes = append(apprNodes, nodeInfo)
		}
	}

	return lowNodes, apprNodes, highNodes
}

func usageToKeysAndValues(usage map[v1.ResourceName]*resource.Quantity) []interface{} {
	// log message in one line
	keysAndValues := []interface{}{}
	if quantity, exists := usage[v1.ResourceCPU]; exists {
		keysAndValues = append(keysAndValues, "CPU", quantity.MilliValue())
	}
	if quantity, exists := usage[v1.ResourceMemory]; exists {
		keysAndValues = append(keysAndValues, "Mem", quantity.Value())
	}
	if quantity, exists := usage[v1.ResourcePods]; exists {
		keysAndValues = append(keysAndValues, "Pods", quantity.Value())
	}
	for name := range usage {
		if !nodeutil.IsBasicResource(name) {
			keysAndValues = append(keysAndValues, string(name), usage[name].Value())
		}
	}
	return keysAndValues
}

// sortNodesByUsage sorts nodes based on usage according to the given plugin.
func sortNodesByUsage(nodes []NodeInfo, ascending bool) {
	sort.Slice(nodes, func(i, j int) bool {
		ti := resource.NewQuantity(0, resource.DecimalSI).Value()
		tj := resource.NewQuantity(0, resource.DecimalSI).Value()
		for resourceName := range nodes[i].usage {
			if resourceName == v1.ResourceCPU {
				ti += nodes[i].usage[resourceName].MilliValue()
			} else {
				ti += nodes[i].usage[resourceName].Value()
			}
		}
		for resourceName := range nodes[j].usage {
			if resourceName == v1.ResourceCPU {
				tj += nodes[j].usage[resourceName].MilliValue()
			} else {
				tj += nodes[j].usage[resourceName].Value()
			}
		}

		// Return ascending order for HighNodeUtilization plugin
		if ascending {
			return ti < tj
		}

		// Return descending order for LowNodeUtilization plugin
		return ti > tj
	})
}

// isNodeAboveTargetUtilization checks if a node is overutilized
// At least one resource has to be above the high threshold
func isNodeAboveTargetUtilization(usage NodeUsage, threshold map[v1.ResourceName]*resource.Quantity) bool {
	for name, nodeValue := range usage.usage {
		// usage.highResourceThreshold[name] < nodeValue
		if threshold[name].Cmp(*nodeValue) == -1 {
			return true
		}
	}
	return false
}

// isNodeWithLowUtilization checks if a node is underutilized
// All resources have to be below the low threshold
func isNodeWithLowUtilization(usage NodeUsage, threshold map[v1.ResourceName]*resource.Quantity) bool {
	for name, nodeValue := range usage.usage {
		// usage.lowResourceThreshold[name] < nodeValue
		if threshold[name].Cmp(*nodeValue) == -1 {
			return false
		}
	}

	return true
}

// getResourceNames returns list of resource names in resource thresholds
func getResourceNames(thresholds api.ResourceThresholds) []v1.ResourceName {
	resourceNames := make([]v1.ResourceName, 0, len(thresholds))
	for name := range thresholds {
		resourceNames = append(resourceNames, name)
	}
	return resourceNames
}

func averageNodeBasicresources(nodes []*v1.Node, usageClient usageClient) api.ResourceThresholds {
	total := api.ResourceThresholds{}
	average := api.ResourceThresholds{}
	numberOfNodes := len(nodes)
	for _, node := range nodes {
		usage := usageClient.nodeUtilization(node.Name)
		nodeCapacity := node.Status.Capacity
		if len(node.Status.Allocatable) > 0 {
			nodeCapacity = node.Status.Allocatable
		}
		for resource, value := range usage {
			nodeCapacityValue := nodeCapacity[resource]
			if resource == v1.ResourceCPU {
				total[resource] += api.Percentage(value.MilliValue()) / api.Percentage(nodeCapacityValue.MilliValue()) * 100.0
			} else {
				total[resource] += api.Percentage(value.Value()) / api.Percentage(nodeCapacityValue.Value()) * 100.0
			}
		}
	}
	for resource, value := range total {
		average[resource] = value / api.Percentage(numberOfNodes)
	}
	return average
}
