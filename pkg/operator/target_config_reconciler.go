package operator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ghodss/yaml"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	configlistersv1 "github.com/openshift/client-go/config/listers/config/v1"
	routeinformers "github.com/openshift/client-go/route/informers/externalversions"
	routelistersv1 "github.com/openshift/client-go/route/listers/route/v1"
	"github.com/openshift/cluster-kube-descheduler-operator/bindata"
	deschedulerv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	operatorconfigclientv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned/typed/descheduler/v1"
	operatorclientinformers "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/informers/externalversions/descheduler/v1"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/library-go/pkg/controller"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	utilptr "k8s.io/utils/ptr"

	deschedulerapi "sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/api/v1alpha2"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/defaultevictor"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/podlifetime"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removeduplicates"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodshavingtoomanyrestarts"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatinginterpodantiaffinity"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatingnodeaffinity"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatingnodetaints"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatingtopologyspreadconstraint"
)

const DefaultImage = "quay.io/openshift/origin-descheduler:latest"

// deschedulerCommand provides descheduler command with policyconfigfile mounted as volume and log-level for backwards
// compatibility with 3.11
var DeschedulerCommand = []string{"/bin/descheduler", "--policy-config-file", "/policy-dir/policy.yaml", "--v", "2"}

type TargetConfigReconciler struct {
	ctx                      context.Context
	deschedulerImagePullSpec string
	softtainterImagePullSpec string
	operatorClient           operatorconfigclientv1.KubedeschedulersV1Interface
	deschedulerClient        *operatorclient.DeschedulerClient
	kubeClient               kubernetes.Interface
	dynamicClient            dynamic.Interface
	eventRecorder            events.Recorder
	queue                    workqueue.RateLimitingInterface
	protectedNamespaces      []string
	configSchedulerLister    configlistersv1.SchedulerLister
	routeRouteLister         routelistersv1.RouteLister
}

func NewTargetConfigReconciler(
	ctx context.Context,
	deschedulerImagePullSpec string,
	softtainterImagePullSpec string,
	operatorConfigClient operatorconfigclientv1.KubedeschedulersV1Interface,
	operatorClientInformer operatorclientinformers.KubeDeschedulerInformer,
	deschedulerClient *operatorclient.DeschedulerClient,
	kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	configInformer configinformers.SharedInformerFactory,
	routeInformers routeinformers.SharedInformerFactory,
	eventRecorder events.Recorder,
) *TargetConfigReconciler {
	// make sure our list of excluded system namespaces is up to date
	allNamespaces, err := kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.ErrorS(err, "error listing namespaces")
		return nil
	}
	protectedNamespaces := []string{"kube-system", "hypershift"}
	for _, ns := range allNamespaces.Items {
		if strings.HasPrefix(ns.Name, "openshift-") {
			protectedNamespaces = append(protectedNamespaces, ns.Name)
		}
	}

	c := &TargetConfigReconciler{
		ctx:                      ctx,
		operatorClient:           operatorConfigClient,
		deschedulerClient:        deschedulerClient,
		kubeClient:               kubeClient,
		dynamicClient:            dynamicClient,
		eventRecorder:            eventRecorder,
		queue:                    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "TargetConfigReconciler"),
		protectedNamespaces:      protectedNamespaces,
		deschedulerImagePullSpec: deschedulerImagePullSpec,
		softtainterImagePullSpec: softtainterImagePullSpec,
		configSchedulerLister:    configInformer.Config().V1().Schedulers().Lister(),
		routeRouteLister:         routeInformers.Route().V1().Routes().Lister(),
	}
	configInformer.Config().V1().Schedulers().Informer().AddEventHandler(c.eventHandler())
	routeInformers.Route().V1().Routes().Informer().AddEventHandler(c.eventHandler())
	operatorClientInformer.Informer().AddEventHandler(c.eventHandler())

	return c
}

func (c TargetConfigReconciler) sync() error {
	descheduler, err := c.operatorClient.KubeDeschedulers(operatorclient.OperatorNamespace).Get(c.ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "unable to get operator configuration", "namespace", operatorclient.OperatorNamespace, "kubedescheduler", operatorclient.OperatorConfigName)
		return err
	}

	if descheduler.Spec.DeschedulingIntervalSeconds == nil {
		return fmt.Errorf("descheduler should have an interval set")
	}

	specAnnotations := map[string]string{
		"kubedeschedulers.operator.openshift.io/cluster": strconv.FormatInt(descheduler.Generation, 10),
	}

	configMap, forceDeployment, manageConfigMapErr := c.manageConfigMap(descheduler)
	if manageConfigMapErr != nil {
		// if we returned an error from the configmap AND want to force a deployment
		// it means we want to scale the deployment to 0
		if forceDeployment {
			klog.ErrorS(manageConfigMapErr, "Error managing targetConfig")
			_, err = c.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).UpdateScale(
				c.ctx,
				operatorclient.OperandName,
				&autoscalingv1.Scale{
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorclient.OperandName,
						Namespace: operatorclient.OperatorNamespace,
					},
					Spec: autoscalingv1.ScaleSpec{
						Replicas: 0,
					},
				},
				metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			_, _, err = v1helpers.UpdateStatus(c.ctx, c.deschedulerClient,
				v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
					Type:   "TargetConfigControllerDegraded",
					Status: operatorv1.ConditionTrue,
					Reason: manageConfigMapErr.Error(),
				}))
			return err
		}
		return manageConfigMapErr
	} else {
		resourceVersion := "0"
		if configMap != nil { // SyncConfigMap can return nil
			resourceVersion = configMap.ObjectMeta.ResourceVersion
		}
		specAnnotations["configmaps/cluster"] = resourceVersion
	}

	if service, _, err := c.manageService(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if service != nil { // SyncConfigMap can return nil
			resourceVersion = service.ObjectMeta.ResourceVersion
		}
		specAnnotations["services/metrics"] = resourceVersion
	}

	if sa, _, err := c.manageServiceAccount(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if sa != nil { // SyncConfigMap can return nil
			resourceVersion = sa.ObjectMeta.ResourceVersion
		}
		specAnnotations["serviceaccounts/openshift-descheduler-operand"] = resourceVersion
	}

	if stsa, _, err := c.manageSoftTainterServiceAccount(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if stsa != nil { // SyncConfigMap can return nil
			resourceVersion = stsa.ObjectMeta.ResourceVersion
		}
		specAnnotations["serviceaccounts/openshift-descheduler-softtainter"] = resourceVersion
	}

	if clusterRole, _, err := c.manageClusterRole(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if clusterRole != nil { // SyncConfigMap can return nil
			resourceVersion = clusterRole.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterroles/openshift-descheduler-operand"] = resourceVersion
	}

	if stClusterRole, _, err := c.manageSoftTainterClusterRole(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if stClusterRole != nil { // SyncConfigMap can return nil
			resourceVersion = stClusterRole.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterroles/openshift-descheduler-softtainter"] = resourceVersion
	}

	if clusterRoleBinding, _, err := c.manageClusterRoleBinding(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if clusterRoleBinding != nil { // SyncConfigMap can return nil
			resourceVersion = clusterRoleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterrolebindings/openshift-descheduler-operand"] = resourceVersion
	}

	if stClusterRoleBinding, _, err := c.manageSoftTainterClusterRoleBinding(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if stClusterRoleBinding != nil { // SyncConfigMap can return nil
			resourceVersion = stClusterRoleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterrolebindings/openshift-descheduler-softtainter"] = resourceVersion
	}

	// TODO: manage softtainter VAP

	if clusterMonitoringViewClusterRoleBinding, _, err := c.manageClusterMonitoringViewClusterRoleBinding(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if clusterMonitoringViewClusterRoleBinding != nil { // SyncConfigMap can return nil
			resourceVersion = clusterMonitoringViewClusterRoleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterrolebindings/openshift-descheduler-operand"] = resourceVersion
	}

	if role, _, err := c.manageRole(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if role != nil { // SyncConfigMap can return nil
			resourceVersion = role.ObjectMeta.ResourceVersion
		}
		specAnnotations["roles/prometheus-k8s"] = resourceVersion
	}

	if roleBinding, _, err := c.manageRoleBinding(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if roleBinding != nil { // SyncConfigMap can return nil
			resourceVersion = roleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["rolebindings/prometheus-k8s"] = resourceVersion
	}

	if _, err := c.manageServiceMonitor(descheduler); err != nil {
		return err
	}

	deschedulerDeployment, _, err := c.manageDeschedulerDeployment(descheduler, specAnnotations)
	if err != nil {
		return err
	}

	softTainterDeployment, _, err := c.manageSoftTainterDeployment(descheduler, specAnnotations)
	if err != nil {
		return err
	}

	_, _, err = v1helpers.UpdateStatus(c.ctx, c.deschedulerClient,
		v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
			Type:   "TargetConfigControllerDegraded",
			Status: operatorv1.ConditionFalse,
		}),
		func(status *operatorv1.OperatorStatus) error {
			resourcemerge.SetDeploymentGeneration(&status.Generations, deschedulerDeployment)
			return nil
		},
		func(status *operatorv1.OperatorStatus) error {
			resourcemerge.SetDeploymentGeneration(&status.Generations, softTainterDeployment)
			return nil
		})
	return err
}

func (c *TargetConfigReconciler) manageClusterRole(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.ClusterRole, bool, error) {
	required := resourceread.ReadClusterRoleV1OrDie(bindata.MustAsset("assets/kube-descheduler/operandclusterrole.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyClusterRole(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageSoftTainterClusterRole(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.ClusterRole, bool, error) {
	required := resourceread.ReadClusterRoleV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtainterclusterrole.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyClusterRole(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageSoftTainterClusterRoleBinding(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.ClusterRoleBinding, bool, error) {
	required := resourceread.ReadClusterRoleBindingV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtainterclusterrolebinding.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyClusterRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageClusterRoleBinding(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.ClusterRoleBinding, bool, error) {
	required := resourceread.ReadClusterRoleBindingV1OrDie(bindata.MustAsset("assets/kube-descheduler/operandclusterrolebinding.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyClusterRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageClusterMonitoringViewClusterRoleBinding(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.ClusterRoleBinding, bool, error) {
	required := resourceread.ReadClusterRoleBindingV1OrDie(bindata.MustAsset("assets/kube-descheduler/operandclusterrolebindingprometheus.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyClusterRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageRole(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.Role, bool, error) {
	required := resourceread.ReadRoleV1OrDie(bindata.MustAsset("assets/kube-descheduler/role.yaml"))
	required.Namespace = descheduler.Namespace
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyRole(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageRoleBinding(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.RoleBinding, bool, error) {
	required := resourceread.ReadRoleBindingV1OrDie(bindata.MustAsset("assets/kube-descheduler/rolebinding.yaml"))
	required.Namespace = descheduler.Namespace
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageServiceAccount(descheduler *deschedulerv1.KubeDescheduler) (*v1.ServiceAccount, bool, error) {
	required := resourceread.ReadServiceAccountV1OrDie(bindata.MustAsset("assets/kube-descheduler/operandserviceaccount.yaml"))
	required.Namespace = descheduler.Namespace
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyServiceAccount(c.ctx, c.kubeClient.CoreV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageSoftTainterServiceAccount(descheduler *deschedulerv1.KubeDescheduler) (*v1.ServiceAccount, bool, error) {
	required := resourceread.ReadServiceAccountV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtainterserviceaccount.yaml"))
	required.Namespace = descheduler.Namespace
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyServiceAccount(c.ctx, c.kubeClient.CoreV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageService(descheduler *deschedulerv1.KubeDescheduler) (*v1.Service, bool, error) {
	required := resourceread.ReadServiceV1OrDie(bindata.MustAsset("assets/kube-descheduler/service.yaml"))
	required.Namespace = descheduler.Namespace
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	return resourceapply.ApplyService(c.ctx, c.kubeClient.CoreV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageServiceMonitor(descheduler *deschedulerv1.KubeDescheduler) (bool, error) {
	required := resourceread.ReadUnstructuredOrDie(bindata.MustAsset("assets/kube-descheduler/servicemonitor.yaml"))
	_, changed, err := resourceapply.ApplyKnownUnstructured(c.ctx, c.dynamicClient, c.eventRecorder, required)
	return changed, err
}

func defaultEvictorOverrides(profileCustomizations *deschedulerv1.ProfileCustomizations, pluginConfig *v1alpha2.PluginConfig) error {
	// set priority class threshold if customized
	if profileCustomizations.ThresholdPriority != nil && profileCustomizations.ThresholdPriorityClassName != "" {
		return fmt.Errorf("It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields")
	}

	if profileCustomizations.ThresholdPriority != nil || profileCustomizations.ThresholdPriorityClassName != "" {
		pluginConfig.Args.Object.(*defaultevictor.DefaultEvictorArgs).PriorityThreshold = &deschedulerapi.PriorityThreshold{
			Value: profileCustomizations.ThresholdPriority,
			Name:  profileCustomizations.ThresholdPriorityClassName,
		}
	}

	return nil
}

func affinityAndTaintsProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile := &v1alpha2.DeschedulerProfile{
		Name: string(deschedulerv1.AffinityAndTaints),
		PluginConfigs: []v1alpha2.PluginConfig{
			{
				Name: removepodsviolatinginterpodantiaffinity.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodsviolatinginterpodantiaffinity.RemovePodsViolatingInterPodAntiAffinityArgs{},
				},
			},
			{
				Name: removepodsviolatingnodetaints.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodsviolatingnodetaints.RemovePodsViolatingNodeTaintsArgs{},
				},
			},
			{
				Name: removepodsviolatingnodeaffinity.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodsviolatingnodeaffinity.RemovePodsViolatingNodeAffinityArgs{
						NodeAffinityType: []string{"requiredDuringSchedulingIgnoredDuringExecution"},
					},
				},
			},
			{
				Name: defaultevictor.PluginName,
				Args: runtime.RawExtension{
					Object: &defaultevictor.DefaultEvictorArgs{
						IgnorePvcPods:         ignorePVCPods,
						EvictLocalStoragePods: evictLocalStoragePods,
					},
				},
			},
		},
		Plugins: v1alpha2.Plugins{
			Filter: v1alpha2.PluginSet{
				Enabled: []string{
					defaultevictor.PluginName,
				},
			},
			Deschedule: v1alpha2.PluginSet{
				Enabled: []string{
					removepodsviolatinginterpodantiaffinity.PluginName,
					removepodsviolatingnodetaints.PluginName,
					removepodsviolatingnodeaffinity.PluginName,
				},
			},
		},
	}

	// exclude openshift namespaces from descheduling
	if len(includedNamespaces) > 0 || len(excludedNamespaces) > 0 {
		profile.PluginConfigs[0].Args.Object.(*removepodsviolatinginterpodantiaffinity.RemovePodsViolatingInterPodAntiAffinityArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		profile.PluginConfigs[1].Args.Object.(*removepodsviolatingnodetaints.RemovePodsViolatingNodeTaintsArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		profile.PluginConfigs[2].Args.Object.(*removepodsviolatingnodeaffinity.RemovePodsViolatingNodeAffinityArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
	}

	if profileCustomizations == nil {
		return profile, nil
	}

	if err := defaultEvictorOverrides(profileCustomizations, &profile.PluginConfigs[3]); err != nil {
		return nil, err
	}

	return profile, nil
}

func topologyAndDuplicatesProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile := &v1alpha2.DeschedulerProfile{
		Name: string(deschedulerv1.TopologyAndDuplicates),
		PluginConfigs: []v1alpha2.PluginConfig{
			{
				Name: removepodsviolatingtopologyspreadconstraint.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodsviolatingtopologyspreadconstraint.RemovePodsViolatingTopologySpreadConstraintArgs{
						Constraints: []v1.UnsatisfiableConstraintAction{v1.DoNotSchedule},
					},
				},
			},
			{
				Name: removeduplicates.PluginName,
				Args: runtime.RawExtension{
					Object: &removeduplicates.RemoveDuplicatesArgs{},
				},
			},
			{
				Name: defaultevictor.PluginName,
				Args: runtime.RawExtension{
					Object: &defaultevictor.DefaultEvictorArgs{
						IgnorePvcPods:         ignorePVCPods,
						EvictLocalStoragePods: evictLocalStoragePods,
					},
				},
			},
		},
		Plugins: v1alpha2.Plugins{
			Filter: v1alpha2.PluginSet{
				Enabled: []string{
					defaultevictor.PluginName,
				},
			},
			Balance: v1alpha2.PluginSet{
				Enabled: []string{
					removepodsviolatingtopologyspreadconstraint.PluginName,
					removeduplicates.PluginName,
				},
			},
		},
	}

	// exclude openshift namespaces from descheduling
	if len(includedNamespaces) > 0 || len(excludedNamespaces) > 0 {
		profile.PluginConfigs[0].Args.Object.(*removepodsviolatingtopologyspreadconstraint.RemovePodsViolatingTopologySpreadConstraintArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		profile.PluginConfigs[1].Args.Object.(*removeduplicates.RemoveDuplicatesArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
	}

	if profileCustomizations == nil {
		return profile, nil
	}

	if err := defaultEvictorOverrides(profileCustomizations, &profile.PluginConfigs[2]); err != nil {
		return nil, err
	}

	return profile, nil
}

func softTopologyAndDuplicatesProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile, err := topologyAndDuplicatesProfile(profileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
	if err != nil {
		return profile, err
	}
	profile.Name = string(deschedulerv1.SoftTopologyAndDuplicates)
	profile.PluginConfigs[0].Args.Object.(*removepodsviolatingtopologyspreadconstraint.RemovePodsViolatingTopologySpreadConstraintArgs).Constraints = []v1.UnsatisfiableConstraintAction{v1.DoNotSchedule, v1.ScheduleAnyway}
	return profile, err
}

func lifecycleAndUtilizationProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile := &v1alpha2.DeschedulerProfile{
		Name: string(deschedulerv1.LifecycleAndUtilization),
		PluginConfigs: []v1alpha2.PluginConfig{
			{
				Name: podlifetime.PluginName,
				Args: runtime.RawExtension{
					Object: &podlifetime.PodLifeTimeArgs{
						MaxPodLifeTimeSeconds: utilptr.To[uint](86400), // 24 hours
					},
				},
			},
			{
				Name: removepodshavingtoomanyrestarts.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodshavingtoomanyrestarts.RemovePodsHavingTooManyRestartsArgs{
						PodRestartThreshold:     100,
						IncludingInitContainers: true,
					},
				},
			},
			{
				Name: nodeutilization.LowNodeUtilizationPluginName,
				Args: runtime.RawExtension{
					Object: &nodeutilization.LowNodeUtilizationArgs{
						Thresholds:       deschedulerapi.ResourceThresholds{},
						TargetThresholds: deschedulerapi.ResourceThresholds{},
					},
				},
			},
			{
				Name: defaultevictor.PluginName,
				Args: runtime.RawExtension{
					Object: &defaultevictor.DefaultEvictorArgs{
						IgnorePvcPods:         ignorePVCPods,
						EvictLocalStoragePods: evictLocalStoragePods,
					},
				},
			},
		},
		Plugins: v1alpha2.Plugins{
			Filter: v1alpha2.PluginSet{
				Enabled: []string{
					defaultevictor.PluginName,
				},
			},
			Deschedule: v1alpha2.PluginSet{
				Enabled: []string{
					podlifetime.PluginName,
					removepodshavingtoomanyrestarts.PluginName,
				},
			},
			Balance: v1alpha2.PluginSet{
				Enabled: []string{
					nodeutilization.LowNodeUtilizationPluginName,
				},
			},
		},
	}

	// exclude openshift namespaces from descheduling
	if len(includedNamespaces) > 0 || len(excludedNamespaces) > 0 {
		profile.PluginConfigs[0].Args.Object.(*podlifetime.PodLifeTimeArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		profile.PluginConfigs[1].Args.Object.(*removepodshavingtoomanyrestarts.RemovePodsHavingTooManyRestartsArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		if len(includedNamespaces) > 0 {
			// log a warning if user tries to enable ns inclusion with a profile that activates LowNodeUtilization
			klog.Warning("LowNodeUtilization is enabled, however it does not support namespace inclusion. Namespace inclusion will only be considered by other strategies (like RemovePodsHavingTooManyRestarts and PodLifeTime)")
		}
		if len(excludedNamespaces) > 0 {
			profile.PluginConfigs[2].Args.Object.(*nodeutilization.LowNodeUtilizationArgs).EvictableNamespaces = &deschedulerapi.Namespaces{
				Exclude: excludedNamespaces,
			}
		}
	}

	resourceNames := []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory, v1.ResourcePods}
	lowThreshold := deschedulerapi.Percentage(20)
	highThreshold := deschedulerapi.Percentage(50)
	args := profile.PluginConfigs[2].Args.Object.(*nodeutilization.LowNodeUtilizationArgs)

	if profileCustomizations != nil {
		if profileCustomizations.PodLifetime != nil {
			profile.PluginConfigs[0].Args.Object.(*podlifetime.PodLifeTimeArgs).MaxPodLifeTimeSeconds = utilptr.To[uint](uint(profileCustomizations.PodLifetime.Seconds()))
		}

		if profileCustomizations.DevLowNodeUtilizationThresholds != nil {
			switch *profileCustomizations.DevLowNodeUtilizationThresholds {
			case deschedulerv1.LowThreshold:
				lowThreshold = 10
				highThreshold = 30
			case deschedulerv1.MediumThreshold, "":
				lowThreshold = 20
				highThreshold = 50
			case deschedulerv1.HighThreshold:
				lowThreshold = 40
				highThreshold = 70
			default:
				return nil, fmt.Errorf("unknown Descheduler LowNodeUtilization threshold %v, only 'Low', 'Medium' and 'High' are supported", *profileCustomizations.DevLowNodeUtilizationThresholds)
			}
		}

		if profileCustomizations.DevActualUtilizationProfile != "" {
			query := ""
			switch profileCustomizations.DevActualUtilizationProfile {
			case deschedulerv1.PrometheusCPUUsageProfile:
				query = "instance:node_cpu:rate:sum"
			case deschedulerv1.PrometheusCPUPSIPressureProfile:
				query = "rate(node_pressure_cpu_waiting_seconds_total[1m])"
			case deschedulerv1.PrometheusCPUPSIPressureByUtilizationProfile:
				query = "avg by (instance) ( rate(node_pressure_cpu_waiting_seconds_total[1m])) and (1 - avg by (instance) (rate(node_cpu_seconds_total{mode=\"idle\"}[1m]))) > 0.7 or avg by (instance) ( rate(node_pressure_cpu_waiting_seconds_total[1m])) * 0"
			case deschedulerv1.PrometheusMemoryPSIPressureProfile:
				query = "rate(node_pressure_memory_waiting_seconds_total[1m])"
			case deschedulerv1.PrometheusIOPSIPressureProfile:
				query = "rate(node_pressure_io_waiting_seconds_total[1m])"
			default:
				if !strings.HasPrefix(string(profileCustomizations.DevActualUtilizationProfile), "query:") {
					return nil, fmt.Errorf("unknown prometheus profile: %v", profileCustomizations.DevActualUtilizationProfile)
				}
				query = strings.TrimPrefix(string(profileCustomizations.DevActualUtilizationProfile), "query:")
			}
			args.MetricsUtilization.Prometheus = nodeutilization.Prometheus{
				Query: query,
			}
			resourceNames = []v1.ResourceName{nodeutilization.ResourceMetrics}
		}

		if err := defaultEvictorOverrides(profileCustomizations, &profile.PluginConfigs[3]); err != nil {
			return nil, err
		}
	}

	for _, resourceName := range resourceNames {
		args.Thresholds[resourceName] = lowThreshold
		args.TargetThresholds[resourceName] = highThreshold
	}

	return profile, nil
}

func longLifecycleProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile, err := lifecycleAndUtilizationProfile(profileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
	if err != nil {
		return profile, err
	}
	profile.PluginConfigs = profile.PluginConfigs[1:]
	profile.Plugins.Deschedule.Enabled = profile.Plugins.Deschedule.Enabled[1:]
	profile.Name = string(deschedulerv1.LongLifecycle)
	return profile, err
}

func compactAndScaleProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile := &v1alpha2.DeschedulerProfile{
		Name: string(deschedulerv1.CompactAndScale),
		PluginConfigs: []v1alpha2.PluginConfig{
			{
				Name: nodeutilization.HighNodeUtilizationPluginName,
				Args: runtime.RawExtension{
					Object: &nodeutilization.HighNodeUtilizationArgs{
						Thresholds: deschedulerapi.ResourceThresholds{
							v1.ResourceCPU:    20,
							v1.ResourceMemory: 20,
							v1.ResourcePods:   20,
						},
					},
				},
			},
			{
				Name: defaultevictor.PluginName,
				Args: runtime.RawExtension{
					Object: &defaultevictor.DefaultEvictorArgs{
						IgnorePvcPods:         ignorePVCPods,
						EvictLocalStoragePods: evictLocalStoragePods,
					},
				},
			},
		},
		Plugins: v1alpha2.Plugins{
			Filter: v1alpha2.PluginSet{
				Enabled: []string{
					defaultevictor.PluginName,
				},
			},
			Balance: v1alpha2.PluginSet{
				Enabled: []string{
					nodeutilization.HighNodeUtilizationPluginName,
				},
			},
		},
	}

	// exclude openshift namespaces from descheduling
	if len(includedNamespaces) > 0 || len(excludedNamespaces) > 0 {
		if len(includedNamespaces) > 0 {
			// log a warning if user tries to enable ns inclusion with a profile that activates LowNodeUtilization
			klog.Warning("HighNodeUtilization is enabled, however it does not support namespace inclusion. Namespace inclusion will only be considered by other strategies (like RemovePodsHavingTooManyRestarts and PodLifeTime)")
		}
		if len(excludedNamespaces) > 0 {
			profile.PluginConfigs[0].Args.Object.(*nodeutilization.HighNodeUtilizationArgs).EvictableNamespaces = &deschedulerapi.Namespaces{
				Exclude: excludedNamespaces,
			}
		}
	}

	if profileCustomizations == nil {
		return profile, nil
	}

	if profileCustomizations.DevHighNodeUtilizationThresholds != nil {
		args := profile.PluginConfigs[0].Args.Object.(*nodeutilization.HighNodeUtilizationArgs)
		switch *profileCustomizations.DevHighNodeUtilizationThresholds {
		case deschedulerv1.CompactMinimalThreshold:
			args.Thresholds[v1.ResourceCPU] = 10
			args.Thresholds[v1.ResourceMemory] = 10
			args.Thresholds[v1.ResourcePods] = 10
		case deschedulerv1.CompactModestThreshold, "":
			args.Thresholds[v1.ResourceCPU] = 20
			args.Thresholds[v1.ResourceMemory] = 20
			args.Thresholds[v1.ResourcePods] = 20
		case deschedulerv1.CompactModerateThreshold:
			args.Thresholds[v1.ResourceCPU] = 30
			args.Thresholds[v1.ResourceMemory] = 30
			args.Thresholds[v1.ResourcePods] = 30
		default:
			return nil, fmt.Errorf("unknown Descheduler HighNodeUtilization threshold %v, only 'Minimal', 'Modest' and 'Moderate' are supported", *profileCustomizations.DevHighNodeUtilizationThresholds)
		}
	}

	if err := defaultEvictorOverrides(profileCustomizations, &profile.PluginConfigs[1]); err != nil {
		return nil, err
	}

	return profile, nil
}

func (c *TargetConfigReconciler) manageConfigMap(descheduler *deschedulerv1.KubeDescheduler) (*v1.ConfigMap, bool, error) {
	required := resourceread.ReadConfigMapV1OrDie(bindata.MustAsset("assets/kube-descheduler/configmap.yaml"))
	required.Name = descheduler.Name
	required.Namespace = descheduler.Namespace
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)

	scheduler, err := c.configSchedulerLister.Get("cluster")
	if err != nil {
		return nil, false, err
	}

	// override the included/excluded namespace
	excludedNamespaces := c.protectedNamespaces
	protectedNamespacesSet := sets.NewString(c.protectedNamespaces...)
	includedNamespaces := []string{}
	if descheduler.Spec.ProfileCustomizations != nil {
		if len(descheduler.Spec.ProfileCustomizations.Namespaces.Excluded) > 0 && len(descheduler.Spec.ProfileCustomizations.Namespaces.Included) > 0 {
			return nil, false, fmt.Errorf("It is forbidden to combine both included and excluded namespaces")
		}
		if len(descheduler.Spec.ProfileCustomizations.Namespaces.Included) > 0 {
			for _, ns := range descheduler.Spec.ProfileCustomizations.Namespaces.Included {
				if protectedNamespacesSet.Has(ns) {
					return nil, false, fmt.Errorf("Protected namespace %v included. It is forbidden to include any of the protected namespaces from %v", ns, c.protectedNamespaces)
				}
				includedNamespaces = append(includedNamespaces, ns)
			}
			excludedNamespaces = []string{}
		}
		for _, ns := range descheduler.Spec.ProfileCustomizations.Namespaces.Excluded {
			excludedNamespaces = append(excludedNamespaces, ns)
		}
	}

	// parse whatever profiles are set into their policy representations then merge them into one file
	if len(descheduler.Spec.Profiles) == 0 {
		return nil, false, fmt.Errorf("descheduler should have at least 1 profile enabled")
	}

	policy := &v1alpha2.DeschedulerPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DeschedulerPolicy",
			APIVersion: "descheduler/v1alpha2",
		},
		Profiles: []v1alpha2.DeschedulerProfile{},
	}

	if descheduler.Spec.EvictionLimits != nil {
		if descheduler.Spec.EvictionLimits.Total != nil {
			policy.MaxNoOfPodsToEvictTotal = utilptr.To[uint](uint(*descheduler.Spec.EvictionLimits.Total))
		}
	}

	if descheduler.Spec.ProfileCustomizations != nil && descheduler.Spec.ProfileCustomizations.DevActualUtilizationProfile != "" {
		// detect the prometheus server url
		route, err := c.routeRouteLister.Routes("openshift-monitoring").Get("prometheus-k8s")
		if err != nil {
			return nil, false, fmt.Errorf("unable to get openshift-monitoring/prometheus-k8s route: %v", err)
		}
		if len(route.Status.Ingress) == 0 {
			return nil, false, fmt.Errorf("No ingress found in openshift-monitoring/prometheus-k8s route")
		}
		if route.Status.Ingress[0].Host == "" {
			return nil, false, fmt.Errorf("Host for status.ingress[0] in openshift-monitoring/prometheus-k8s route is empty")
		}
		klog.InfoS("Detecting prometheus server url", "url", route.Status.Ingress[0].Host)
		policy.Prometheus = v1alpha2.Prometheus{
			URL: "https://" + route.Status.Ingress[0].Host,
		}
	}

	// ignore PVC pods by default
	ignorePVCPods := true
	evictLocalStoragePods := false
	for _, profileName := range descheduler.Spec.Profiles {
		if profileName == deschedulerv1.EvictPodsWithPVC {
			ignorePVCPods = false
			continue
		}
		if profileName == deschedulerv1.EvictPodsWithLocalStorage {
			evictLocalStoragePods = true
			continue
		}
	}

	profiles := sets.NewString()
	for _, profileName := range descheduler.Spec.Profiles {
		if err := checkProfileConflicts(profiles, profileName, scheduler); err != nil {
			klog.ErrorS(err, "Profile conflict")
			return nil, true, err
		}
		var profile *v1alpha2.DeschedulerProfile
		var err error
		switch profileName {
		case deschedulerv1.AffinityAndTaints:
			profile, err = affinityAndTaintsProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.TopologyAndDuplicates:
			profile, err = topologyAndDuplicatesProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.SoftTopologyAndDuplicates:
			profile, err = softTopologyAndDuplicatesProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.LifecycleAndUtilization:
			profile, err = lifecycleAndUtilizationProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.EvictPodsWithLocalStorage, deschedulerv1.EvictPodsWithPVC:
			continue
		case deschedulerv1.DevPreviewLongLifecycle, deschedulerv1.LongLifecycle:
			profile, err = longLifecycleProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.CompactAndScale:
			profile, err = compactAndScaleProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		default:
			err = fmt.Errorf("Profile %q not recognized", profileName)
		}
		if err != nil {
			return nil, false, err
		}
		policy.Profiles = append(policy.Profiles, *profile)
	}

	// Check for conflicting kube-scheduler config
	if scheduler.Spec.Profile == configv1.HighNodeUtilization &&
		(profiles.Has(string(deschedulerv1.LifecycleAndUtilization)) || profiles.Has(string(deschedulerv1.DevPreviewLongLifecycle)) || profiles.Has(string(deschedulerv1.LongLifecycle))) {
		// force a new deployment so we can scale it to 0
		return nil, true, fmt.Errorf("enabling Descheduler LowNodeUtilization with Scheduler HighNodeUtilization may cause an eviction/scheduling hot loop")
	}

	if scheduler.Spec.Profile == configv1.LowNodeUtilization &&
		profiles.Has(string(deschedulerv1.CompactAndScale)) {
		// force a new deployment so we can scale it to 0
		return nil, true, fmt.Errorf("enabling Descheduler CompactAndScale with Scheduler LowNodeUtilization may cause an eviction/scheduling hot loop")
	}

	policyBytes, err := yaml.Marshal(policy)
	if err != nil {
		return nil, false, err
	}
	required.Data = map[string]string{"policy.yaml": string(policyBytes)}
	return resourceapply.ApplyConfigMap(c.ctx, c.kubeClient.CoreV1(), c.eventRecorder, required)
}

// checkProfileConflicts ensures that multiple profiles aren't redeclared
// it also checks for various inter-profile conflicts (profiles which should not be enabled simultaneously)
func checkProfileConflicts(profiles sets.String, profileName deschedulerv1.DeschedulerProfile, scheduler *configv1.Scheduler) error {
	if profiles.Has(string(profileName)) {
		return fmt.Errorf("profile %s already declared, ignoring", profileName)
	} else {
		profiles.Insert(string(profileName))
	}

	// DevPreviewLongLifecycle is deprecated in 4.17, remove in 4.19+
	if profiles.Has(string(deschedulerv1.DevPreviewLongLifecycle)) && profiles.Has(string(deschedulerv1.LifecycleAndUtilization)) {
		return fmt.Errorf("cannot declare %s and %s profiles simultaneously, ignoring", deschedulerv1.DevPreviewLongLifecycle, deschedulerv1.LifecycleAndUtilization)
	}

	if profiles.Has(string(deschedulerv1.LongLifecycle)) && profiles.Has(string(deschedulerv1.LifecycleAndUtilization)) {
		return fmt.Errorf("cannot declare %s and %s profiles simultaneously, ignoring", deschedulerv1.LongLifecycle, deschedulerv1.LifecycleAndUtilization)
	}

	if profiles.Has(string(deschedulerv1.SoftTopologyAndDuplicates)) && profiles.Has(string(deschedulerv1.TopologyAndDuplicates)) {
		return fmt.Errorf("cannot declare %s and %s profiles simultaneously, ignoring", deschedulerv1.SoftTopologyAndDuplicates, deschedulerv1.TopologyAndDuplicates)
	}

	if profiles.Has(string(deschedulerv1.CompactAndScale)) && profiles.Has(string(deschedulerv1.LifecycleAndUtilization)) {
		return fmt.Errorf("cannot declare %s and %s profiles simultaneously, ignoring", deschedulerv1.CompactAndScale, deschedulerv1.LifecycleAndUtilization)
	}

	if profiles.Has(string(deschedulerv1.CompactAndScale)) && profiles.Has(string(deschedulerv1.LongLifecycle)) {
		return fmt.Errorf("cannot declare %s and %s profiles simultaneously, ignoring", deschedulerv1.CompactAndScale, deschedulerv1.LongLifecycle)
	}

	if profiles.Has(string(deschedulerv1.CompactAndScale)) && profiles.Has(string(deschedulerv1.DevPreviewLongLifecycle)) {
		return fmt.Errorf("cannot declare %s and %s profiles simultaneously, ignoring", deschedulerv1.CompactAndScale, deschedulerv1.DevPreviewLongLifecycle)
	}

	if profiles.Has(string(deschedulerv1.CompactAndScale)) && profiles.Has(string(deschedulerv1.TopologyAndDuplicates)) {
		return fmt.Errorf("cannot declare %s and %s profiles simultaneously, ignoring", deschedulerv1.CompactAndScale, deschedulerv1.TopologyAndDuplicates)
	}
	return nil
}

func (c *TargetConfigReconciler) manageDeployment(required *appsv1.Deployment, descheduler *deschedulerv1.KubeDescheduler, targetImagePullSpec string, specAnnotations map[string]string) (*appsv1.Deployment, bool, error) {
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.OwnerReferences = []metav1.OwnerReference{
		ownerReference,
	}
	controller.EnsureOwnerRef(required, ownerReference)
	replicas := int32(1)
	required.Spec.Replicas = &replicas
	required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args,
		fmt.Sprintf("--descheduling-interval=%ss", strconv.Itoa(int(*descheduler.Spec.DeschedulingIntervalSeconds))))

	featureGates := []string{}
	if descheduler.Spec.ProfileCustomizations != nil && descheduler.Spec.ProfileCustomizations.DevEnableEvictionsInBackground {
		featureGates = append(featureGates, "EvictionsInBackground=true")
	}

	if len(featureGates) > 0 {
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("--feature-gates=%s", strings.Join(featureGates, ",")))
	}

	var observedConfig map[string]interface{}
	if err := yaml.Unmarshal(descheduler.Spec.ObservedConfig.Raw, &observedConfig); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal the observedConfig: %v", err)
	}

	cipherSuites, cipherSuitesFound, err := unstructured.NestedStringSlice(observedConfig, "servingInfo", "cipherSuites")
	if err != nil {
		return nil, false, fmt.Errorf("couldn't get the servingInfo.cipherSuites config from observedConfig: %v", err)
	}

	minTLSVersion, minTLSVersionFound, err := unstructured.NestedString(observedConfig, "servingInfo", "minTLSVersion")
	if err != nil {
		return nil, false, fmt.Errorf("couldn't get the servingInfo.minTLSVersion config from observedConfig: %v", err)
	}

	if cipherSuitesFound && len(cipherSuites) > 0 {
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("--tls-cipher-suites=%s", strings.Join(cipherSuites, ",")))
	}

	if minTLSVersionFound && len(minTLSVersion) > 0 {
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("--tls-min-version=%s", minTLSVersion))
	}

	required.Spec.Template.Spec.Volumes[0].VolumeSource.ConfigMap.LocalObjectReference.Name = descheduler.Name

	if len(descheduler.Spec.Mode) > 0 {
		switch descheduler.Spec.Mode {
		case deschedulerv1.Automatic:
			// No additional flags/configuration for now
		case deschedulerv1.Predictive:
			// Run the simulator in the dry mode (metrics are enabled by default)
			required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, "--dry-run=true")
		default:
			return nil, false, fmt.Errorf("descheduler mode %v not recognized", descheduler.Spec.Mode)
		}
	}

	images := map[string]string{
		"${IMAGE}": targetImagePullSpec,
	}
	for i := range required.Spec.Template.Spec.Containers {
		for pat, img := range images {
			if required.Spec.Template.Spec.Containers[i].Image == pat {
				required.Spec.Template.Spec.Containers[i].Image = img
				break
			}
		}
	}

	switch descheduler.Spec.LogLevel {
	case operatorv1.Normal:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 2))
	case operatorv1.Debug:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 4))
	case operatorv1.Trace:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 6))
	case operatorv1.TraceAll:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 8))
	default:
		required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", 2))
	}

	resourcemerge.MergeMap(resourcemerge.BoolPtr(false), &required.Spec.Template.Annotations, specAnnotations)

	return resourceapply.ApplyDeployment(
		c.ctx,
		c.kubeClient.AppsV1(),
		c.eventRecorder,
		required,
		resourcemerge.ExpectedDeploymentGeneration(required, descheduler.Status.Generations))
}

func (c *TargetConfigReconciler) manageDeschedulerDeployment(descheduler *deschedulerv1.KubeDescheduler, specAnnotations map[string]string) (*appsv1.Deployment, bool, error) {
	required := resourceread.ReadDeploymentV1OrDie(bindata.MustAsset("assets/kube-descheduler/deployment.yaml"))
	required.Name = operatorclient.OperandName
	required.Namespace = descheduler.Namespace
	return c.manageDeployment(required, descheduler, c.deschedulerImagePullSpec, specAnnotations)
}

func (c *TargetConfigReconciler) manageSoftTainterDeployment(descheduler *deschedulerv1.KubeDescheduler, specAnnotations map[string]string) (*appsv1.Deployment, bool, error) {
	required := resourceread.ReadDeploymentV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtainterdeployment.yaml"))
	required.Name = operatorclient.SoftTainterOperandName
	required.Namespace = descheduler.Namespace
	return c.manageDeployment(required, descheduler, c.softtainterImagePullSpec, specAnnotations)
}

// Run starts the kube-scheduler and blocks until stopCh is closed.
func (c *TargetConfigReconciler) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting TargetConfigReconciler")
	defer klog.Infof("Shutting down TargetConfigReconciler")

	// doesn't matter what workers say, only start one.
	go wait.Until(c.runWorker, time.Second, stopCh)

	<-stopCh
}

func (c *TargetConfigReconciler) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *TargetConfigReconciler) processNextWorkItem() bool {
	dsKey, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(dsKey)

	err := c.sync()
	if err == nil {
		c.queue.Forget(dsKey)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("%v failed with : %v", dsKey, err))
	c.queue.AddRateLimited(dsKey)

	return true
}

// eventHandler queues the operator to check spec and status
func (c *TargetConfigReconciler) eventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(workQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(workQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(workQueueKey) },
	}
}
