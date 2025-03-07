package softtainter

import (
	"context"
	"fmt"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"sigs.k8s.io/descheduler/pkg/api/v1alpha2"
	"sigs.k8s.io/descheduler/pkg/descheduler/metricscollector"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization"

	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	nodeutil "sigs.k8s.io/descheduler/pkg/descheduler/node"
	"time"

	"github.com/ghodss/yaml"

	desv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
)

var (
	log = logf.Log.WithName("controller_nodeClassifier")
)

type nodeClassifier struct {
	args                     *NodeClassifierArgs
	underutilizationCriteria []interface{}
	overutilizationCriteria  []interface{}
	resourceNames            []corev1.ResourceName
	usageClient              usageClient
	client                   client.Client
	resyncPeriod             time.Duration
	MetricsCollector         *metricscollector.MetricsCollector
}

func (nc *nodeClassifier) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	expectedCMNN := types.NamespacedName{
		Namespace: operatorclient.OperatorNamespace,
		Name:      operatorclient.OperatorConfigName, // TODO: check this
	}
	if request.NamespacedName != expectedCMNN {
		logger.Info("Skipping reconciliation because the requested resource is not the relevant configMap for the descheduler")
		return reconcile.Result{RequeueAfter: nc.resyncPeriod}, nil
	}

	configMap := corev1.ConfigMap{}
	descheduler := desv1.KubeDescheduler{}
	policy := &v1alpha2.DeschedulerPolicy{}

	err := nc.client.Get(ctx, request.NamespacedName, &configMap)
	if err != nil {
		return reconcile.Result{}, err
	}

	policy_yaml, ok := configMap.Data["policy.yaml"]
	if !ok {
		err := fmt.Errorf("unable to find policy.yaml in the configMap for the descheduler")
		logger.Error(err, "reconciliation failed")
		return reconcile.Result{}, err
	}
	err = yaml.Unmarshal([]byte(policy_yaml), policy)
	if err != nil {
		logger.Error(err, "failed parsing policy.yaml")
		return reconcile.Result{}, err
	}

	err = nc.client.Get(ctx, client.ObjectKey{
		Namespace: operatorclient.OperatorNamespace,
		Name:      operatorclient.OperatorConfigName,
	}, &descheduler)
	if err != nil {
		logger.Error(err, "failed reading descheduler operator CR")
		return reconcile.Result{}, err
	}

	if descheduler.Spec.DeschedulingIntervalSeconds == nil {
		return reconcile.Result{}, fmt.Errorf("descheduler should have an interval set")
	}
	nc.resyncPeriod = time.Duration(*descheduler.Spec.DeschedulingIntervalSeconds) * time.Second

	var lnargs *nodeutilization.LowNodeUtilizationArgs
	for _, p := range policy.Profiles {
		if p.Name == string(desv1.LifecycleAndUtilization) {
			for _, pc := range p.PluginConfigs {
				if pc.Name == nodeutilization.LowNodeUtilizationPluginName {
					lnargs = pc.Args.Object.(*nodeutilization.LowNodeUtilizationArgs)
				}
			}
		}
	}
	if lnargs == nil {
		err := fmt.Errorf("unable to read LowNodeUtilizationArgs")
		logger.Error(err, "reconciliation failed")
		return reconcile.Result{}, err
	}

	nc.args = &NodeClassifierArgs{
		UseDeviationThresholds: lnargs.UseDeviationThresholds,
		Thresholds:             lnargs.Thresholds,
		TargetThresholds:       lnargs.TargetThresholds,
		MetricsUtilization:     MetricsUtilization{MetricsServer: lnargs.MetricsUtilization.MetricsServer},
		EvictableNamespaces:    lnargs.EvictableNamespaces,
	}

	nc.resourceNames = getResourceNames(nc.args.Thresholds)
	// TODO: import usage clients
	/*
		if nc.args.MetricsUtilization.MetricsServer {
			if nc.MetricsCollector == nil {
				err := fmt.Errorf("metrics client not initialized")
				logger.Error(err, "reconciliation failed")
				return reconcile.Result{}, err
			}
			nc.usageClient = newActualUsageClient(resourceNames, handle.GetPodsAssignedToNodeFunc(), handle.MetricsCollector())
		} else {
			nc.usageClient = newRequestedUsageClient(resourceNames, handle.GetPodsAssignedToNodeFunc())
		}
	*/

	nl := corev1.NodeList{}
	err = nc.client.List(ctx, &nl, &client.ListOptions{})
	if err != nil {
		return reconcile.Result{}, err
	}
	nodeList := make([]*corev1.Node, len(nl.Items))
	for _, node := range nl.Items {
		nodeList = append(nodeList, &node)
	}
	err = nc.syncSoftTaints(ctx, nodeList)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{RequeueAfter: nc.resyncPeriod}, nil
}

func (nc *nodeClassifier) syncSoftTaints(ctx context.Context, nodes []*corev1.Node) error {
	if err := nc.usageClient.sync(nodes); err != nil {
		return err
	}

	lowNodes, apprNodes, sourceNodes := classifyNodes(
		getNodeUsage(nodes, nc.usageClient),
		getNodeThresholds(nodes, nc.args.Thresholds, nc.args.TargetThresholds, nc.resourceNames, nc.args.UseDeviationThresholds, nc.usageClient),
		// The node has to be schedulable (to be able to move workload there)
		func(node *corev1.Node, usage NodeUsage, threshold NodeThresholds) bool {
			if nodeutil.IsNodeUnschedulable(node) {
				klog.V(2).InfoS("Node is unschedulable, thus not considered as underutilized", "node", klog.KObj(node))
				return false
			}
			return isNodeWithLowUtilization(usage, threshold.lowResourceThreshold)
		},
		func(node *corev1.Node, usage NodeUsage, threshold NodeThresholds) bool {
			return isNodeAboveTargetUtilization(usage, threshold.highResourceThreshold)
		},
	)

	if nc.args.SoftTainter.ApplySoftTaints {
		nc.taintOverUtilized(ctx, lowNodes, apprNodes, sourceNodes)
	}

	// log message for nodes with low utilization
	klog.V(1).InfoS("Criteria for a node under utilization", nc.underutilizationCriteria...)
	klog.V(1).InfoS("Number of underutilized nodes", "totalNumber", len(lowNodes))

	// log message for over utilized nodes
	klog.V(1).InfoS("Criteria for a node above target utilization", nc.overutilizationCriteria...)
	klog.V(1).InfoS("Number of overutilized nodes", "totalNumber", len(sourceNodes))

	return nil
}

func (nc *nodeClassifier) taintOverUtilized(ctx context.Context, lowNodes, apprNodes, highNodes []NodeInfo) {
	for _, nodeInfo := range append(lowNodes, apprNodes...) {
		if IsNodeSoftTainted(nodeInfo.node, nc.args.SoftTainter.SoftTaintKey, nc.args.SoftTainter.SoftTaintValue) {
			removed, err := RemoveSoftTaint(ctx, nc.client, nodeInfo.node, nc.args.SoftTainter.SoftTaintKey)
			if err != nil {
				klog.ErrorS(err, "Failed removing soft taint from node, check RBAC", "node", nodeInfo.node.Name, "taint", nc.args.SoftTainter.SoftTaintKey)
			} else if removed {
				klog.InfoS("The soft taint got removed from the node since it's not considered overutilized anymore", "node", nodeInfo.node.Name, "taint", nc.args.SoftTainter.SoftTaintKey)
			}
		}
	}
	for _, nodeInfo := range highNodes {
		if !IsNodeSoftTainted(nodeInfo.node, nc.args.SoftTainter.SoftTaintKey, nc.args.SoftTainter.SoftTaintValue) {
			updated, err := AddOrUpdateSoftTaint(ctx, nc.client, nodeInfo.node, nc.args.SoftTainter.SoftTaintKey, nc.args.SoftTainter.SoftTaintValue)
			if err != nil {
				klog.ErrorS(err, "Failed adding soft taint to node, check RBAC", "node", nodeInfo.node.Name, "taint", nc.args.SoftTainter.SoftTaintKey)
			} else if updated {
				klog.InfoS("The soft taint got added to the node since it's now considered overutilized", "node", nodeInfo.node.Name, "taint", nc.args.SoftTainter.SoftTaintKey)
			}
		}
	}
}

// RegisterReconciler creates a new Reconciler and registers it into manager.
func RegisterReconciler(mgr manager.Manager) error {

	// TODO: import metrics collector
	//metricsCollector = metricscollector.NewMetricsCollector(sharedInformerFactory.Core().V1().Nodes().Lister(), rs.MetricsClient, nodeSelector)

	// Create a new controller
	c, err := controller.New(
		"nodeclassification-controller",
		mgr,
		controller.Options{
			Reconciler: &nodeClassifier{
				args:                     nil, // TODO:
				underutilizationCriteria: nil, // TODO:
				overutilizationCriteria:  nil, // TODO:
				resourceNames:            nil, // TODO:
				usageClient:              nil, // TODO:
				client:                   mgr.GetClient(),
				resyncPeriod:             60 * time.Second, // TODO: read the descheduler CR once
			},
		},
	)
	if err != nil {
		return err
	}

	// Watch and enqueue the CM for the descheduler to sync on descheduler configuration
	if err := c.Watch(source.Kind(mgr.GetCache(), &corev1.ConfigMap{}, &handler.TypedEnqueueRequestForObject[*corev1.ConfigMap]{})); err != nil {
		return err
	}

	return nil
}
