package operator

import (
	"context"
	"fmt"
	"os"
	"slices"
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
	operatorprofiles "github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/profiles"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/softtainter"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/openshift/library-go/pkg/controller"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	coreinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"k8s.io/kube-openapi/pkg/validation/strfmt"
	"k8s.io/kube-openapi/pkg/validation/validate"
	"k8s.io/kubernetes/pkg/util/taints"
)

const DefaultImage = "quay.io/openshift/origin-descheduler:latest"
const kubeVirtShedulableLabelSelector = "kubevirt.io/schedulable=true"
const psiPath = "/proc/pressure/"
const EXPERIMENTAL_DISABLE_PSI_CHECK = "EXPERIMENTAL_DISABLE_PSI_CHECK"

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
	namespaceLister          corev1listers.NamespaceLister
	nodeLister               corev1listers.NodeLister
	cache                    resourceapply.ResourceCache
	psiPath                  string
}

func NewTargetConfigReconciler(
	ctx context.Context,
	deschedulerImagePullSpec string,
	softTainterImagePullSpec string,
	operatorConfigClient operatorconfigclientv1.KubedeschedulersV1Interface,
	operatorClientInformer operatorclientinformers.KubeDeschedulerInformer,
	deschedulerClient *operatorclient.DeschedulerClient,
	kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	configInformer configinformers.SharedInformerFactory,
	routeInformers routeinformers.SharedInformerFactory,
	coreInformers coreinformers.SharedInformerFactory,
	eventRecorder events.Recorder,
) *TargetConfigReconciler {
	// make sure our list of excluded system namespaces is up to date
	allNamespaces, err := kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.ErrorS(err, "error listing namespaces")
		return nil
	}
	protectedNamespaces := []string{"kube-system", "hypershift", "openshift"}
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
		softtainterImagePullSpec: softTainterImagePullSpec,
		configSchedulerLister:    configInformer.Config().V1().Schedulers().Lister(),
		routeRouteLister:         routeInformers.Route().V1().Routes().Lister(),
		namespaceLister:          coreInformers.Core().V1().Namespaces().Lister(),
		nodeLister:               coreInformers.Core().V1().Nodes().Lister(),
		cache:                    resourceapply.NewResourceCache(),
		psiPath:                  psiPath,
	}

	configInformer.Config().V1().Schedulers().Informer().AddEventHandler(c.eventHandler())
	routeInformers.Route().V1().Routes().Informer().AddEventHandler(c.eventHandler())
	operatorClientInformer.Informer().AddEventHandler(c.eventHandler())
	coreInformers.Core().V1().Nodes().Informer().AddEventHandler(c.eventHandler())
	coreInformers.Core().V1().Namespaces().Informer().AddEventHandler(c.eventHandler())
	return c
}

func (c TargetConfigReconciler) scaleDownDeployment(scaleDownError error) error {
	_, err := c.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).UpdateScale(
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
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	_, _, err = v1helpers.UpdateStatus(c.ctx, c.deschedulerClient,
		v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
			Type:   "TargetConfigControllerDegraded",
			Status: operatorv1.ConditionTrue,
			Reason: scaleDownError.Error(),
		}))
	return err
}

func (c TargetConfigReconciler) sync() error {
	descheduler, err := c.operatorClient.KubeDeschedulers(operatorclient.OperatorNamespace).Get(c.ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "unable to get operator configuration", "namespace", operatorclient.OperatorNamespace, "kubedescheduler", operatorclient.OperatorConfigName)
		return err
	}

	if err := validateDeschedulerCR(descheduler); err != nil {
		klog.ErrorS(err, "descheduler validation failed")
		if err := c.scaleDownDeployment(err); err != nil {
			return fmt.Errorf("error scaling down the deployment: %w", err)
		}
		return fmt.Errorf("descheduler validation failed: %w", err)
	}

	if descheduler.Spec.DeschedulingIntervalSeconds == nil || *descheduler.Spec.DeschedulingIntervalSeconds <= 0 {
		valErr := fmt.Errorf("descheduler should have an interval set and it should be greater than 0")
		if err := c.scaleDownDeployment(valErr); err != nil {
			return fmt.Errorf("error scaling down the deployment: %w", err)
		}
		return valErr
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
			return c.scaleDownDeployment(manageConfigMapErr)
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

	isSoftTainterNeeded, err := c.isSoftTainterNeeded(descheduler)
	if err != nil {
		return err
	}

	if stsa, _, err := c.manageSoftTainterServiceAccount(descheduler, isSoftTainterNeeded); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if stsa != nil {
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

	if stClusterRole, _, err := c.manageSoftTainterClusterRole(descheduler, isSoftTainterNeeded); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if stClusterRole != nil {
			resourceVersion = stClusterRole.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterroles/openshift-descheduler-softtainter"] = resourceVersion
	}

	if clusterRoleBinding, _, err := c.manageClusterRoleBinding(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if clusterRoleBinding != nil {
			resourceVersion = clusterRoleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterrolebindings/openshift-descheduler-operand"] = resourceVersion
	}

	if stClusterRoleBinding, _, err := c.manageSoftTainterClusterRoleBinding(descheduler, isSoftTainterNeeded); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if stClusterRoleBinding != nil {
			resourceVersion = stClusterRoleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterrolebindings/openshift-descheduler-softtainter"] = resourceVersion
	}

	if stRole, _, err := c.manageSoftTainterRole(descheduler, isSoftTainterNeeded); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if stRole != nil {
			resourceVersion = stRole.ObjectMeta.ResourceVersion
		}
		specAnnotations["roles/openshift-descheduler-softtainter"] = resourceVersion
	}

	if stRoleBinding, _, err := c.manageSoftTainterRoleBinding(descheduler, isSoftTainterNeeded); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if stRoleBinding != nil {
			resourceVersion = stRoleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["rolebindings/openshift-descheduler-softtainter"] = resourceVersion
	}

	if prometheusRule, _, err := c.managePrometheusRule(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if prometheusRule != nil {
			resourceVersion = prometheusRule.GetResourceVersion()
		}
		specAnnotations["prometheusrule/descheduler-rules"] = resourceVersion
	}

	if softTainterVAP, _, err := c.manageSoftTainterValidatingAdmissionPolicy(descheduler, isSoftTainterNeeded); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if softTainterVAP != nil {
			resourceVersion = softTainterVAP.GetResourceVersion()
		}
		specAnnotations["validatingadmissionpolicy/openshift-descheduler-softtainter-vap"] = resourceVersion
	}

	if softTainterVAPBinding, _, err := c.manageSoftTainterValidatingAdmissionPolicyBinding(descheduler, isSoftTainterNeeded); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if softTainterVAPBinding != nil {
			resourceVersion = softTainterVAPBinding.GetResourceVersion()
		}
		specAnnotations["validatingadmissionpolicybinding/openshift-descheduler-softtainter-vap-binding"] = resourceVersion
	}

	if clusterMonitoringViewClusterRoleBinding, _, err := c.manageClusterMonitoringViewClusterRoleBinding(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if clusterMonitoringViewClusterRoleBinding != nil { // SyncConfigMap can return nil
			resourceVersion = clusterMonitoringViewClusterRoleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterrolebindings/openshift-descheduler-softtainter"] = resourceVersion
	}

	if softtainterClusterMonitoringViewClusterRoleBinding, _, err := c.manageSoftTainterClusterMonitoringViewClusterRoleBinding(descheduler, isSoftTainterNeeded); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if softtainterClusterMonitoringViewClusterRoleBinding != nil {
			resourceVersion = softtainterClusterMonitoringViewClusterRoleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["clusterrolebindings/openshift-descheduler-operand"] = resourceVersion
	}

	if softtainterPSIAlert, _, err := c.managePSIAlert(descheduler, isSoftTainterNeeded); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if softtainterPSIAlert != nil {
			resourceVersion = softtainterPSIAlert.GetResourceVersion()
		}
		specAnnotations["prometheusrule/descheduler-psi-alert"] = resourceVersion
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

	if operandRole, _, err := c.manageOperandRole(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if operandRole != nil {
			resourceVersion = operandRole.ObjectMeta.ResourceVersion
		}
		specAnnotations["roles/openshift-descheduler-operand"] = resourceVersion
	}

	if operandRoleBinding, _, err := c.manageOperandRoleBinding(descheduler); err != nil {
		return err
	} else {
		resourceVersion := "0"
		if operandRoleBinding != nil {
			resourceVersion = operandRoleBinding.ObjectMeta.ResourceVersion
		}
		specAnnotations["rolebindings/openshift-descheduler-operand"] = resourceVersion
	}

	if _, err := c.manageServiceMonitor(descheduler); err != nil {
		return err
	}

	deschedulerDeployment, _, err := c.manageDeschedulerDeployment(descheduler, specAnnotations)
	if err != nil {
		return err
	}

	softTainterDeployment, _, err := c.manageSoftTainterDeployment(descheduler, specAnnotations, isSoftTainterNeeded)
	if err != nil {
		return err
	}

	statusUpdateFunctions := []v1helpers.UpdateStatusFunc{
		v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
			Type:   "TargetConfigControllerDegraded",
			Status: operatorv1.ConditionFalse,
		}),
		func(status *operatorv1.OperatorStatus) error {
			resourcemerge.SetDeploymentGeneration(&status.Generations, deschedulerDeployment)
			return nil
		},
	}
	if isSoftTainterNeeded {
		statusUpdateFunctions = append(
			statusUpdateFunctions,
			func(status *operatorv1.OperatorStatus) error {
				resourcemerge.SetDeploymentGeneration(&status.Generations, softTainterDeployment)
				return nil
			},
		)
	}
	_, _, err = v1helpers.UpdateStatus(c.ctx, c.deschedulerClient, statusUpdateFunctions...)
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

func (c *TargetConfigReconciler) manageSoftTainterClusterRole(descheduler *deschedulerv1.KubeDescheduler, stEnabled bool) (*rbacv1.ClusterRole, bool, error) {
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
	if stEnabled {
		return resourceapply.ApplyClusterRole(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
	}
	return resourceapply.DeleteClusterRole(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageSoftTainterClusterRoleBinding(descheduler *deschedulerv1.KubeDescheduler, stEnabled bool) (*rbacv1.ClusterRoleBinding, bool, error) {
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
	if stEnabled {
		return resourceapply.ApplyClusterRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
	}
	return resourceapply.DeleteClusterRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
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

func (c *TargetConfigReconciler) managePrometheusRule(descheduler *deschedulerv1.KubeDescheduler) (*unstructured.Unstructured, bool, error) {
	required := resourceread.ReadUnstructuredOrDie(bindata.MustAsset("assets/kube-descheduler/prometheusrule.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.SetOwnerReferences([]metav1.OwnerReference{ownerReference})
	controller.EnsureOwnerRef(required, ownerReference)
	return resourceapply.ApplyKnownUnstructured(c.ctx, c.dynamicClient, c.eventRecorder, required)
}

func (c *TargetConfigReconciler) managePSIAlert(descheduler *deschedulerv1.KubeDescheduler, stEnabled bool) (*unstructured.Unstructured, bool, error) {
	required := resourceread.ReadUnstructuredOrDie(bindata.MustAsset("assets/kube-descheduler/psialert.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.SetOwnerReferences([]metav1.OwnerReference{ownerReference})
	controller.EnsureOwnerRef(required, ownerReference)
	if stEnabled {
		return resourceapply.ApplyKnownUnstructured(c.ctx, c.dynamicClient, c.eventRecorder, required)
	}
	return resourceapply.DeleteKnownUnstructured(c.ctx, c.dynamicClient, c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageSoftTainterValidatingAdmissionPolicy(descheduler *deschedulerv1.KubeDescheduler, stEnabled bool) (*admissionv1.ValidatingAdmissionPolicy, bool, error) {
	required := resourceread.ReadValidatingAdmissionPolicyV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtaintervalidatingadmissionpolicy.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.SetOwnerReferences([]metav1.OwnerReference{ownerReference})
	controller.EnsureOwnerRef(required, ownerReference)
	if stEnabled {
		return resourceapply.ApplyValidatingAdmissionPolicyV1(c.ctx, c.kubeClient.AdmissionregistrationV1(), c.eventRecorder, required, c.cache)
	}
	return DeleteValidatingAdmissionPolicyV1(c.ctx, c.kubeClient.AdmissionregistrationV1(), c.eventRecorder, required)

}

func (c *TargetConfigReconciler) manageSoftTainterValidatingAdmissionPolicyBinding(descheduler *deschedulerv1.KubeDescheduler, stEnabled bool) (*admissionv1.ValidatingAdmissionPolicyBinding, bool, error) {
	required := resourceread.ReadValidatingAdmissionPolicyBindingV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtaintervalidatingadmissionpolicybinding.yaml"))
	ownerReference := metav1.OwnerReference{
		APIVersion: "operator.openshift.io/v1",
		Kind:       "KubeDescheduler",
		Name:       descheduler.Name,
		UID:        descheduler.UID,
	}
	required.SetOwnerReferences([]metav1.OwnerReference{ownerReference})
	controller.EnsureOwnerRef(required, ownerReference)
	if stEnabled {
		return resourceapply.ApplyValidatingAdmissionPolicyBindingV1(c.ctx, c.kubeClient.AdmissionregistrationV1(), c.eventRecorder, required, c.cache)
	}
	return DeleteValidatingAdmissionPolicyBindingV1(c.ctx, c.kubeClient.AdmissionregistrationV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageSoftTainterClusterMonitoringViewClusterRoleBinding(descheduler *deschedulerv1.KubeDescheduler, stEnabled bool) (*rbacv1.ClusterRoleBinding, bool, error) {
	required := resourceread.ReadClusterRoleBindingV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtainterclusterrolebindingprometheus.yaml"))
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
	if stEnabled {
		return resourceapply.ApplyClusterRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
	}
	return resourceapply.DeleteClusterRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
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

func (c *TargetConfigReconciler) manageOperandRole(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.Role, bool, error) {
	required := resourceread.ReadRoleV1OrDie(bindata.MustAsset("assets/kube-descheduler/operandrole.yaml"))
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

func (c *TargetConfigReconciler) manageOperandRoleBinding(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.RoleBinding, bool, error) {
	required := resourceread.ReadRoleBindingV1OrDie(bindata.MustAsset("assets/kube-descheduler/operandrolebinding.yaml"))
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

func (c *TargetConfigReconciler) manageSoftTainterRole(descheduler *deschedulerv1.KubeDescheduler, stEnabled bool) (*rbacv1.Role, bool, error) {
	required := resourceread.ReadRoleV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtainterrole.yaml"))
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

	if stEnabled {
		return resourceapply.ApplyRole(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
	}
	return resourceapply.DeleteRole(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageSoftTainterRoleBinding(descheduler *deschedulerv1.KubeDescheduler, stEnabled bool) (*rbacv1.RoleBinding, bool, error) {
	required := resourceread.ReadRoleBindingV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtainterrolebinding.yaml"))
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

	if stEnabled {
		return resourceapply.ApplyRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
	}
	return resourceapply.DeleteRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
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

func (c *TargetConfigReconciler) manageSoftTainterServiceAccount(descheduler *deschedulerv1.KubeDescheduler, stEnabled bool) (*v1.ServiceAccount, bool, error) {
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

	if stEnabled {
		return resourceapply.ApplyServiceAccount(c.ctx, c.kubeClient.CoreV1(), c.eventRecorder, required)
	}
	return resourceapply.DeleteServiceAccount(c.ctx, c.kubeClient.CoreV1(), c.eventRecorder, required)
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

	// parse whatever profiles are set into their policy representations then merge them into one file
	if len(descheduler.Spec.Profiles) == 0 {
		return nil, false, fmt.Errorf("descheduler should have at least 1 profile enabled")
	}

	profiles := sets.NewString()
	for _, profileName := range descheduler.Spec.Profiles {
		profiles.Insert(string(profileName))
		switch profileName {
		case deschedulerv1.KubeVirtRelieveAndMigrate, deschedulerv1.DevKubeVirtRelieveAndMigrate:
			kvDeployed, kverr := c.isKubeVirtDeployed()
			if kverr != nil {
				return nil, false, kverr
			}
			if !kvDeployed {
				return nil, true, fmt.Errorf("profile %v can only be used when KubeVirt is properly deployed", profileName)
			}
			psiEnabled, psierr := c.isPSIenabled()
			if psierr != nil {
				return nil, false, psierr
			}
			if !psiEnabled {
				return nil, true, fmt.Errorf("profile %v can only be used when PSI metrics are enabled for the worker nodes", profileName)
			}
		}
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

	var prometheusHost string
	if c.isPrometheusAsMetricsProviderForProfiles(descheduler) {
		// detect the prometheus server url
		route, err := c.routeRouteLister.Routes("openshift-monitoring").Get("prometheus-k8s")
		if err != nil {
			return nil, true, fmt.Errorf("unable to get openshift-monitoring/prometheus-k8s route: %v", err)
		}
		if len(route.Status.Ingress) == 0 {
			return nil, true, fmt.Errorf("No ingress found in openshift-monitoring/prometheus-k8s route")
		}
		if route.Status.Ingress[0].Host == "" {
			return nil, true, fmt.Errorf("Host for status.ingress[0] in openshift-monitoring/prometheus-k8s route is empty")
		}
		err = c.checkNamespaceMonitoringLabel()
		if err != nil {
			return nil, false, err
		}
		prometheusHost = route.Status.Ingress[0].Host
		klog.InfoS("Detecting prometheus server url", "url", prometheusHost)
	}

	policy, err := operatorprofiles.BuildDeschedulingPolicy(descheduler, c.protectedNamespaces, prometheusHost)
	if err != nil {
		return nil, false, err
	}

	policyBytes, err := yaml.Marshal(policy)
	if err != nil {
		return nil, false, err
	}
	required.Data = map[string]string{"policy.yaml": string(policyBytes)}
	return resourceapply.ApplyConfigMap(c.ctx, c.kubeClient.CoreV1(), c.eventRecorder, required)
}

// validateDeschedulerCR validates the descheduler object against the CRD schema
func validateDeschedulerCR(descheduler *deschedulerv1.KubeDescheduler) error {
	var schema spec.Schema
	if err := yaml.Unmarshal(bindata.MustAsset("assets/kube-descheduler/crdschema.yaml"), &schema); err != nil {
		return fmt.Errorf("failed to unmarshal CRD schema: %w", err)
	}

	deschedulerUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(descheduler)
	if err != nil {
		return fmt.Errorf("failed to convert descheduler to unstructured: %w", err)
	}

	// Standard OpenAPI schema validation
	result := validate.NewSchemaValidator(&schema, nil, "", strfmt.Default).Validate(deschedulerUnstructured)
	if result != nil && result.HasErrors() {
		var errMsgs []string
		for _, err := range result.Errors {
			errMsgs = append(errMsgs, err.Error())
		}
		return fmt.Errorf("validation errors: %s", strings.Join(errMsgs, "; "))
	}

	// CEL validation for x-kubernetes-validations rules
	if err := validateCELRules(descheduler, &schema); err != nil {
		return err
	}

	return nil
}

// validateCELRules evaluates CEL validation rules from the schema
func validateCELRules(descheduler *deschedulerv1.KubeDescheduler, schema *spec.Schema) error {
	// Extract profiles field schema which contains x-kubernetes-validations
	profilesSchema, ok := schema.Properties["spec"]
	if !ok {
		return nil
	}

	profilesField, ok := profilesSchema.Properties["profiles"]
	if !ok {
		return nil
	}

	// Get x-kubernetes-validations from the schema
	celValidations, ok := profilesField.VendorExtensible.Extensions["x-kubernetes-validations"]
	if !ok {
		return nil
	}

	// Parse the validations
	var validations []map[string]interface{}
	validationsBytes, err := yaml.Marshal(celValidations)
	if err != nil {
		return fmt.Errorf("failed to marshal CEL validations: %w", err)
	}
	if err := yaml.Unmarshal(validationsBytes, &validations); err != nil {
		return fmt.Errorf("failed to unmarshal CEL validations: %w", err)
	}

	// Create CEL environment with 'self' variable (array of strings)
	env, err := cel.NewEnv(
		cel.Declarations(
			decls.NewVar("self", decls.NewListType(decls.String)),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create CEL environment: %w", err)
	}

	// Convert profiles to slice of strings for CEL
	profileStrings := make([]string, len(descheduler.Spec.Profiles))
	for i, p := range descheduler.Spec.Profiles {
		profileStrings[i] = string(p)
	}

	// Evaluate each validation rule
	for _, validation := range validations {
		rule, ok := validation["rule"].(string)
		if !ok {
			continue
		}
		message, _ := validation["message"].(string)

		// Compile the CEL expression
		ast, issues := env.Compile(rule)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("failed to compile CEL rule %q: %w", rule, issues.Err())
		}

		// Create program
		program, err := env.Program(ast)
		if err != nil {
			return fmt.Errorf("failed to create CEL program for rule %q: %w", rule, err)
		}

		// Evaluate the expression with 'self' set to the profiles array
		evalResult, _, err := program.Eval(map[string]interface{}{
			"self": profileStrings,
		})
		if err != nil {
			return fmt.Errorf("failed to evaluate CEL rule %q: %w", rule, err)
		}

		// Check if the rule evaluated to false (validation failed)
		if result, ok := evalResult.Value().(bool); ok && !result {
			if message != "" {
				return fmt.Errorf("%s", message)
			}
			return fmt.Errorf("CEL validation failed for rule: %s", rule)
		}
	}

	return nil
}

func (c *TargetConfigReconciler) manageDeployment(required *appsv1.Deployment, descheduler *deschedulerv1.KubeDescheduler, targetImageKey, targetImagePullSpec string, specAnnotations map[string]string) (*appsv1.Deployment, bool, error) {
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

	images := map[string]string{
		targetImageKey: targetImagePullSpec,
	}
	for i := range required.Spec.Template.Spec.Containers {
		for pat, img := range images {
			if required.Spec.Template.Spec.Containers[i].Image == pat {
				required.Spec.Template.Spec.Containers[i].Image = img
				break
			}
		}
	}

	required.Spec.Template.Spec.Volumes[0].VolumeSource.ConfigMap.LocalObjectReference.Name = descheduler.Name

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
	const targetImageKey = "${OPERAND_IMAGE}"
	required := resourceread.ReadDeploymentV1OrDie(bindata.MustAsset("assets/kube-descheduler/deployment.yaml"))
	required.Name = operatorclient.OperandName
	required.Namespace = descheduler.Namespace

	required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args,
		fmt.Sprintf("--descheduling-interval=%ss", strconv.Itoa(int(*descheduler.Spec.DeschedulingIntervalSeconds))))

	featureGates := []string{}
	evictionsInBackgroundEnabled := descheduler.Spec.ProfileCustomizations != nil && descheduler.Spec.ProfileCustomizations.DevEnableEvictionsInBackground
	evictionsInBackgroundEnabled = evictionsInBackgroundEnabled || hasKubeVirtRelieveAndMigrateProfile(descheduler.Spec.Profiles)
	if evictionsInBackgroundEnabled {
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

	return c.manageDeployment(required, descheduler, targetImageKey, c.deschedulerImagePullSpec, specAnnotations)
}

func (c *TargetConfigReconciler) manageSoftTainterDeployment(descheduler *deschedulerv1.KubeDescheduler, specAnnotations map[string]string, stEnabled bool) (*appsv1.Deployment, bool, error) {
	const targetImageKey = "${SOFTTAINTER_IMAGE}"
	required := resourceread.ReadDeploymentV1OrDie(bindata.MustAsset("assets/kube-descheduler/softtainterdeployment.yaml"))
	required.Name = operatorclient.SoftTainterOperandName
	required.Namespace = descheduler.Namespace
	if stEnabled {
		return c.manageDeployment(required, descheduler, targetImageKey, c.softtainterImagePullSpec, specAnnotations)
	}
	return resourceapply.DeleteDeployment(c.ctx, c.kubeClient.AppsV1(), c.eventRecorder, required)
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

func (c *TargetConfigReconciler) checkNamespaceMonitoringLabel() error {
	operatorNamespace, err := c.namespaceLister.Get(operatorclient.OperatorNamespace)
	if err != nil {
		klog.ErrorS(err, "error fetching operator namespace")
		return err
	}
	if operatorNamespace.GetLabels()[operatorclient.OpenshiftClusterMonitoringLabelKey] != operatorclient.OpenshiftClusterMonitoringLabelValue {
		return fmt.Errorf("namespace %v is not labeled with %v=%v", operatorclient.OperatorNamespace, operatorclient.OpenshiftClusterMonitoringLabelKey, operatorclient.OpenshiftClusterMonitoringLabelValue)
	}
	return nil
}

func (c *TargetConfigReconciler) isKubeVirtDeployed() (bool, error) {
	ls, err := labels.Parse(kubeVirtShedulableLabelSelector)
	if err != nil {
		return false, err
	}
	nodes, err := c.nodeLister.List(ls)
	if err != nil {
		return false, err
	}
	if len(nodes) > 0 {
		return true, nil
	}
	return false, nil
}

func (c *TargetConfigReconciler) isPSIenabled() (bool, error) {
	if boolValue, err := strconv.ParseBool(os.Getenv(EXPERIMENTAL_DISABLE_PSI_CHECK)); err == nil && boolValue {
		return true, nil
	}

	_, err := os.Stat(c.psiPath)
	if err == nil {
		return true, nil
	} else {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
}

func hasKubeVirtRelieveAndMigrateProfile(profiles []deschedulerv1.DeschedulerProfile) bool {
	return slices.Contains(profiles, deschedulerv1.KubeVirtRelieveAndMigrate) || slices.Contains(profiles, deschedulerv1.DevKubeVirtRelieveAndMigrate)
}

func (c *TargetConfigReconciler) isSoftTainterNeeded(descheduler *deschedulerv1.KubeDescheduler) (bool, error) {
	if hasKubeVirtRelieveAndMigrateProfile(descheduler.Spec.Profiles) {
		return true, nil
	}

	ls, err := labels.Parse(kubeVirtShedulableLabelSelector)
	if err != nil {
		return false, err
	}
	nodes, err := c.nodeLister.List(ls)
	if err != nil {
		return false, err
	}
	leftoverSoftTaints := false
	softTaints := []*v1.Taint{
		{Key: softtainter.AppropriatelyUtilizedSoftTaintKey, Value: softtainter.AppropriatelyUtilizedSoftTaintValue, Effect: v1.TaintEffectPreferNoSchedule},
		{Key: softtainter.OverUtilizedSoftTaintKey, Value: softtainter.OverUtilizedSoftTaintValue, Effect: v1.TaintEffectPreferNoSchedule},
	}
	for _, node := range nodes {
		for _, t := range softTaints {
			if taints.TaintExists(node.Spec.Taints, t) {
				leftoverSoftTaints = true
				klog.InfoS("The softtainter is disabled a leftover soft taint is still present", "node", node.Name, "taintKey", t.Key)
			}
		}
	}
	if leftoverSoftTaints {
		klog.InfoS("Deploying the softtainter to cleanup leftover soft taints")
	}
	return leftoverSoftTaints, nil
}

// isPrometheusAsMetricsProviderForProfiles returns true when at least a profile that by default relies on PrometheusMetrics is in use
// or the user is explicitly configuring DevActualUtilizationProfile profile customization
func (c *TargetConfigReconciler) isPrometheusAsMetricsProviderForProfiles(descheduler *deschedulerv1.KubeDescheduler) bool {
	if descheduler != nil &&
		(hasKubeVirtRelieveAndMigrateProfile(descheduler.Spec.Profiles) ||
			(descheduler.Spec.ProfileCustomizations != nil && descheduler.Spec.ProfileCustomizations.DevActualUtilizationProfile != "")) {
		return true
	}
	return false
}
