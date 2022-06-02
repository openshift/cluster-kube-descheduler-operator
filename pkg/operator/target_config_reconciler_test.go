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

const podLifeTimeConfig = `apiVersion: descheduler/v1alpha1
ignorePvcPods: true
kind: DeschedulerPolicy
strategies:
  LowNodeUtilization:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces: null
      nodeFit: false
      nodeResourceUtilizationThresholds:
        targetThresholds:
          cpu: 50
          memory: 50
          pods: 50
        thresholds:
          cpu: 20
          memory: 20
          pods: 20
      thresholdPriority: null
      thresholdPriorityClassName: ""
  PodLifeTime:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podLifeTime:
        maxPodLifeTimeSeconds: 300
      thresholdPriority: null
      thresholdPriorityClassName: ""
  RemovePodsHavingTooManyRestarts:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podsHavingTooManyRestarts:
        includingInitContainers: true
        podRestartThreshold: 100
      thresholdPriority: null
      thresholdPriorityClassName: ""
`

const podLifeTimeWithThresholdPriorityClassNameConfig = `apiVersion: descheduler/v1alpha1
ignorePvcPods: true
kind: DeschedulerPolicy
strategies:
  LowNodeUtilization:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces: null
      nodeFit: false
      nodeResourceUtilizationThresholds:
        targetThresholds:
          cpu: 50
          memory: 50
          pods: 50
        thresholds:
          cpu: 20
          memory: 20
          pods: 20
      thresholdPriority: null
      thresholdPriorityClassName: className
  PodLifeTime:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podLifeTime:
        maxPodLifeTimeSeconds: 86400
      thresholdPriority: null
      thresholdPriorityClassName: className
  RemovePodsHavingTooManyRestarts:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podsHavingTooManyRestarts:
        includingInitContainers: true
        podRestartThreshold: 100
      thresholdPriority: null
      thresholdPriorityClassName: className
`

const podLifeTimeWithThresholdPriorityConfig = `apiVersion: descheduler/v1alpha1
ignorePvcPods: true
kind: DeschedulerPolicy
strategies:
  LowNodeUtilization:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces: null
      nodeFit: false
      nodeResourceUtilizationThresholds:
        targetThresholds:
          cpu: 50
          memory: 50
          pods: 50
        thresholds:
          cpu: 20
          memory: 20
          pods: 20
      thresholdPriority: 1000
      thresholdPriorityClassName: ""
  PodLifeTime:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podLifeTime:
        maxPodLifeTimeSeconds: 86400
      thresholdPriority: 1000
      thresholdPriorityClassName: ""
  RemovePodsHavingTooManyRestarts:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podsHavingTooManyRestarts:
        includingInitContainers: true
        podRestartThreshold: 100
      thresholdPriority: 1000
      thresholdPriorityClassName: ""
`

const evictPvcPodsConfig = `apiVersion: descheduler/v1alpha1
ignorePvcPods: false
kind: DeschedulerPolicy
strategies:
  LowNodeUtilization:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces: null
      nodeFit: false
      nodeResourceUtilizationThresholds:
        targetThresholds:
          cpu: 50
          memory: 50
          pods: 50
        thresholds:
          cpu: 20
          memory: 20
          pods: 20
      thresholdPriority: null
      thresholdPriorityClassName: ""
  PodLifeTime:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podLifeTime:
        maxPodLifeTimeSeconds: 86400
      thresholdPriority: null
      thresholdPriorityClassName: ""
  RemovePodsHavingTooManyRestarts:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podsHavingTooManyRestarts:
        includingInitContainers: true
        podRestartThreshold: 100
      thresholdPriority: null
      thresholdPriorityClassName: ""
`

const lowNodeUtilizationLowConfig = `apiVersion: descheduler/v1alpha1
ignorePvcPods: true
kind: DeschedulerPolicy
strategies:
  LowNodeUtilization:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces: null
      nodeFit: false
      nodeResourceUtilizationThresholds:
        targetThresholds:
          cpu: 30
          memory: 30
          pods: 30
        thresholds:
          cpu: 10
          memory: 10
          pods: 10
      thresholdPriority: null
      thresholdPriorityClassName: ""
  PodLifeTime:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podLifeTime:
        maxPodLifeTimeSeconds: 86400
      thresholdPriority: null
      thresholdPriorityClassName: ""
  RemovePodsHavingTooManyRestarts:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podsHavingTooManyRestarts:
        includingInitContainers: true
        podRestartThreshold: 100
      thresholdPriority: null
      thresholdPriorityClassName: ""
`

const lowNodeUtilizationMediumConfig = `apiVersion: descheduler/v1alpha1
ignorePvcPods: true
kind: DeschedulerPolicy
strategies:
  LowNodeUtilization:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces: null
      nodeFit: false
      nodeResourceUtilizationThresholds:
        targetThresholds:
          cpu: 50
          memory: 50
          pods: 50
        thresholds:
          cpu: 20
          memory: 20
          pods: 20
      thresholdPriority: null
      thresholdPriorityClassName: ""
  PodLifeTime:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podLifeTime:
        maxPodLifeTimeSeconds: 86400
      thresholdPriority: null
      thresholdPriorityClassName: ""
  RemovePodsHavingTooManyRestarts:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podsHavingTooManyRestarts:
        includingInitContainers: true
        podRestartThreshold: 100
      thresholdPriority: null
      thresholdPriorityClassName: ""
`

const lowNodeUtilizationHighConfig = `apiVersion: descheduler/v1alpha1
ignorePvcPods: true
kind: DeschedulerPolicy
strategies:
  LowNodeUtilization:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces: null
      nodeFit: false
      nodeResourceUtilizationThresholds:
        targetThresholds:
          cpu: 70
          memory: 70
          pods: 70
        thresholds:
          cpu: 40
          memory: 40
          pods: 40
      thresholdPriority: null
      thresholdPriorityClassName: ""
  PodLifeTime:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podLifeTime:
        maxPodLifeTimeSeconds: 86400
      thresholdPriority: null
      thresholdPriorityClassName: ""
  RemovePodsHavingTooManyRestarts:
    enabled: true
    params:
      includePreferNoSchedule: false
      includeSoftConstraints: false
      labelSelector: null
      namespaces:
        exclude: null
        include: []
      nodeFit: false
      podsHavingTooManyRestarts:
        includingInitContainers: true
        podRestartThreshold: 100
      thresholdPriority: null
      thresholdPriorityClassName: ""
`

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
	}{
		{
			name: "Podlifetime",
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
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{PodLifetime: &fiveMinutes},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": podLifeTimeConfig},
			},
		},
		{
			name: "PvcPods",
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
					Profiles: []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization", "EvictPodsWithPVC"},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": evictPvcPodsConfig},
			},
		},
		{
			name: "ThresholdPriorityClassName",
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
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{ThresholdPriorityClassName: "className"},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": podLifeTimeWithThresholdPriorityClassNameConfig},
			},
		},
		{
			name: "ThresholdPriority",
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
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{ThresholdPriority: &priority},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": podLifeTimeWithThresholdPriorityConfig},
			},
		},
		{
			name: "LowNodeUtilizationLow",
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
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": lowNodeUtilizationLowConfig},
			},
		},
		{
			name: "LowNodeUtilizationMedium",
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
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: &deschedulerv1.MediumThreshold},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": lowNodeUtilizationMediumConfig},
			},
		},
		{
			name: "LowNodeUtilizationNoCustomization",
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
					Profiles: []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": lowNodeUtilizationMediumConfig},
			},
		},
		{
			name: "LowNodeUtilizationHigh",
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
					Profiles:              []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: &deschedulerv1.HighThreshold},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": lowNodeUtilizationHighConfig},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _ := tt.targetConfigReconciler.manageConfigMap(tt.descheduler)
			if !apiequality.Semantic.DeepEqual(tt.want.Data, got.Data) {
				t.Errorf("manageConfigMap diff \n\n %+v", cmp.Diff(tt.want.Data, got.Data))
			}
		})
	}
}

var configLowNodeUtilization = &configv1.Scheduler{
	Spec: configv1.SchedulerSpec{Policy: configv1.ConfigMapNameReference{Name: ""},
		Profile: configv1.LowNodeUtilization,
	},
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
