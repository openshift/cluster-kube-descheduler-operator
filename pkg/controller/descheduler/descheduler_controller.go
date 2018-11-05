package descheduler

import (
	"context"
	"log"
	"fmt"

	deschedulerv1alpha1 "github.com/openshift/descheduler-operator/pkg/apis/descheduler/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	batch "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)


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

	deschedulerConfigMap := &v1.ConfigMap{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: descheduler.Name, Namespace: descheduler.Namespace}, deschedulerConfigMap)
	if err != nil && errors.IsNotFound(err) {
		// Create a new ConfigMap
		cm, err := r.createConfigMap(descheduler)
		if err != nil {
			log.Printf("%v", err)
			return reconcile.Result{}, err
		}
		log.Printf("Creating a new configmap %s/%s\n", cm.Namespace, cm.Name)
		err = r.client.Create(context.TODO(), cm)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}
	
	// Check if the job already exists
	deschedulerJob := &batch.Job{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: descheduler.Name, Namespace: descheduler.Namespace}, deschedulerJob)
	if err != nil && errors.IsNotFound(err) {
		// Create descheduler job
		dj, err := r.createJob(descheduler)
		if err != nil {
			log.Printf("%v", err)
			return reconcile.Result{}, err
		}
		log.Printf("Creating a new job %s/%s\n", dj.Namespace, dj.Name)
		err = r.client.Create(context.TODO(), dj)
		if err != nil {
			return reconcile.Result{}, err
		}
		// Pod created successfully - don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}


	return reconcile.Result{}, nil
}

// createConfigmap creates config map from given fields of descheduler
func (r *ReconcileDescheduler) createConfigMap(descheduler *deschedulerv1alpha1.Descheduler) (*v1.ConfigMap, error) {
	log.Printf("Creating config map")
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
			"policy.yaml": "apiVersion: \"descheduler/v1alpha1\"\nkind: \"DeschedulerPolicy\"\nstrategies:\n  \"RemoveDuplicates\":\n     enabled: true\n",
		},
	}
	err := controllerutil.SetControllerReference(descheduler, cm, r.scheme)
	if err != nil {
		return nil, fmt.Errorf("Error setting owner references %v", err)
	}
	return cm, nil
}


// createJob creates a descheduler job.
func (r *ReconcileDescheduler) createJob(descheduler *deschedulerv1alpha1.Descheduler) (*batch.Job, error) {
	activeDeadline := int64(100)
	log.Printf("Creating descheduler job")
	job := &batch.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: batch.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      descheduler.Name,
			Namespace: descheduler.Namespace,
		},
		Spec: batch.JobSpec{
			ActiveDeadlineSeconds: &activeDeadline,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: "descheduler-job-spec",
				},
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
					RestartPolicy: "Never",
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
	}
	err := controllerutil.SetControllerReference(descheduler, job, r.scheme)
	if err != nil {
		return nil, fmt.Errorf("Error setting owner references %v", err)
	}
	return job, nil
}
