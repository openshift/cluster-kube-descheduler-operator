package descheduler

import (
	"context"
	"fmt"
	"log"
	"strings"

	deschedulerv1alpha1 "github.com/openshift/descheduler-operator/pkg/apis/descheduler/v1alpha1"
	batch "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	Running  = "RunningPhase"
	Updating = "UpdatingPhase"
)

// array of valid strategies. TODO: Make this map(or set) once we have lot of strategies.
var validStrategies = []string{"duplicates", "interpodantiaffinity", "lownodeutilization", "nodeaffinity"}

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Descheduler Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileDescheduler{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("descheduler-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Descheduler
	err = c.Watch(&source.Kind{Type: &deschedulerv1alpha1.Descheduler{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileDescheduler{}

// ReconcileDescheduler reconciles a Descheduler object
type ReconcileDescheduler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Descheduler object and makes changes based on the state read
// and what is in the Descheduler.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileDescheduler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Printf("Reconciling Descheduler %s/%s\n", request.Namespace, request.Name)

	// Fetch the Descheduler instance
	descheduler := &deschedulerv1alpha1.Descheduler{}
	err := r.client.Get(context.TODO(), request.NamespacedName, descheduler)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Printf("Descheduler %s/%s not found. Ignoring since object must be deleted\n", request.Namespace, request.Name)
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Printf("Failed to get Descheduler: %v", err)
		return reconcile.Result{}, err
	}

	// Descheduler. If descheduler object doesn't have any of the valid fields, return error
	// immediately, don't proceed with config map/ job creation.

	if len(descheduler.Spec.Schedule) == 0 {
		log.Printf("Descheduler should have schedule for cron job set")
		return reconcile.Result{}, err
	}

	strategiesEnabled := getAllStrategiesEnabled(descheduler.Spec.Strategies)
	if err := validateStrategies(strategiesEnabled); err != nil {
		return reconcile.Result{}, err
	}

	// Generate Descheduler policy configmap
	if err := r.generateConfigMap(descheduler); err != nil {
		return reconcile.Result{}, err
	}

	// Generate descheduler job.
	if err := r.generateDeschedulerJob(descheduler); err != nil {
		return reconcile.Result{}, err
	}

	if descheduler.Status.Phase != Running {
		descheduler.Status.Phase = Running
		return reconcile.Result{Requeue: true}, nil
	}

	return reconcile.Result{}, nil
}

// getAllStrategiesEnabled returns the list of strategies enabled for descheduler.
func getAllStrategiesEnabled(strategies []deschedulerv1alpha1.Strategy) []string {
	strategyName := make([]string, 0)
	for _, strategy := range strategies {
		strategyName = append(strategyName, strategy.Name)

	}
	return strategyName
}

// validateStrategies validates the given strategies.
func validateStrategies(strategies []string) error {
	if len(strategies) == 0 {
		err := fmt.Errorf("descheduler should have atleast one strategy enabled and it should be one of %v", strings.Join(validStrategies, ","))
		log.Printf("%v", err)
		return err
	}

	if len(strategies) > len(validStrategies) { // As of now, there are only 4 strategies supported in descheduler.
		err := fmt.Errorf("descheduler can have a maximum of %v strategies enabled at this point of time", len(validStrategies))
		log.Printf("%v", err)
		return err
	}
	// Identify invalid strategies
	invalidStrategies := identifyInvalidStrategies(strategies)
	if len(invalidStrategies) > 0 {
		err := fmt.Errorf("expected one of the %v to be enabled but found following invalid strategies %v",
			strings.Join(validStrategies, ","), strings.Join(invalidStrategies, ","))
		log.Printf("%v", err)
		return err
	}
	return nil
}

// identifyInvalidStrategies collects all the invalid strategies.
func identifyInvalidStrategies(strategies []string) []string {
	invalidStrategiesEnabled := make([]string, 0)
	for _, strategy := range strategies {
		validStrategyFound := false
		for _, validStrategy := range validStrategies {
			if strings.ToUpper(strategy) == strings.ToUpper(validStrategy) {
				validStrategyFound = true
			}
		}
		// Aggregate wrong strategies enabled.
		if !validStrategyFound {
			invalidStrategiesEnabled = append(invalidStrategiesEnabled, strategy)
		}
	}
	return invalidStrategiesEnabled
}

// generateConfigMap generates configmap needed for descheduler from CR.
func (r *ReconcileDescheduler) generateConfigMap(descheduler *deschedulerv1alpha1.Descheduler) error {
	deschedulerConfigMap := &v1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: descheduler.Name, Namespace: descheduler.Namespace}, deschedulerConfigMap)
	if err != nil && errors.IsNotFound(err) {
		// Create a new ConfigMap
		cm, err := r.createConfigMap(descheduler)
		if err != nil {
			log.Printf("%v", err)
			return err
		}
		log.Printf("Creating a new configmap %s/%s\n", cm.Namespace, cm.Name)
		err = r.client.Create(context.TODO(), cm)
		if err != nil {
			return err
		}
	} else if !CheckIfPropertyChanges(descheduler.Spec.Strategies, deschedulerConfigMap.Data) {
		// descheduler strategies got updated. Let's delete the configmap and in next reconcilation phase, we would create a new one.
		// TODO: Delete job as well.
		log.Printf("Strategy mismatch in configmap. Delete it")
		err = r.client.Delete(context.TODO(), deschedulerConfigMap)
		if err != nil {
			log.Printf("Error while deleting configmap")
			return err
		}
		descheduler.Status.Phase = Updating
		return nil
	} else if err != nil {
		return err
	}
	return nil
}

// createConfigmap creates config map from given fields of descheduler
func (r *ReconcileDescheduler) createConfigMap(descheduler *deschedulerv1alpha1.Descheduler) (*v1.ConfigMap, error) {
	log.Printf("Creating config map")
	strategiesPolicyString := generateConfigMapString(descheduler.Spec.Strategies)
	log.Printf("%v", strategiesPolicyString)
	cm := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      descheduler.Name,
			Namespace: descheduler.Namespace,
		},
		Data: map[string]string{
			"policy.yaml": "apiVersion: \"descheduler/v1alpha1\"\nkind: \"DeschedulerPolicy\"\nstrategies:\n" + strategiesPolicyString,
		},
	}
	err := controllerutil.SetControllerReference(descheduler, cm, r.scheme)
	if err != nil {
		return nil, fmt.Errorf("error setting owner references %v", err)
	}
	return cm, nil
}

// generateConfigMapString generates configmap needed for the string.
func generateConfigMapString(requestedStrategies []deschedulerv1alpha1.Strategy) string {
	strategiesPolicyString := ""
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
			strategiesPolicyString = "" // Accept no other strategy except for the valid ones.
		}
	}
	// At last, we will have a "\n", which we don't need.
	return strings.TrimSuffix(strategiesPolicyString, "\n")
}

// addStrategyParamsForLowNodeUtilization adds parameters for low node utilization strategy.
func addStrategyParamsForLowNodeUtilization(params []deschedulerv1alpha1.Param) string {
	thresholds := ""
	targetThresholds := ""
	thresholdsString := "         thresholds:"
	targetThresholdsString := "         targetThresholds:"
	for _, param := range params {
		// collect all thresholds
		log.Printf("%v %v", param.Name, param.Value)
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
	}
	// If threshold is specified we should specify target threshold as well.
	return thresholdsString + "\n" + thresholds + targetThresholdsString + "\n" + targetThresholds
}

// generateDeschedulerJob generates descheduler job.
func (r *ReconcileDescheduler) generateDeschedulerJob(descheduler *deschedulerv1alpha1.Descheduler) error {
	log.Print("Inside generated descheduler job")
	// Check if the cron job already exists
	deschedulerCronJob := &batchv1beta1.CronJob{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: descheduler.Name, Namespace: descheduler.Namespace}, deschedulerCronJob)
	if err != nil && errors.IsNotFound(err) {
		// Create descheduler cronjob
		dj, err := r.createCronJob(descheduler)
		if err != nil {
			log.Printf(" error while creating job %v", err)
			return err
		}
		log.Printf("Creating a new cron job %s/%s\n", dj.Namespace, dj.Name)
		err = r.client.Create(context.TODO(), dj)
		if err != nil {
			log.Printf(" error while creating cron job %v", err)
			return err
		}
		// Cronjob created successfully - don't requeue
		return nil
	} else if deschedulerCronJob.Spec.Schedule != descheduler.Spec.Schedule {
		// descheduler schedule mismatch. Let's delete it and in the next reconcilation loop, we will create a new one.
		log.Printf("Schedule mismatch in cron job. Delete it")
		err = r.client.Delete(context.TODO(), deschedulerCronJob)
		if err != nil {
			log.Printf("Error while deleting configmap")
			return err
		}
		descheduler.Status.Phase = Updating
		return nil
	} else if err != nil {
		return err
	}
	return nil
}

// CheckIfPropertyChanges checks if the given strategies or their params have changed.
func CheckIfPropertyChanges(strategies []deschedulerv1alpha1.Strategy, existingStrategies map[string]string) bool {
	policyString := existingStrategies["policy.yaml"]
	currentPolicyString := "apiVersion: \"descheduler/v1alpha1\"\nkind: \"DeschedulerPolicy\"\nstrategies:\n" + generateConfigMapString(strategies)
	log.Printf("\n%v, \n%v", policyString, currentPolicyString)
	return policyString == currentPolicyString
}

// createCronJob creates a descheduler job.
func (r *ReconcileDescheduler) createCronJob(descheduler *deschedulerv1alpha1.Descheduler) (*batchv1beta1.CronJob, error) {
	log.Printf("Creating descheduler job")
	job := &batchv1beta1.CronJob{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CronJob",
			APIVersion: batch.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      descheduler.Name,
			Namespace: descheduler.Namespace,
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
								Image: "registry.svc.ci.openshift.org/openshift/origin-v4.0:descheduler", // TODO: Make this configurable too.
								Ports: []v1.ContainerPort{{ContainerPort: 80}},
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
								Command: []string{"/bin/descheduler", "--policy-config-file", "/policy-dir/policy.yaml"},
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
	err := controllerutil.SetControllerReference(descheduler, job, r.scheme)
	if err != nil {
		return nil, fmt.Errorf("error setting owner references %v", err)
	}
	return job, nil
}
