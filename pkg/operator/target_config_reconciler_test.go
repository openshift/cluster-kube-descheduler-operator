package operator

import (
	"context"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/google/go-cmp/cmp"
	apiequality "k8s.io/apimachinery/pkg/api/equality"

	configv1 "github.com/openshift/api/config/v1"
	deschedulerv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
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

var configFeatureGateTPNU = &configv1.FeatureGate{
	Spec: configv1.FeatureGateSpec{
		FeatureGateSelection: configv1.FeatureGateSelection{
			FeatureSet: configv1.TechPreviewNoUpgrade,
		},
	},
}

var configFeatureGateDefault = &configv1.FeatureGate{
	Spec: configv1.FeatureGateSpec{
		FeatureGateSelection: configv1.FeatureGateSelection{
			FeatureSet: configv1.Default,
		},
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeCustomizationConfig.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/lifecycleAndUtilizationEvictPvcPodsConfig.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityClassNameConfig.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityConfig.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/lowNodeUtilizationLowConfig.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/lowNodeUtilizationMediumConfig.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/lowNodeUtilizationMediumConfig.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/lowNodeUtilizationHighConfig.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/affinityAndTaintsWithNamespaces.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/longLifecycleWithNamespaces.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/longLifecycleWithLocalStorage.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/softTopologyAndDuplicates.yaml"))},
			},
			forceDeployment: true,
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/topologyAndDuplicates.yaml"))},
			},
			forceDeployment: true,
		},
		{
			name: "CompactAndScaleWithNamespaces",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				configFeatureGateLister: &fakeFeatureGateConfigLister{
					Items: map[string]*configv1.FeatureGate{"cluster": configFeatureGateTPNU},
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
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/highNodeUtilizationWithNamespaces.yaml"))},
			},
			forceDeployment: true,
		},
		{
			name: "CompactAndScaleMinimal",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				configFeatureGateLister: &fakeFeatureGateConfigLister{
					Items: map[string]*configv1.FeatureGate{"cluster": configFeatureGateTPNU},
				},
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						HighNodeUtilizationThresholds: &deschedulerv1.CompactMinimalThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/highNodeUtilizationMinimal.yaml"))},
			},
			forceDeployment: true,
		},
		{
			name: "CompactAndScaleModest",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				configFeatureGateLister: &fakeFeatureGateConfigLister{
					Items: map[string]*configv1.FeatureGate{"cluster": configFeatureGateTPNU},
				},
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						HighNodeUtilizationThresholds: &deschedulerv1.CompactModestThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/highNodeUtilization.yaml"))},
			},
			forceDeployment: true,
		},
		{
			name: "CompactAndScaleModerate",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				configFeatureGateLister: &fakeFeatureGateConfigLister{
					Items: map[string]*configv1.FeatureGate{"cluster": configFeatureGateTPNU},
				},
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						HighNodeUtilizationThresholds: &deschedulerv1.CompactModerateThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/highNodeUtilizationModerate.yaml"))},
			},
			forceDeployment: true,
		},
		{
			name: "CompactAndScaleTPNUNotEnabled",
			targetConfigReconciler: &TargetConfigReconciler{
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configHighNodeUtilization},
				},
				configFeatureGateLister: &fakeFeatureGateConfigLister{
					Items: map[string]*configv1.FeatureGate{"cluster": configFeatureGateDefault},
				},
				protectedNamespaces: []string{"openshift-kube-scheduler", "kube-system"},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"CompactAndScale"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						HighNodeUtilizationThresholds: &deschedulerv1.CompactModerateThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(MustAsset("pkg/operator/testdata/highNodeUtilizationModerate.yaml"))},
			},
			err:             fmt.Errorf("Profile \"CompactAndScale\" not supported without TechPreviewNoUpgrade feature set enabled"),
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
			if tt.targetConfigReconciler.configFeatureGateLister == nil {
				tt.targetConfigReconciler.configFeatureGateLister = &fakeFeatureGateConfigLister{
					Items: map[string]*configv1.FeatureGate{"cluster": configFeatureGateDefault},
				}
			}
			got, forceDeployment, err := tt.targetConfigReconciler.manageConfigMap(tt.descheduler)
			if tt.forceDeployment != forceDeployment {
				t.Fatalf("Expected forceDeployment to be %v, got %v instead\n", tt.forceDeployment, forceDeployment)
			}
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
			if !apiequality.Semantic.DeepEqual(tt.want.Data, got.Data) {
				t.Errorf("manageConfigMap diff \n\n %+v", cmp.Diff(tt.want.Data, got.Data))
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

// A scheduler configuration lister
type fakeFeatureGateConfigLister struct {
	Err   error
	Items map[string]*configv1.FeatureGate
}

func (lister *fakeFeatureGateConfigLister) List(selector labels.Selector) ([]*configv1.FeatureGate, error) {
	itemsList := make([]*configv1.FeatureGate, 0)
	for _, v := range lister.Items {
		itemsList = append(itemsList, v)
	}
	return itemsList, lister.Err
}

func (lister *fakeFeatureGateConfigLister) Get(name string) (*configv1.FeatureGate, error) {
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
