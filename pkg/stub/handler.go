package stub

import (
	"fmt"

	"context"
	"github.com/descheduler-io/descheduler-operator/pkg/apis/descheduler/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/sirupsen/logrus"
	batch "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewHandler() sdk.Handler {
	return &Handler{}
}

type Handler struct {
}

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.Descheduler:
		logrus.Infof("Watching all Descheduler objects %v", o)
		// Ignore the delete event since the garbage collector will clean up all secondary resources for the CR
		// All secondary resources must have the CR set as their OwnerReference for this to be the case
		if event.Deleted {
			return nil
		}
		err := checkPrereqs()
		if err != nil {
			return fmt.Errorf("Descheduler prerequisites failed with %v", err)
		}

		deschedulerJob := createDeschedulerJob()
		/*if checkIfDeschedulerPodExists("kube-system") {
			logrus.Infof("Already descheduler pod exists")
			return nil
		}*/

		err = sdk.Create(deschedulerJob)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create descheduler pod: %v", err)
		}
	}
	return nil
}

// checkPrereqs returns true, if descheduler serviceaccount, cluster role-binding and configmap exist.
func checkPrereqs() error {
	if err := checkServiceAccount(); err != nil {
		return err
	}
	if err := checkClusterRole(); err != nil {
		return err
	}
	if err := checkClusterRoleBinding(); err != nil {
		logrus.Errorf("Error while looking/creating cluster role binding: %v", err)
	}
	if err := checkConfigMap(); err != nil {
		logrus.Errorf("Error while looking/creating config map: %v", err)
	}
	return nil
}

func checkConfigMap() error {
	cmList := getConfigMapList()
	listOptions := &metav1.ListOptions{}
	err := sdk.List("kube-system", cmList, sdk.WithListOptions(listOptions))
	if err != nil {
		logrus.Errorf("Error while listing cluster roles %v", err)
		return err
	}
	for _, cm := range cmList.Items {
		if cm.Name == "descheduler-policy-configmap" {
			logrus.Infof("descheduler cluster role exists, no need to create one")
			return nil
		}
	}
	err = sdk.Create(createConfigMap())
	if err != nil {
		logrus.Infof("Error while creating configmap descheduler-policy-configmap in kube-system namespace with %v", err)
		return err
	}
	return nil
}

func checkClusterRole() error {
	CRList := getClusterRole()
	listOptions := &metav1.ListOptions{}
	err := sdk.List(metav1.NamespaceAll, CRList, sdk.WithListOptions(listOptions))
	if err != nil {
		logrus.Errorf("Error while listing cluster roles %v", err)
		return err
	}
	for _, cr := range CRList.Items {
		// As of now, this is hard coded, the service account name.
		if cr.Name == "descheduler-cluster-role" {
			logrus.Infof("descheduler cluster role exists, no need to create one")
			return nil
		}
	}
	logrus.Infof("descheduler service account doesn't exist, creating one")
	err = sdk.Create(createClusterRole())
	if err != nil {
		logrus.Infof("Error while creating service account descheduler-sa in kube-system namespace with %v", err)
		return err
	}
	return nil
}

// checkServiceAccount checks if descheduler sa is available, if not creates one.
func checkServiceAccount() error {
	// As of now, I am listing before creating, if the size of this list is huge, this may cause problems.
	saList := getServiceAccount()
	listOptions := &metav1.ListOptions{}
	err := sdk.List("kube-system", saList, sdk.WithListOptions(listOptions))
	if err != nil {
		logrus.Errorf("Error while listing sa accounts %v", err)
		return err
	}
	for _, sa := range saList.Items {
		// As of now, this is hard coded, the service account name.
		if sa.Name == "descheduler-sa" {
			logrus.Infof("descheduler service account exists, no need to create one")
			return nil
		}
	}
	logrus.Infof("descheduler service account doesn't exist, creating one")
	// Create service account if doesn't exist.
	err = sdk.Create(createServiceAccount())
	if err != nil {
		logrus.Infof("Error while creating service account descheduler-sa in kube-system namespace with %v", err)
		return err
	}
	return nil
}

func createClusterRole() *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "descheduler-cluster-role",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "watch", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "watch", "list", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/eviction"},
				Verbs:     []string{"create"},
			},
		},
	}
}

// checkClusterRoleBinding checks if cluster role binding is available, if not creates one.
func checkClusterRoleBinding() error {
	clusterRoleBindingList := getClusterRoleBinding()
	listOptions := &metav1.ListOptions{}
	err := sdk.List("kube-system", clusterRoleBindingList, sdk.WithListOptions(listOptions))
	if err != nil {
		logrus.Errorf("Error while listing cluster role binding %v", err)
		return err
	}
	for _, crb := range clusterRoleBindingList.Items {
		// As of now, this is hard coded, the service account name.
		if crb.Name == "descheduler-cluster-role-binding" {
			logrus.Infof("descheduler cluster role binding exists, no need to create one")
			return nil
		}
	}
	logrus.Infof("descheduler cluster role binding exists, creating one")
	// Create service account if doesn't exist.
	err = sdk.Create(createClusterRoleBinding())
	if err != nil {
		logrus.Infof("Error while creating service account descheduler-sa in kube-system namespace with %v", err)
		return err
	}
	return nil
}

func createClusterRoleBinding() *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "descheduler-cluster-role-binding",
			Namespace: "kube-system",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "descheduler-cluster-role",
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      "descheduler-sa",
			Namespace: "kube-system",
		}},
	}
}

func createServiceAccount() *v1.ServiceAccount {
	return &v1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "descheduler-sa",
			Namespace: "kube-system",
		},
	}
}

// We need to populate this configmap from properties.
func createConfigMap() *v1.ConfigMap {
	return &v1.ConfigMap {
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "descheduler-policy-configmap",
			Namespace: "kube-system",
		},
		// strategies:\n  \"RemoveDuplicates\":\n    enabled: true
		Data: map[string]string{
			"policy.yaml": "apiVersion: \"descheduler/v1alpha1\"\nkind: \"DeschedulerPolicy\"\nstrategies:\n  \"RemoveDuplicates\":\n     enabled: true\n",
		},
	}
}


func getConfigMapList() *v1.ConfigMapList {
	return &v1.ConfigMapList {
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
	}
}

func getServiceAccount() *v1.ServiceAccountList {
	return &v1.ServiceAccountList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
	}
}

func getClusterRoleBinding() *rbacv1.ClusterRoleBindingList {
	return &rbacv1.ClusterRoleBindingList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
	}
}

func getClusterRole() *rbacv1.ClusterRoleList {
	return &rbacv1.ClusterRoleList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
	}
}

func createDeschedulerJob() *batch.Job {
	activeDeadline := int64(100)
	logrus.Infof("Printing ")
	return &batch.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: batch.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "descheduler-job",
			Namespace: "kube-system",
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
									Name: "descheduler-policy-configmap",
								},
							},
						},
					},
					},
					RestartPolicy: "Never",
					Containers: []v1.Container{{
						Name:  "kube-descheduler",
						Image: "ravig/descheduler",
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
					ServiceAccountName: "descheduler-sa",
				},
			},
		},
	}
}

func checkIfDeschedulerPodExists(namespace string) bool {
	podList := getPodList()
	listOptions := &metav1.ListOptions{}
	err := sdk.List("kube-system", podList, sdk.WithListOptions(listOptions))
	if err != nil {
		logrus.Infof("Error while listing pods")
		return false
	}
	for _, pod := range podList.Items {
		if pod.Name == "descheduler" && (pod.Status.Phase != "Running" || pod.Status.Phase != "Succeded") {
			return true
		}
	}
	return false
}

// getPodList returns a v1.PodList object
func getPodList() *v1.PodList {
	return &v1.PodList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
	}
}

func getJobList() *batch.JobList {
	return &batch.JobList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: batch.SchemeGroupVersion.String(),
		},
	}
}

