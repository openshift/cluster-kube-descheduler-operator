package operator

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/imdario/mergo"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	configlistersv1 "github.com/openshift/client-go/config/listers/config/v1"
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

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	deschedulerapi "sigs.k8s.io/descheduler/pkg/api/v1alpha1"
)

const DefaultImage = "quay.io/openshift/origin-descheduler:latest"

// deschedulerCommand provides descheduler command with policyconfigfile mounted as volume and log-level for backwards
// compatibility with 3.11
var DeschedulerCommand = []string{"/bin/descheduler", "--policy-config-file", "/policy-dir/policy.yaml", "--v", "2"}

type TargetConfigReconciler struct {
	ctx                   context.Context
	targetImagePullSpec   string
	operatorClient        operatorconfigclientv1.KubedeschedulersV1Interface
	deschedulerClient     *operatorclient.DeschedulerClient
	kubeClient            kubernetes.Interface
	dynamicClient         dynamic.Interface
	eventRecorder         events.Recorder
	queue                 workqueue.RateLimitingInterface
	excludedNamespaces    []string
	configSchedulerLister configlistersv1.SchedulerLister
}

func NewTargetConfigReconciler(
	ctx context.Context,
	targetImagePullSpec string,
	operatorConfigClient operatorconfigclientv1.KubedeschedulersV1Interface,
	operatorClientInformer operatorclientinformers.KubeDeschedulerInformer,
	deschedulerClient *operatorclient.DeschedulerClient,
	kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	configInformer configinformers.SharedInformerFactory,
	eventRecorder events.Recorder,
) *TargetConfigReconciler {
	// make sure our list of excluded system namespaces is up to date
	allNamespaces, err := kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.ErrorS(err, "error listing namespaces")
		return nil
	}
	excludedNamespaces := []string{"kube-system"}
	for _, ns := range allNamespaces.Items {
		if strings.HasPrefix(ns.Name, "openshift-") {
			excludedNamespaces = append(excludedNamespaces, ns.Name)
		}
	}

	c := &TargetConfigReconciler{
		ctx:                   ctx,
		operatorClient:        operatorConfigClient,
		deschedulerClient:     deschedulerClient,
		kubeClient:            kubeClient,
		dynamicClient:         dynamicClient,
		eventRecorder:         eventRecorder,
		queue:                 workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "TargetConfigReconciler"),
		excludedNamespaces:    excludedNamespaces,
		targetImagePullSpec:   targetImagePullSpec,
		configSchedulerLister: configInformer.Config().V1().Schedulers().Lister(),
	}
	configInformer.Config().V1().Schedulers().Informer().AddEventHandler(c.eventHandler())
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

	forceDeployment := false
	replicas := int32(1)
	_, forceDeployment, err = c.manageConfigMap(descheduler)
	if err != nil {
		// if we returned an error from the configmap AND want to force a deployment
		// it means we want to scale the deployment to 0
		if forceDeployment {
			replicas = 0
			deployment, _, err := c.manageDeployment(descheduler, forceDeployment, &replicas)
			if err != nil {
				klog.ErrorS(err, "Unable to scale descheduler operand deployment")
			}
			_, _, err = v1helpers.UpdateStatus(c.deschedulerClient,
				v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
					Type:   "TargetConfigControllerDegraded",
					Status: operatorv1.ConditionTrue,
				}),
				func(status *operatorv1.OperatorStatus) error {
					resourcemerge.SetDeploymentGeneration(&status.Generations, deployment)
					return nil
				})
		}
		return err
	}

	if _, _, err := c.manageService(descheduler); err != nil {
		return err
	}

	if _, _, err := c.manageRole(descheduler); err != nil {
		return err
	}

	if _, _, err := c.manageRoleBinding(descheduler); err != nil {
		return err
	}

	if _, err := c.manageServiceMonitor(descheduler); err != nil {
		return err
	}

	deployment, _, err := c.manageDeployment(descheduler, forceDeployment, &replicas)
	if err != nil {
		return err
	}

	_, _, err = v1helpers.UpdateStatus(c.deschedulerClient, func(status *operatorv1.OperatorStatus) error {
		resourcemerge.SetDeploymentGeneration(&status.Generations, deployment)
		return nil
	})
	return err
}

func (c *TargetConfigReconciler) manageRole(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.Role, bool, error) {
	required := resourceread.ReadRoleV1OrDie(bindata.MustAsset("assets/kube-descheduler/role.yaml"))
	required.Namespace = descheduler.Namespace
	required.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "KubeDescheduler",
			Name:       descheduler.Name,
			UID:        descheduler.UID,
		},
	}

	return resourceapply.ApplyRole(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageRoleBinding(descheduler *deschedulerv1.KubeDescheduler) (*rbacv1.RoleBinding, bool, error) {
	required := resourceread.ReadRoleBindingV1OrDie(bindata.MustAsset("assets/kube-descheduler/rolebinding.yaml"))
	required.Namespace = descheduler.Namespace
	required.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "KubeDescheduler",
			Name:       descheduler.Name,
			UID:        descheduler.UID,
		},
	}

	return resourceapply.ApplyRoleBinding(c.ctx, c.kubeClient.RbacV1(), c.eventRecorder, required)
}

func (c *TargetConfigReconciler) manageService(descheduler *deschedulerv1.KubeDescheduler) (*v1.Service, bool, error) {
	required := resourceread.ReadServiceV1OrDie(bindata.MustAsset("assets/kube-descheduler/service.yaml"))
	required.Namespace = descheduler.Namespace
	required.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "KubeDescheduler",
			Name:       descheduler.Name,
			UID:        descheduler.UID,
		},
	}

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
	required.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "KubeDescheduler",
			Name:       descheduler.Name,
			UID:        descheduler.UID,
		},
	}

	scheduler, err := c.configSchedulerLister.Get("cluster")
	if err != nil {
		return nil, false, err
	}

	// parse whatever profiles are set into their policy representations then merge them into one file
	if len(descheduler.Spec.Profiles) == 0 {
		return nil, false, fmt.Errorf("descheduler should have at least 1 profile enabled")
	}
	profiles := sets.NewString()
	policy := &deschedulerapi.DeschedulerPolicy{}
	for _, profileName := range descheduler.Spec.Profiles {
		p := bindata.MustAsset("assets/profiles/" + string(profileName) + ".yaml")
		profile := &deschedulerapi.DeschedulerPolicy{}
		if err := yaml.Unmarshal(p, profile); err != nil {
			return nil, false, err
		}

		if err := checkProfileConflicts(profiles, profileName, scheduler); err != nil {
			klog.ErrorS(err, "Profile conflict")
			continue
		}

		// exclude openshift namespaces from descheduling
		for name, strategy := range profile.Strategies {
			// skip strategies which don't support namespace exclusion yet
			if name == "LowNodeUtilization" {
				continue
			}
			if strategy.Params == nil {
				strategy.Params = &deschedulerapi.StrategyParameters{}
			}
			strategy.Params.Namespaces = &deschedulerapi.Namespaces{Exclude: c.excludedNamespaces}
			profile.Strategies[name] = strategy
		}
		mergo.Merge(policy, profile, mergo.WithAppendSlice)
	}

	// Check for conflicting kube-scheduler config
	if scheduler.Spec.Profile == configv1.HighNodeUtilization &&
		(profiles.Has(string(deschedulerv1.LifecycleAndUtilization)) || profiles.Has(string(deschedulerv1.DevPreviewLongLifecycle))) {
		// force a new deployment so we can scale it to 0
		return nil, true, fmt.Errorf("enabling Descheduler LowNodeUtilization with Scheduler HighNodeUtilization may cause an eviction/scheduling hot loop")
	}

	// set PodLifetime if non-default
	if descheduler.Spec.ProfileCustomizations != nil && descheduler.Spec.ProfileCustomizations.PodLifetime != nil {
		seconds := uint(descheduler.Spec.ProfileCustomizations.PodLifetime.Seconds())
		if strategy, ok := policy.Strategies["PodLifetime"]; ok {
			strategy.Params.PodLifeTime.MaxPodLifeTimeSeconds = &seconds
		}
	}

	// ignore PVC pods by default
	ignorePVCPods := true
	policy.IgnorePVCPods = &ignorePVCPods

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

	if profiles.Has(string(deschedulerv1.DevPreviewLongLifecycle)) && profiles.Has(string(deschedulerv1.LifecycleAndUtilization)) {
		return fmt.Errorf("cannot declare %s and %s profiles simultaneously, ignoring", deschedulerv1.DevPreviewLongLifecycle, deschedulerv1.LifecycleAndUtilization)
	}

	if profiles.Has(string(deschedulerv1.SoftTopologyAndDuplicates)) && profiles.Has(string(deschedulerv1.TopologyAndDuplicates)) {
		return fmt.Errorf("cannot declare %s and %s profiles simultaneously, ignoring", deschedulerv1.TopologyAndDuplicates, deschedulerv1.SoftTopologyAndDuplicates)
	}
	return nil
}

func (c *TargetConfigReconciler) manageDeployment(descheduler *deschedulerv1.KubeDescheduler, forceDeployment bool, replicas *int32) (*appsv1.Deployment, bool, error) {
	required := resourceread.ReadDeploymentV1OrDie(bindata.MustAsset("assets/kube-descheduler/deployment.yaml"))
	required.Name = descheduler.Name
	required.Namespace = descheduler.Namespace
	required.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "KubeDescheduler",
			Name:       descheduler.Name,
			UID:        descheduler.UID,
		},
	}
	required.Spec.Replicas = replicas
	required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args,
		fmt.Sprintf("--descheduling-interval=%ss", strconv.Itoa(int(*descheduler.Spec.DeschedulingIntervalSeconds))))
	required.Spec.Template.Spec.Volumes[0].VolumeSource.ConfigMap.LocalObjectReference.Name = descheduler.Name

	images := map[string]string{
		"${IMAGE}": c.targetImagePullSpec,
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

	if !forceDeployment {
		existingDeployment, err := c.kubeClient.AppsV1().Deployments(required.Namespace).Get(c.ctx, descheduler.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				forceDeployment = true
			} else {
				return nil, false, err
			}
		} else {
			forceDeployment = deploymentChanged(existingDeployment, required)
		}
	}
	// FIXME: this method will disappear in 4.6 so we need to fix this ASAP
	return resourceapply.ApplyDeploymentWithForce(
		c.ctx,
		c.kubeClient.AppsV1(),
		c.eventRecorder,
		required,
		resourcemerge.ExpectedDeploymentGeneration(required, descheduler.Status.Generations),
		forceDeployment)
}

func deploymentChanged(existing, new *appsv1.Deployment) bool {
	newArgs := sets.NewString(new.Spec.Template.Spec.Containers[0].Args...)
	existingArgs := sets.NewString(existing.Spec.Template.Spec.Containers[0].Args...)
	return existing.Name != new.Name ||
		existing.Namespace != new.Namespace ||
		existing.Spec.Template.Spec.Containers[0].Image != new.Spec.Template.Spec.Containers[0].Image ||
		existing.Spec.Template.Spec.Volumes[0].VolumeSource.ConfigMap.LocalObjectReference.Name != new.Spec.Template.Spec.Volumes[0].VolumeSource.ConfigMap.LocalObjectReference.Name ||
		!reflect.DeepEqual(newArgs, existingArgs)
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
