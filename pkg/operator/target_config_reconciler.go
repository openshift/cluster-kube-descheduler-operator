package operator

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	deschedulerv1beta1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1beta1"
	operatorconfigclientv1beta1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned/typed/descheduler/v1beta1"
	operatorclientinformers "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/informers/externalversions/descheduler/v1beta1"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/library-go/pkg/operator/events"

	batch "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

const DefaultImage = "quay.io/openshift/origin-descheduler:latest"

// array of valid strategies. TODO: Make this map(or set) once we have lot of strategies.
var validStrategies = sets.NewString("duplicates", "interpodantiaffinity", "lownodeutilization", "nodeaffinity")

// deschedulerCommand provides descheduler command with policyconfigfile mounted as volume and log-level for backwards
// compatibility with 3.11
var DeschedulerCommand = []string{"/bin/descheduler", "--policy-config-file", "/policy-dir/policy.yaml", "--v", "5"}

type TargetConfigReconciler struct {
	operatorClient operatorconfigclientv1beta1.KubedeschedulersV1beta1Interface
	kubeClient     kubernetes.Interface
	eventRecorder  events.Recorder
	queue          workqueue.RateLimitingInterface
}

func NewTargetConfigReconciler(
	operatorConfigClient operatorconfigclientv1beta1.KubedeschedulersV1beta1Interface,
	operatorClientInformer operatorclientinformers.KubeDeschedulerInformer,
	kubeClient kubernetes.Interface,
	eventRecorder events.Recorder,
) *TargetConfigReconciler {
	c := &TargetConfigReconciler{
		operatorClient: operatorConfigClient,
		kubeClient:     kubeClient,
		eventRecorder:  eventRecorder,
		queue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "TargetConfigReconciler"),
	}

	operatorClientInformer.Informer().AddEventHandler(c.eventHandler())

	return c
}

func (c TargetConfigReconciler) sync() error {
	descheduler, err := c.operatorClient.KubeDeschedulers(operatorclient.OperatorNamespace).Get("cluster", metav1.GetOptions{})
	if err != nil {
		return err
	}

	if len(descheduler.Spec.Schedule) == 0 {
		return fmt.Errorf("descheduler should have schedule for cron job set")
	}

	if err := validateStrategies(descheduler.Spec.Strategies); err != nil {
		return err
	}
	if err := c.generateConfigMap(descheduler); err != nil {
		return err
	}
	if err := c.generateDeschedulerJob(descheduler); err != nil {
		return err
	}

	return nil
}

func validateStrategies(strategies []deschedulerv1beta1.Strategy) error {
	if len(strategies) == 0 {
		return fmt.Errorf("descheduler should have atleast one strategy enabled and it should be one of %v", strings.Join(validStrategies.List(), ","))
	}

	if len(strategies) > len(validStrategies) {
		return fmt.Errorf("descheduler can have a maximum of %v strategies enabled at this point of time", len(validStrategies))
	}

	invalidStrategies := make([]string, 0, len(strategies))
	for _, strategy := range strategies {
		if !validStrategies.Has(strings.ToLower(strategy.Name)) {
			invalidStrategies = append(invalidStrategies, strategy.Name)
		}
	}
	if len(invalidStrategies) > 0 {
		return fmt.Errorf("expected one of the %v to be enabled but found following invalid strategies %v",
			strings.Join(validStrategies.List(), ","), strings.Join(invalidStrategies, ","))
	}
	return nil
}

func (c *TargetConfigReconciler) generateConfigMap(descheduler *deschedulerv1beta1.KubeDescheduler) error {
	configMap, err := c.kubeClient.CoreV1().ConfigMaps(descheduler.Namespace).Get(descheduler.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// configmap was not found, so create it
		cm := &v1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      descheduler.Name,
				Namespace: descheduler.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "v1beta1",
						Kind:       "KubeDescheduler",
						Name:       descheduler.Name,
						UID:        descheduler.UID,
					},
				},
			},
			Data: map[string]string{
				"policy.yaml": generateConfigMapString(descheduler.Spec.Strategies),
			},
		}
		if _, err := c.kubeClient.CoreV1().ConfigMaps(descheduler.Namespace).Create(cm); err != nil {
			return err
		}
		return nil
	}

	if propertiesHaveChanged(descheduler.Spec.Strategies, configMap.Data) {
		configMap.Data = map[string]string{"policy.yaml": generateConfigMapString(descheduler.Spec.Strategies)}
		if _, err := c.kubeClient.CoreV1().ConfigMaps(descheduler.Namespace).Update(configMap); err != nil {
			return err
		}
	}

	return nil
}

// generateConfigMapString generates configmap needed for the string.
func generateConfigMapString(requestedStrategies []deschedulerv1beta1.Strategy) string {
	strategiesPolicyString := "apiVersion: \"descheduler/v1beta1\"\nkind: \"DeschedulerPolicy\"\nstrategies:\n"
	// There is no need to do validation here. By the time, we reach here, validation would have already happened.
	for _, strategy := range requestedStrategies {
		switch strings.ToLower(strategy.Name) {
		case "duplicates":
			strategiesPolicyString = strategiesPolicyString + "  \"RemoveDuplicates\":\n     enabled: true\n"
		case "interpodantiaffinity":
			strategiesPolicyString = strategiesPolicyString + "  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true\n"
		case "lownodeutilization":
			strategiesPolicyString = strategiesPolicyString + "  \"LowNodeUtilization\":\n     enabled: true\n     params:\n" + "       nodeResourceUtilizationThresholds:\n"
			paramString := ""
			if len(strategy.Params) > 0 {
				// TODO: Make this more generic using methods and interfaces.
				paramString = addStrategyParamsForLowNodeUtilization(strategy.Params)
				strategiesPolicyString = strategiesPolicyString + paramString
			}
		case "nodeaffinity":
			strategiesPolicyString = strategiesPolicyString + "  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution\n"
		default:
			// Accept no other strategy except for the valid ones.
		}
	}
	// At last, we will have a "\n", which we don't need.
	return strings.TrimSuffix(strategiesPolicyString, "\n")
}

// addStrategyParamsForLowNodeUtilization adds parameters for low node utilization strategy.
func addStrategyParamsForLowNodeUtilization(params []deschedulerv1beta1.Param) string {
	thresholds := ""
	targetThresholds := ""
	thresholdsString := "         thresholds:"
	targetThresholdsString := "         targetThresholds:"
	noOfNodes := "         numberOfNodes:"
	nodesParamFound := false
	for _, param := range params {
		// collect all thresholds
		if !strings.Contains(strings.ToUpper(param.Name), strings.ToUpper("target")) {
			switch param.Name {
			case "cputhreshold":
				thresholds = thresholds + "           cpu: " + param.Value + "\n"
			case "memorythreshold":
				thresholds = thresholds + "           memory: " + param.Value + "\n"
			case "podsthreshold":
				thresholds = thresholds + "           pods: " + param.Value + "\n"
			}
		} else {
			// collect all target thresholds
			switch param.Name {
			case "cputargetthreshold":
				targetThresholds = targetThresholds + "           cpu: " + param.Value + "\n"
			case "memorytargetthreshold":
				targetThresholds = targetThresholds + "           memory: " + param.Value + "\n"
			case "podstargetthreshold":
				targetThresholds = targetThresholds + "           pods: " + param.Value + "\n"
			}
		}
		if param.Name == "nodes" {
			nodesParamFound = true
			noOfNodes = noOfNodes + " " + param.Value + "\n"
		}
	}
	// If noOfNodes parameter is not found, set it to 0.
	if !nodesParamFound {
		noOfNodes = noOfNodes + " 0\n"
	}
	// If threshold is specified we should specify target threshold as well.
	return thresholdsString + "\n" + thresholds + targetThresholdsString + "\n" + targetThresholds + noOfNodes
}

func propertiesHaveChanged(strategies []deschedulerv1beta1.Strategy, existingStrategies map[string]string) bool {
	policyString := existingStrategies["policy.yaml"]
	return policyString == generateConfigMapString(strategies)
}

func (c *TargetConfigReconciler) generateDeschedulerJob(descheduler *deschedulerv1beta1.KubeDescheduler) error {
	cronJob, err := c.kubeClient.BatchV1beta1().CronJobs(descheduler.Namespace).Get(descheduler.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		return c.createCronJob(descheduler)
	}

	if cronJob.Spec.Schedule != descheduler.Spec.Schedule ||
		cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image != descheduler.Spec.Image ||
		flagsChanged(descheduler.Spec.Flags, cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Command) {
		// Delete should trigger a new sync event to create the CronJob again with the new params
		return c.kubeClient.BatchV1beta1().CronJobs(descheduler.Namespace).Delete(descheduler.Name, &metav1.DeleteOptions{})
	}

	return nil
}

func flagsChanged(newFlags []deschedulerv1beta1.Param, oldFlags []string) bool {
	latestFlags, err := ValidateFlags(newFlags)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(latestFlags, oldFlags)
}

// ValidateFlags validates flags for descheduler. We don't validate the values here in descheduler operator.
func ValidateFlags(flags []deschedulerv1beta1.Param) ([]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	deschedulerFlags := make([]string, 0)
	validFlags := []string{"descheduling-interval", "dry-run", "node-selector"}
	for _, flag := range flags {
		allowedFlag := false
		for _, validFlag := range validFlags {
			if flag.Name == validFlag {
				allowedFlag = true
			}
		}
		if allowedFlag {
			deschedulerFlags = append(deschedulerFlags, []string{"--" + flag.Name, flag.Value}...)
		} else {
			return nil, fmt.Errorf("descheduler allows only following flags %v but found %v", strings.Join(validFlags, ","), flag.Name)
		}
	}
	return deschedulerFlags, nil
}

func (c *TargetConfigReconciler) createCronJob(descheduler *deschedulerv1beta1.KubeDescheduler) error {
	flags, err := ValidateFlags(descheduler.Spec.Flags)
	if err != nil {
		return err
	}
	if len(descheduler.Spec.Image) == 0 {
		// Set the default image here
		descheduler.Spec.Image = DefaultImage // No need to update the CR here making it opaque to end-user
	}
	flags = append(DeschedulerCommand, flags...)
	job := &batchv1beta1.CronJob{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CronJob",
			APIVersion: batch.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      descheduler.Name,
			Namespace: descheduler.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1beta1",
					Kind:       "KubeDescheduler",
					Name:       descheduler.Name,
					UID:        descheduler.UID,
				},
			},
		},
		Spec: batchv1beta1.CronJobSpec{
			Schedule: descheduler.Spec.Schedule,
			JobTemplate: batchv1beta1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: "descheduler-job-spec",
				},
				Spec: batch.JobSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Volumes: []v1.Volume{{
								Name: "policy-volume",
								VolumeSource: v1.VolumeSource{
									ConfigMap: &v1.ConfigMapVolumeSource{
										LocalObjectReference: v1.LocalObjectReference{
											Name: descheduler.Name,
										},
									},
								},
							},
							},
							PriorityClassName: "system-cluster-critical",
							RestartPolicy:     "Never",
							Containers: []v1.Container{{
								Name:  "openshift-descheduler",
								Image: descheduler.Spec.Image,
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("100m"),
										v1.ResourceMemory: resource.MustParse("500Mi"),
									},
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("100m"),
										v1.ResourceMemory: resource.MustParse("500Mi"),
									},
								},
								Command: flags,
								VolumeMounts: []v1.VolumeMount{{
									MountPath: "/policy-dir",
									Name:      "policy-volume",
								}},
							}},
							ServiceAccountName: "openshift-descheduler", // TODO: This is hardcoded as of now, find a way to reference it from rbac.yaml.
						},
					},
				},
			},
		},
	}
	_, err = c.kubeClient.BatchV1beta1().CronJobs(descheduler.Namespace).Create(job)
	return err
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
