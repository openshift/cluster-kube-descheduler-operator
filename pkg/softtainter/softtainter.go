package softtainter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/rest"
	"k8s.io/kubernetes/pkg/util/taints"

	desv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/descheduler"
	desclient "sigs.k8s.io/descheduler/pkg/descheduler/client"
	nodeutil "sigs.k8s.io/descheduler/pkg/descheduler/node"
	"sigs.k8s.io/descheduler/pkg/framework/pluginregistry"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization/classifier"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization/normalizer"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	prometheusAuthTokenSecretKey        = "prometheusAuthToken"
	OverUtilizedSoftTaintKey            = "nodeutilization.descheduler.openshift.io/overutilized"
	OverUtilizedSoftTaintValue          = "true"
	AppropriatelyUtilizedSoftTaintKey   = "nodeutilization.descheduler.openshift.io/appropriate"
	AppropriatelyUtilizedSoftTaintValue = "true"
)

var (
	log = logf.Log.WithName("controller_nodeClassifier")
)

type softTainterArgs struct {
	useDeviationThresholds bool
	thresholds             api.ResourceThresholds
	targetThresholds       api.ResourceThresholds
	mode                   desv1.Mode
}

type softTainter struct {
	args                              *softTainterArgs
	resourceNames                     []corev1.ResourceName
	previousPrometheusClientTransport *http.Transport
	currentPrometheusAuthToken        string
	promClient                        promapi.Client
	promQuery                         string
	promURL                           string
	client                            client.Client
	resyncPeriod                      time.Duration
	policyConfigFile                  string
	nodeUtilizationFactory            func(promapi.Client, string) NodeUtilization
}

type NodeUtilization interface {
	NodeUsageFromPrometheusMetrics(ctx context.Context) (map[string]map[corev1.ResourceName]*resource.Quantity, error)
}

type actualNodeUtilization struct {
	promClient promapi.Client
	promQuery  string
}

var _ NodeUtilization = &actualNodeUtilization{}

func (anu *actualNodeUtilization) NodeUsageFromPrometheusMetrics(ctx context.Context) (map[string]map[corev1.ResourceName]*resource.Quantity, error) {
	return nodeutilization.NodeUsageFromPrometheusMetrics(ctx, anu.promClient, anu.promQuery)
}

func (st *softTainter) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	des := desv1.KubeDescheduler{}

	descheduler.SetupPlugins()
	policy, err := descheduler.LoadPolicyConfig(st.policyConfigFile, nil, pluginregistry.PluginRegistry)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = st.client.Get(ctx, client.ObjectKey{
		Namespace: operatorclient.OperatorNamespace,
		Name:      operatorclient.OperatorConfigName,
	}, &des)
	if err != nil {
		logger.Error(err, "failed reading descheduler operator CR")
		return reconcile.Result{}, err
	}

	if des.Spec.DeschedulingIntervalSeconds == nil || *des.Spec.DeschedulingIntervalSeconds <= 0 {
		return reconcile.Result{}, fmt.Errorf("descheduler should have an interval set and it should be greater than 0")
	}
	st.resyncPeriod = time.Duration(*des.Spec.DeschedulingIntervalSeconds) * time.Second

	nodeSelector := labels.Everything()
	if policy.NodeSelector != nil {
		sel, err := labels.Parse(*policy.NodeSelector)
		if err != nil {
			return reconcile.Result{}, err
		}
		nodeSelector = sel
	}

	nl := corev1.NodeList{}
	err = st.client.List(ctx, &nl, &client.ListOptions{LabelSelector: nodeSelector})
	if err != nil {
		return reconcile.Result{}, err
	}
	nodeList := make([]*corev1.Node, len(nl.Items))
	for i, node := range nl.Items {
		nodeList[i] = &node
	}

	var lnargs *nodeutilization.LowNodeUtilizationArgs

	for _, p := range policy.Profiles {
		if p.Name == string(desv1.RelieveAndMigrate) {
			for _, pc := range p.PluginConfigs {
				if pc.Name == nodeutilization.LowNodeUtilizationPluginName {
					lnargs = pc.Args.(*nodeutilization.LowNodeUtilizationArgs)
				}
			}
		}
	}

	if lnargs == nil {
		logger.Info("SoftTainter is disabled, cleaning up eventual leftover taints")
		err = st.cleanAllSoftTaints(ctx, nodeList)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: st.resyncPeriod}, nil
	}

	st.args = &softTainterArgs{
		useDeviationThresholds: lnargs.UseDeviationThresholds,
		thresholds:             lnargs.Thresholds,
		targetThresholds:       lnargs.TargetThresholds,
		mode:                   des.Spec.Mode,
	}

	if lnargs.MetricsUtilization == nil || lnargs.MetricsUtilization.Prometheus == nil {
		err := fmt.Errorf("unable to read MetricsUtilization.Prometheus")
		logger.Error(err, "reconciliation failed")
		return reconcile.Result{}, err
	}
	st.promQuery = lnargs.MetricsUtilization.Prometheus.Query

	var authToken *api.AuthToken
	for _, provider := range policy.MetricsProviders {
		if provider.Prometheus != nil {
			st.promURL = provider.Prometheus.URL
			authToken = provider.Prometheus.AuthToken
		}
	}
	if st.promURL == "" {
		err := fmt.Errorf("unable to read promURL")
		logger.Error(err, "reconciliation failed")
		return reconcile.Result{}, err
	}

	if authToken != nil {
		err = st.reconcileSecretToken(ctx, authToken)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else {
		err = st.reconcileInClusterSAToken()
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	st.resourceNames = getResourceNames(st.args.thresholds)

	err = st.syncSoftTaints(ctx, nodeList)
	// In case of errors, the exponential backoff mechanism of controller-runtime can easily
	// exceed the configured tick time.
	// Never return an error here to honor the fixed tick cadence:
	// the next execution will still try to process all the nodes
	// according to fresh data.
	if err != nil {
		logger.Error(err, "reconciliation failed")
	}
	return reconcile.Result{RequeueAfter: st.resyncPeriod}, nil
}

func (st *softTainter) syncSoftTaints(ctx context.Context, nodes []*corev1.Node) error {
	nodesMap := make(map[string]*corev1.Node, len(nodes))

	nu := st.nodeUtilizationFactory(st.promClient, st.promQuery)
	nodeUsages, err := nu.NodeUsageFromPrometheusMetrics(ctx)
	if err != nil {
		return err
	}

	// just to keep a symmetry with the behaviour of the descheduler
	// in case we cannot simplistically assume that the capacity for
	// the selected metric is always 100% for all the nodes
	capacities := referencedResourceListForNodesCapacity(nodes)

	for _, node := range nodes {
		if _, exists := nodeUsages[node.Name]; !exists {
			return fmt.Errorf("unable to find metric entry for %v, query: %v", node.Name, st.promQuery)
		}
		nodesMap[node.Name] = node
	}

	// usage, by default, is exposed in absolute values. we need to normalize
	// them (convert them to percentages) to be able to compare them with the
	// user provided thresholds. thresholds are already provided in percentage
	// in the <0; 100> interval.
	var usage map[string]api.ResourceThresholds
	var thresholds map[string][]api.ResourceThresholds
	if st.args.useDeviationThresholds {
		// here the thresholds provided by the user represent
		// deviations from the average so we need to treat them
		// differently. when calculating the average we only
		// need to consider the resources for which the user
		// has provided thresholds.
		usage, thresholds = assessNodesUsagesAndRelativeThresholds(
			filterResourceNames(nodeUsages, st.resourceNames),
			capacities,
			st.args.thresholds,
			st.args.targetThresholds,
		)
	} else {
		usage, thresholds = assessNodesUsagesAndStaticThresholds(
			nodeUsages,
			capacities,
			st.args.thresholds,
			st.args.targetThresholds,
		)
	}

	// classify nodes in under and over utilized. we will later try to move
	// pods from the overutilized nodes to the underutilized ones.
	nodeGroups := classifier.Classify(
		usage, thresholds,
		// underutilization criteria processing. nodes that are
		// underutilized but aren't schedulable are ignored.
		func(nodeName string, usage, threshold api.ResourceThresholds) bool {
			if nodeutil.IsNodeUnschedulable(nodesMap[nodeName]) {
				log.V(2).Info(
					"Node is unschedulable, thus not considered as underutilized",
					"node", nodesMap[nodeName],
				)
				return false
			}
			return isNodeBelowThreshold(usage, threshold)
		},
		// overutilization criteria evaluation.
		func(nodeName string, usage, threshold api.ResourceThresholds) bool {
			return isNodeAboveThreshold(usage, threshold)
		},
	)
	var lowNodes []*corev1.Node
	var apprNodes []*corev1.Node
	var highNodes []*corev1.Node
	categories := []string{"underutilized", "overutilized"}

	classifiedNodes := map[string]bool{}
	for i := range nodeGroups {
		for nodeName := range nodeGroups[i] {
			classifiedNodes[nodeName] = true
			log.Info(
				"Node has been classified",
				"category", categories[i],
				"node", nodesMap[nodeName],
				"usage", nodeUsages[nodeName],
				"usagePercentage", normalizer.Round(usage[nodeName]),
			)
			if i == 0 {
				lowNodes = append(lowNodes, nodesMap[nodeName])
			} else if i == 1 {
				highNodes = append(highNodes, nodesMap[nodeName])
			}

		}
	}

	// avoid soft-tainting appropriately utilized nodes
	// when we don't have underutilized
	category := "appropriatelyutilized"
	if len(lowNodes) == 0 {
		category = categories[0]
	}

	// log nodes that are appropriately utilized.
	for nodeName := range nodesMap {
		if !classifiedNodes[nodeName] {
			log.Info(
				"Node has been classified",
				"category", category,
				"node", nodesMap[nodeName],
				"usage", nodeUsages[nodeName],
				"usagePercentage", normalizer.Round(usage[nodeName]),
			)
			if category == categories[0] {
				lowNodes = append(lowNodes, nodesMap[nodeName])
			} else {
				apprNodes = append(apprNodes, nodesMap[nodeName])
			}
		}
	}
	log.Info("Number of under utilized nodes", "totalNumber", len(lowNodes))
	log.Info("Number of appropriately utilized nodes", "totalNumber", len(apprNodes))
	log.Info("Number of over utilized nodes", "totalNumber", len(highNodes))

	if st.args.mode == desv1.Automatic {
		err := st.taintNodes(ctx, lowNodes, apprNodes, highNodes)
		if err != nil {
			return err
		}
	}

	return nil
}

func (st *softTainter) cleanAllSoftTaints(ctx context.Context, nodes []*corev1.Node) error {
	var errs []error

	for _, node := range nodes {
		for k, v := range map[string]string{
			AppropriatelyUtilizedSoftTaintKey: AppropriatelyUtilizedSoftTaintValue,
			OverUtilizedSoftTaintKey:          OverUtilizedSoftTaintValue,
		} {
			if err := st.dropTaint(ctx, node, k, v); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (st *softTainter) reconcileInClusterSAToken() error {
	// Read the sa token and assume it has the sufficient permissions to authenticate
	cfg, err := rest.InClusterConfig()
	if err == nil {
		if st.currentPrometheusAuthToken != cfg.BearerToken {
			log.V(2).Info("Creating Prometheus client (with SA token)")
			prometheusClient, transport, err := desclient.CreatePrometheusClient(st.promURL, cfg.BearerToken)
			if err != nil {
				return fmt.Errorf("unable to create a prometheus client: %v", err)
			}
			st.promClient = prometheusClient
			if st.previousPrometheusClientTransport != nil {
				st.previousPrometheusClientTransport.CloseIdleConnections()
			}
			st.previousPrometheusClientTransport = transport
			st.currentPrometheusAuthToken = cfg.BearerToken
		}
		return nil
	}
	if errors.Is(err, rest.ErrNotInCluster) {
		return nil
	}
	return fmt.Errorf("unexpected error when reading in cluster config: %v", err)
}

func (st *softTainter) reconcileSecretToken(ctx context.Context, authToken *api.AuthToken) error {
	authTokenSecret := authToken.SecretReference
	if authTokenSecret == nil || authTokenSecret.Namespace == "" || authTokenSecret.Name == "" {
		return fmt.Errorf("prometheus metrics source configuration is missing authentication token secret")
	}

	secret := corev1.Secret{}
	err := st.client.Get(ctx, client.ObjectKey{
		Namespace: authTokenSecret.Namespace,
		Name:      authTokenSecret.Name,
	}, &secret)
	if err != nil {
		return err
	}
	token := string(secret.Data[prometheusAuthTokenSecretKey])
	if token == "" {
		return fmt.Errorf("prometheus authentication token secret missing %q data or empty", prometheusAuthTokenSecretKey)
	}
	if st.currentPrometheusAuthToken == token {
		return nil
	} else {
		prometheusClient, transport, err := desclient.CreatePrometheusClient(st.promURL, token)
		if err != nil {
			return fmt.Errorf("unable to create a prometheus client: %v", err)
		}
		st.promClient = prometheusClient
		if st.previousPrometheusClientTransport != nil {
			st.previousPrometheusClientTransport.CloseIdleConnections()
		}
		st.previousPrometheusClientTransport = transport
		st.currentPrometheusAuthToken = token
	}
	return nil
}

func (st *softTainter) taintNodes(ctx context.Context, lowNodes, apprNodes, highNodes []*corev1.Node) error {
	log.Info("reconciling soft taints on nodes")
	var errs []error

	for _, node := range lowNodes {
		log.Info("reconciling soft taints on nodes - low", "node", node.Name)
		for k, v := range map[string]string{
			AppropriatelyUtilizedSoftTaintKey: AppropriatelyUtilizedSoftTaintValue,
			OverUtilizedSoftTaintKey:          OverUtilizedSoftTaintValue,
		} {
			if err := st.dropTaint(ctx, node, k, v); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, node := range apprNodes {
		log.Info("reconciling soft taints on nodes - appr", "node", node.Name)
		for k, v := range map[string]string{
			AppropriatelyUtilizedSoftTaintKey: AppropriatelyUtilizedSoftTaintValue,
		} {
			if err := st.addTaint(ctx, node, k, v); err != nil {
				errs = append(errs, err)
			}
		}
		for k, v := range map[string]string{
			OverUtilizedSoftTaintKey: OverUtilizedSoftTaintValue,
		} {
			if err := st.dropTaint(ctx, node, k, v); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, node := range highNodes {
		log.Info("reconciling soft taints on nodes - high", "node", node.Name)
		for k, v := range map[string]string{
			AppropriatelyUtilizedSoftTaintKey: AppropriatelyUtilizedSoftTaintValue,
			OverUtilizedSoftTaintKey:          OverUtilizedSoftTaintValue,
		} {
			if err := st.addTaint(ctx, node, k, v); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (st *softTainter) addTaint(ctx context.Context, node *corev1.Node, k, v string) error {
	rNode := &corev1.Node{}
	err := st.client.Get(ctx, client.ObjectKey{
		Namespace: node.Namespace,
		Name:      node.Name,
	}, rNode)
	if err != nil {
		return err
	}
	if !taints.TaintExists(rNode.Spec.Taints, &corev1.Taint{Key: k, Value: v, Effect: corev1.TaintEffectPreferNoSchedule}) {
		updated, err := AddOrUpdateSoftTaint(ctx, st.client, rNode, k, v)
		if err != nil {
			log.Error(err, "Failed adding soft taint to node, check RBAC", "node", rNode.Name, "taint", k)
			return err
		} else if updated {
			log.Info("The soft taint got added to the node", "node", rNode.Name, "taint", k)
		}
	}
	return nil
}

func (st *softTainter) dropTaint(ctx context.Context, node *corev1.Node, k, v string) error {
	rNode := &corev1.Node{}
	err := st.client.Get(ctx, client.ObjectKey{
		Namespace: node.Namespace,
		Name:      node.Name,
	}, rNode)
	if err != nil {
		return err
	}
	if taints.TaintExists(rNode.Spec.Taints, &corev1.Taint{Key: k, Value: v, Effect: corev1.TaintEffectPreferNoSchedule}) {
		removed, err := RemoveSoftTaint(ctx, st.client, rNode, k)
		if err != nil {
			log.Error(err, "Failed removing soft taint from node, check RBAC", "node", rNode.Name, "taint", k)
			return err
		} else if removed {
			log.Info("The soft taint got removed from the node", "node", rNode.Name, "taint", k)
		}
	}
	return nil
}

func newNodeUtilizationFactory(promClient promapi.Client, promQuery string) NodeUtilization {
	return &actualNodeUtilization{promClient: promClient, promQuery: promQuery}
}

// RegisterReconciler creates a new Reconciler and registers it into manager.
func RegisterReconciler(mgr manager.Manager, policyConfigFile string) error {

	// Create a new controller
	c, err := controller.New(
		"nodeclassification-controller",
		mgr,
		controller.Options{
			Reconciler: &softTainter{
				client:                 mgr.GetClient(),
				resyncPeriod:           60 * time.Second,
				policyConfigFile:       policyConfigFile,
				nodeUtilizationFactory: newNodeUtilizationFactory,
			},
		},
	)
	if err != nil {
		return err
	}

	// Watch and enqueue to sync on descheduler configuration
	if err := c.Watch(source.Kind(mgr.GetCache(), &desv1.KubeDescheduler{}, &handler.TypedEnqueueRequestForObject[*desv1.KubeDescheduler]{})); err != nil {
		return err
	}

	return nil
}
