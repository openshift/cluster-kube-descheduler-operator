package stub

import (
	"fmt"

	"context"

	"github.com/openshift/descheduler-operator/pkg/apis/descheduler/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/sirupsen/logrus"
	batch "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
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

		// Create descheduler policy config map
		deshedulerConfigMap := createConfigMap()
		err := sdk.Create(deshedulerConfigMap) //TODO: Make this a cron job instead.
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create descheduler pod: %v", err)
		}

		deschedulerJob := createDeschedulerJob()

		err = sdk.Create(deschedulerJob) //TODO: Make this a cron job instead.
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create descheduler pod: %v", err)
		}
	}
	return nil
}

// We need to populate this configmap from CRD and CR fields.
func createConfigMap() *v1.ConfigMap {
	return &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "descheduler-policy-configmap",
			Namespace: "openshift-descheduler-operator",
		},
		Data: map[string]string{
			"policy.yaml": "apiVersion: \"descheduler/v1alpha1\"\nkind: \"DeschedulerPolicy\"\nstrategies:\n  \"RemoveDuplicates\":\n     enabled: true\n",
		},
	}
}

// createDeschedulerJob creates a descheduler job.
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
			Namespace: "openshift-descheduler-operator",
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
						Name:  "openshift-descheduler",
						Image: "ravig/descheduler", //TODO: Change this to official openshift descheduler image, once policy map generation is figured out.
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
}