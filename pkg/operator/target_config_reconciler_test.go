package operator

import (
	"context"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	utilptr "k8s.io/utils/ptr"

	deschedulerv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	operatorconfigclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned/fake"
	bindata "github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/testdata"
)

var configLowNodeUtilization = &configv1.Scheduler{
	Spec: configv1.SchedulerSpec{Policy: configv1.ConfigMapNameReference{Name: ""},
		Profile: configv1.LowNodeUtilization,
	},
}

var configHighNodeUtilization = &configv1.Scheduler{
	Spec: configv1.SchedulerSpec{Policy: configv1.ConfigMapNameReference{Name: ""},
		Profile: configv1.HighNodeUtilization,
	},
}

func TestManageConfigMap(t *testing.T) {
	fm, _ := time.ParseDuration("5m")
	fiveMinutes := metav1.Duration{Duration: fm}
	priority := int32(1000)

	fakeRecorder := NewFakeRecorder(1024)
	tests := []struct {
		name                   string
		targetConfigReconciler *TargetConfigReconciler
		want                   *corev1.ConfigMap
		descheduler            *deschedulerv1.KubeDescheduler
		err                    error
		forceDeployment        bool
	}{
		{
			name: "Podlifetime",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{PodLifetime: &fiveMinutes},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lifecycleAndUtilizationPodLifeTimeCustomizationConfig.yaml"))},
			},
		},
		{
			name: "PvcPods",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization", "EvictPodsWithPVC"},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lifecycleAndUtilizationEvictPvcPodsConfig.yaml"))},
			},
		},
		{
			name: "ThresholdPriorityClassName",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{ThresholdPriorityClassName: "className"},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityClassNameConfig.yaml"))},
			},
		},
		{
			name: "ThresholdPriority",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{ThresholdPriority: &priority},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityConfig.yaml"))},
			},
		},
		{
			name: "ThresholdPriorityClassNameAndValueError",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{ThresholdPriority: &priority, ThresholdPriorityClassName: "className"},
				},
			},
			err: fmt.Errorf("It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields"),
		},
		{
			name: "LowNodeUtilizationLow",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lowNodeUtilizationLowConfig.yaml"))},
			},
		},
		{
			name: "LowNodeUtilizationMedium",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: &deschedulerv1.MediumThreshold},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lowNodeUtilizationMediumConfig.yaml"))},
			},
		},
		{
			name: "LowNodeUtilizationNoCustomization",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lowNodeUtilizationMediumConfig.yaml"))},
			},
		},
		{
			name: "LowNodeUtilizationEmptyDefault",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: utilptr.To[deschedulerv1.LowNodeUtilizationThresholdsType]("")},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lowNodeUtilizationMediumConfig.yaml"))},
			},
		},
		{
			name: "LowNodeUtilizationHigh",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: &deschedulerv1.HighThreshold},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lowNodeUtilizationHighConfig.yaml"))},
			},
		},
		{
			name: "AffinityAndTaintsWithNamespaces",
			targetConfigReconciler: &TargetConfigReconciler{
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"AffinityAndTaints"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{Namespaces: deschedulerv1.Namespaces{
						Included: []string{"includedNamespace"},
					}},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/affinityAndTaintsWithNamespaces.yaml"))},
			},
		},
		{
			name: "LongLifecycleWithNamespaces",
			targetConfigReconciler: &TargetConfigReconciler{
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"LongLifecycle"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{Namespaces: deschedulerv1.Namespaces{
						Included: []string{"includedNamespace"},
					}},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/longLifecycleWithNamespaces.yaml"))},
			},
		},
		{
			name: "LongLifecycleWithLocalStorage",
			targetConfigReconciler: &TargetConfigReconciler{
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"LongLifecycle", "EvictPodsWithLocalStorage"},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/longLifecycleWithLocalStorage.yaml"))},
			},
		},
		{
			name: "SoftTopologyAndDuplicates",
			targetConfigReconciler: &TargetConfigReconciler{
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"SoftTopologyAndDuplicates"},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/softTopologyAndDuplicates.yaml"))},
			},
		},
		{
			name: "TopologyAndDuplicates",
			targetConfigReconciler: &TargetConfigReconciler{
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"TopologyAndDuplicates"},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/topologyAndDuplicates.yaml"))},
			},
		},
		{
			name: "CompactAndScaleWithNamespaces",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{Namespaces: deschedulerv1.Namespaces{
						Included: []string{"includedNamespace"},
					}},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/highNodeUtilizationWithNamespaces.yaml"))},
			},
		},
		{
			name: "CompactAndScaleMinimal",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevHighNodeUtilizationThresholds: &deschedulerv1.CompactMinimalThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/highNodeUtilizationMinimal.yaml"))},
			},
		},
		{
			name: "CompactAndScaleModest",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevHighNodeUtilizationThresholds: &deschedulerv1.CompactModestThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/highNodeUtilization.yaml"))},
			},
		},
		{
			name: "CompactAndScaleDefault",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevHighNodeUtilizationThresholds: utilptr.To[deschedulerv1.HighNodeUtilizationThresholdsType](""),
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/highNodeUtilization.yaml"))},
			},
		},
		{
			name: "CompactAndScaleModerate",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevHighNodeUtilizationThresholds: &deschedulerv1.CompactModerateThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/highNodeUtilizationModerate.yaml"))},
			},
		},
		{
			name: "DevPreviewLongLifecycleAndLifecycleAndUtilizationProfileConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"DevPreviewLongLifecycle", "LifecycleAndUtilization"},
				},
			},
			err:             fmt.Errorf("cannot declare DevPreviewLongLifecycle and LifecycleAndUtilization profiles simultaneously, ignoring"),
			forceDeployment: true,
		},
		{
			name: "LongLifecycleAndLifecycleAndUtilizationProfileConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"LongLifecycle", "LifecycleAndUtilization"},
				},
			},
			err:             fmt.Errorf("cannot declare LongLifecycle and LifecycleAndUtilization profiles simultaneously, ignoring"),
			forceDeployment: true,
		},
		{
			name: "SoftTopologyAndDuplicatesAndTopologyAndDuplicatesProfileConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"SoftTopologyAndDuplicates", "TopologyAndDuplicates"},
				},
			},
			err:             fmt.Errorf("cannot declare SoftTopologyAndDuplicates and TopologyAndDuplicates profiles simultaneously, ignoring"),
			forceDeployment: true,
		},
		{
			name: "CompactAndScaleAndLifecycleAndUtilizationProfileConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale", "LifecycleAndUtilization"},
				},
			},
			err:             fmt.Errorf("cannot declare CompactAndScale and LifecycleAndUtilization profiles simultaneously, ignoring"),
			forceDeployment: true,
		},
		{
			name: "CompactAndScaleAndLongLifecycleProfileConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale", "LongLifecycle"},
				},
			},
			err:             fmt.Errorf("cannot declare CompactAndScale and LongLifecycle profiles simultaneously, ignoring"),
			forceDeployment: true,
		},
		{
			name: "CompactAndScaleAndDevPreviewLongLifecycleProfileConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale", "DevPreviewLongLifecycle"},
				},
			},
			err:             fmt.Errorf("cannot declare CompactAndScale and DevPreviewLongLifecycle profiles simultaneously, ignoring"),
			forceDeployment: true,
		},
		{
			name: "CompactAndScaleAndTopologyAndDuplicatesProfileConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale", "TopologyAndDuplicates"},
				},
			},
			err:             fmt.Errorf("cannot declare CompactAndScale and TopologyAndDuplicates profiles simultaneously, ignoring"),
			forceDeployment: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.targetConfigReconciler == nil {
				tt.targetConfigReconciler = &TargetConfigReconciler{}
			}
			tt.targetConfigReconciler.ctx = context.TODO()
			tt.targetConfigReconciler.kubeClient = fake.NewSimpleClientset()
			tt.targetConfigReconciler.eventRecorder = fakeRecorder
			if tt.targetConfigReconciler.configSchedulerLister == nil {
				tt.targetConfigReconciler.configSchedulerLister = &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configLowNodeUtilization},
				}
			}
			got, forceDeployment, err := tt.targetConfigReconciler.manageConfigMap(tt.descheduler)
			if tt.err != nil {
				if err == nil {
					t.Fatalf("Expected error, not nil\n")
				}
				if tt.err.Error() != err.Error() {
					t.Fatalf("Expected error string: %v, got instead: %v\n", tt.err.Error(), err.Error())
				}
				if tt.forceDeployment != forceDeployment {
					t.Fatalf("Expected forceDeployment to be %v, got %v instead\n", tt.forceDeployment, forceDeployment)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v\n", err)
			}
			if !apiequality.Semantic.DeepEqual(tt.want.Data, got.Data) {
				t.Errorf("manageConfigMap diff \n\n %+v", cmp.Diff(tt.want.Data, got.Data))
			}
		})
	}
}

func TestManageDeployment(t *testing.T) {
	fakeRecorder := NewFakeRecorder(1024)
	tests := []struct {
		name                   string
		targetConfigReconciler *TargetConfigReconciler
		want                   *appsv1.Deployment
		descheduler            *deschedulerv1.KubeDescheduler
		checkContainerOnly     bool
		checkContainerArgsOnly bool
	}{
		{
			name: "NoFeatureGates",
			targetConfigReconciler: &TargetConfigReconciler{
				ctx:           context.TODO(),
				kubeClient:    fake.NewSimpleClientset(),
				eventRecorder: fakeRecorder,
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configLowNodeUtilization},
				},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: utilptr.To[int32](10),
				},
			},
			checkContainerOnly: true,
			want: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "openshift-descheduler",
									Image: "",
									SecurityContext: &corev1.SecurityContext{
										AllowPrivilegeEscalation: utilptr.To[bool](false),
										ReadOnlyRootFilesystem:   utilptr.To[bool](true),
										Capabilities: &corev1.Capabilities{
											Drop: []corev1.Capability{"ALL"},
										},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("500Mi"),
										},
									},
									Command: []string{"/bin/descheduler"},
									Args: []string{
										"--policy-config-file=/policy-dir/policy.yaml",
										"--logging-format=text",
										"--tls-cert-file=/certs-dir/tls.crt",
										"--tls-private-key-file=/certs-dir/tls.key",
										"--descheduling-interval=10s",
										"-v=2",
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "policy-volume",
											MountPath: "/policy-dir",
										},
										{
											Name:      "certs-dir",
											MountPath: "/certs-dir",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "EvictionsInBackground",
			targetConfigReconciler: &TargetConfigReconciler{
				ctx:           context.TODO(),
				kubeClient:    fake.NewSimpleClientset(),
				eventRecorder: fakeRecorder,
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configLowNodeUtilization},
				},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: utilptr.To[int32](10),
					ProfileCustomizations:       &deschedulerv1.ProfileCustomizations{DevEnableEvictionsInBackground: true},
				},
			},
			checkContainerOnly:     true,
			checkContainerArgsOnly: true,
			want: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Args: []string{
										"--policy-config-file=/policy-dir/policy.yaml",
										"--logging-format=text",
										"--tls-cert-file=/certs-dir/tls.crt",
										"--tls-private-key-file=/certs-dir/tls.key",
										"--descheduling-interval=10s",
										"--feature-gates=EvictionsInBackground=true",
										"-v=2",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := tt.targetConfigReconciler.manageDeschedulerDeployment(tt.descheduler, nil)
			if err != nil {
				t.Fatalf("Unexpected error: %v\n", err)
			}
			if tt.checkContainerOnly {
				if tt.checkContainerArgsOnly {
					if !apiequality.Semantic.DeepEqual(tt.want.Spec.Template.Spec.Containers[0].Args, got.Spec.Template.Spec.Containers[0].Args) {
						t.Errorf("manageDeployment diff \n\n %+v", cmp.Diff(tt.want.Spec.Template.Spec.Containers[0].Args, got.Spec.Template.Spec.Containers[0].Args))
					}
				} else {
					if !apiequality.Semantic.DeepEqual(tt.want.Spec.Template.Spec.Containers[0], got.Spec.Template.Spec.Containers[0]) {
						t.Errorf("manageDeployment diff \n\n %+v", cmp.Diff(tt.want.Spec.Template.Spec.Containers[0], got.Spec.Template.Spec.Containers[0]))
					}
				}
			} else {
				if !apiequality.Semantic.DeepEqual(tt.want, got) {
					t.Errorf("manageDeployment diff \n\n %+v", cmp.Diff(tt.want, got))
				}
			}
		})
	}
}

func TestSync(t *testing.T) {
	fakeRecorder := NewFakeRecorder(1024)
	tests := []struct {
		name                   string
		targetConfigReconciler *TargetConfigReconciler
		descheduler            *deschedulerv1.KubeDescheduler
		err                    error
	}{
		{
			name: "Invalid priority threshold configuration",
			targetConfigReconciler: &TargetConfigReconciler{
				ctx:           context.TODO(),
				kubeClient:    fake.NewSimpleClientset(),
				eventRecorder: fakeRecorder,
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configLowNodeUtilization},
				},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster",
					Namespace: "openshift-kube-descheduler-operator",
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: utilptr.To[int32](10),
					Profiles:                    []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations:       &deschedulerv1.ProfileCustomizations{ThresholdPriority: utilptr.To[int32](1000), ThresholdPriorityClassName: "className"},
				},
			},
			err: fmt.Errorf("It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.targetConfigReconciler.operatorClient = operatorconfigclient.NewSimpleClientset(tt.descheduler).KubedeschedulersV1()
			err := tt.targetConfigReconciler.sync()
			if tt.err != nil {
				if err == nil {
					t.Fatalf("Expected error, not nil\n")
				}
				if tt.err.Error() != err.Error() {
					t.Fatalf("Expected error string: %v, got instead: %v\n", tt.err.Error(), err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v\n", err)
			}
		})
	}
}

// A scheduler configuration lister
type fakeSchedConfigLister struct {
	Err   error
	Items map[string]*configv1.Scheduler
}

func (lister *fakeSchedConfigLister) List(selector labels.Selector) ([]*configv1.Scheduler, error) {
	itemsList := make([]*configv1.Scheduler, 0)
	for _, v := range lister.Items {
		itemsList = append(itemsList, v)
	}
	return itemsList, lister.Err
}

func (lister *fakeSchedConfigLister) Get(name string) (*configv1.Scheduler, error) {
	if lister.Err != nil {
		return nil, lister.Err
	}
	item := lister.Items[name]
	if item == nil {
		return nil, errors.NewNotFound(schema.GroupResource{}, name)
	}
	return item, nil
}

// An events recorder
type fakeRecorder struct {
	Events chan string
}

func (f *fakeRecorder) Event(reason, note string) {
	if f.Events != nil {
		f.Events <- fmt.Sprintf(reason + " " + note)
	}
}

func (f *fakeRecorder) Eventf(reason, note string, args ...interface{}) {
	if f.Events != nil {
		f.Events <- fmt.Sprintf(reason+" "+note, args...)
	}
}

func (f *fakeRecorder) Warning(reason, note string) {
	if f.Events != nil {
		f.Events <- fmt.Sprintf(reason + " " + note)
	}
}

func (f *fakeRecorder) Warningf(reason, note string, args ...interface{}) {
	if f.Events != nil {
		f.Events <- fmt.Sprintf(reason+" "+note, args...)
	}
}

func (f *fakeRecorder) ForComponent(componentName string) events.Recorder {
	return *(*(events.Recorder))(unsafe.Pointer(f))
}

func (f *fakeRecorder) WithComponentSuffix(componentNameSuffix string) events.Recorder {
	return *(*(events.Recorder))(unsafe.Pointer(f))
}

func (f *fakeRecorder) WithContext(ctx context.Context) events.Recorder {
	return *(*(events.Recorder))(unsafe.Pointer(f))
}

func (f *fakeRecorder) ComponentName() string {
	return ""
}

func (f *fakeRecorder) Shutdown() {
}

func NewFakeRecorder(bufferSize int) *fakeRecorder {
	return &fakeRecorder{
		Events: make(chan string, bufferSize),
	}
}
