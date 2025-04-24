package operator

import (
	"context"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	utilptr "k8s.io/utils/ptr"

	fakeconfigv1client "github.com/openshift/client-go/config/clientset/versioned/fake"
	configv1informers "github.com/openshift/client-go/config/informers/externalversions"
	fakeroutev1client "github.com/openshift/client-go/route/clientset/versioned/fake"
	routev1informers "github.com/openshift/client-go/route/informers/externalversions"
	deschedulerv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	operatorconfigclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	operatorconfigclientfake "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned/fake"
	operatorclientinformers "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/informers/externalversions"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	bindata "github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/testdata"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/softtainter"
	coreinformers "k8s.io/client-go/informers"
)

var configLowNodeUtilization = &configv1.Scheduler{
	ObjectMeta: metav1.ObjectMeta{
		Name: "cluster",
	},
	Spec: configv1.SchedulerSpec{Policy: configv1.ConfigMapNameReference{Name: ""},
		Profile: configv1.LowNodeUtilization,
	},
}

var configHighNodeUtilization = &configv1.Scheduler{
	ObjectMeta: metav1.ObjectMeta{
		Name: "cluster",
	},
	Spec: configv1.SchedulerSpec{Policy: configv1.ConfigMapNameReference{Name: ""},
		Profile: configv1.HighNodeUtilization,
	},
}

func initTargetConfigReconciler(ctx context.Context, kubeClientObjects, configObjects, routesObjects, deschedulerObjects []runtime.Object) (*TargetConfigReconciler, operatorconfigclient.Interface) {
	fakeKubeClient := fake.NewSimpleClientset(kubeClientObjects...)
	operatorConfigClient := operatorconfigclientfake.NewSimpleClientset(deschedulerObjects...)
	operatorConfigInformers := operatorclientinformers.NewSharedInformerFactory(operatorConfigClient, 10*time.Minute)
	deschedulerClient := &operatorclient.DeschedulerClient{
		Ctx:            ctx,
		SharedInformer: operatorConfigInformers.Kubedeschedulers().V1().KubeDeschedulers().Informer(),
		OperatorClient: operatorConfigClient.KubedeschedulersV1(),
	}
	openshiftConfigClient := fakeconfigv1client.NewSimpleClientset(configObjects...)
	configInformers := configv1informers.NewSharedInformerFactory(openshiftConfigClient, 10*time.Minute)
	openshiftRouteClient := fakeroutev1client.NewSimpleClientset(routesObjects...)
	routeInformers := routev1informers.NewSharedInformerFactory(openshiftRouteClient, 10*time.Minute)
	coreInformers := coreinformers.NewSharedInformerFactory(fakeKubeClient, 10*time.Minute)
	scheme := runtime.NewScheme()

	targetConfigReconciler := NewTargetConfigReconciler(
		ctx,
		"RELATED_IMAGE_OPERAND_IMAGE",
		"RELATED_IMAGE_SOFTTAINTER_IMAGE",
		operatorConfigClient.KubedeschedulersV1(),
		operatorConfigInformers.Kubedeschedulers().V1().KubeDeschedulers(),
		deschedulerClient,
		fakeKubeClient,
		dynamicfake.NewSimpleDynamicClient(scheme),
		configInformers,
		routeInformers,
		coreInformers,
		NewFakeRecorder(1024),
	)

	operatorConfigInformers.Start(ctx.Done())
	configInformers.Start(ctx.Done())
	routeInformers.Start(ctx.Done())
	coreInformers.Start(ctx.Done())

	operatorConfigInformers.WaitForCacheSync(ctx.Done())
	configInformers.WaitForCacheSync(ctx.Done())
	routeInformers.WaitForCacheSync(ctx.Done())
	coreInformers.WaitForCacheSync(ctx.Done())

	return targetConfigReconciler, operatorConfigClient
}

func TestManageConfigMap(t *testing.T) {
	fm, _ := time.ParseDuration("5m")
	fiveMinutes := metav1.Duration{Duration: fm}
	priority := int32(1000)

	tests := []struct {
		name            string
		schedulerConfig *configv1.Scheduler
		want            *corev1.ConfigMap
		descheduler     *deschedulerv1.KubeDescheduler
		routes          []runtime.Object
		nodes           []runtime.Object
		err             error
		forceDeployment bool
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
			name: "LowNodeUtilizationIncludedNamespace",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"LifecycleAndUtilization"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold,
						Namespaces: deschedulerv1.Namespaces{
							Included: []string{"includedNamespace"},
						},
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/lowNodeUtilizationIncludedNamespace.yaml"))},
			},
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
			name: "RelieveAndMigrateWithoutCustomizations",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: nil,
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/relieveAndMigrateDefaults.yaml"))},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
		},
		{
			name: "RelieveAndMigrateLow",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/relieveAndMigrateLowConfig.yaml"))},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
		},
		{
			name: "RelieveAndMigrateMedium",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: &deschedulerv1.MediumThreshold},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/relieveAndMigrateMediumConfig.yaml"))},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
		},
		{
			name: "RelieveAndMigrateDeviationLowWithCombinedMetrics",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds:      &deschedulerv1.LowDeviationThreshold,
						DevActualUtilizationProfile: deschedulerv1.PrometheusCPUCombinedProfile,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/relieveAndMigrateDeviationLowWithCombinedMetrics.yaml"))},
			},
			routes: []runtime.Object{
				&routev1.Route{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "openshift-monitoring",
						Name:      "prometheus-k8s",
					},
					Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
						{
							Host: "prometheus-k8s-openshift-monitoring.apps.example.com",
						},
					},
					},
				},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
		},
		{
			name: "RelieveAndMigrateHigh",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles:              []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{DevLowNodeUtilizationThresholds: &deschedulerv1.HighThreshold},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/relieveAndMigrateHighConfig.yaml"))},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
		},
		{
			name: "RelieveAndMigrateIncludedNamespace",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						Namespaces: deschedulerv1.Namespaces{
							Included: []string{"includedNamespace"},
						},
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/relieveAndMigrateIncludedNamespace.yaml"))},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
		},
		{
			name: "RelieveAndMigrateDynamicThresholdsLow",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/relieveAndMigrateDynamicThresholdsLow.yaml"))},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
		},
		{
			name: "RelieveAndMigrateDynamicThresholdsMedium",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.MediumDeviationThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/relieveAndMigrateDynamicThresholdsMedium.yaml"))},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
		},
		{
			name: "RelieveAndMigrateDynamicThresholdsHigh",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.HighDeviationThreshold,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/relieveAndMigrateDynamicThresholdsHigh.yaml"))},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
		},
		{
			name: "RelieveAndMigrateDynamicAndStaticThresholds",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds:          &deschedulerv1.LowDeviationThreshold,
						DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold,
					},
				},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
			err: fmt.Errorf("only one of DevLowNodeUtilizationThresholds and DevDeviationThresholds customizations can be configured simultaneously"),
		},
		{
			name: "RelieveAndMigrateWithoutKubeVirt",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"DevKubeVirtRelieveAndMigrate"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds:          &deschedulerv1.LowDeviationThreshold,
						DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold,
					},
				},
			},
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
					},
				},
			},
			err:             fmt.Errorf("profile DevKubeVirtRelieveAndMigrate can only be used when KubeVirt is properly deployed"),
			forceDeployment: true,
		},
		{
			name: "AffinityAndTaintsWithNamespaces",
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
			name: "LongLifecycleWithMetrics",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{"LongLifecycle"},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevActualUtilizationProfile: deschedulerv1.PrometheusCPUUsageProfile,
					},
				},
			},
			want: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				Data:     map[string]string{"policy.yaml": string(bindata.MustAsset("assets/longLifecycleWithMetrics.yaml"))},
			},
			routes: []runtime.Object{
				&routev1.Route{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "openshift-monitoring",
						Name:      "prometheus-k8s",
					},
					Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
						{
							Host: "prometheus-k8s-openshift-monitoring.apps.example.com",
						},
					},
					},
				},
			},
		},
		{
			name: "SoftTopologyAndDuplicates",
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
			name:            "CompactAndScaleWithNamespaces",
			schedulerConfig: configHighNodeUtilization,
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
			name:            "CompactAndScaleMinimal",
			schedulerConfig: configHighNodeUtilization,
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
			name:            "CompactAndScaleModest",
			schedulerConfig: configHighNodeUtilization,
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
			name:            "CompactAndScaleDefault",
			schedulerConfig: configHighNodeUtilization,
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
			name:            "CompactAndScaleModerate",
			schedulerConfig: configHighNodeUtilization,
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
		{
			name: "DevPreviewLongLifecycleAndRelieveAndMigrateConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{deschedulerv1.DevPreviewLongLifecycle, deschedulerv1.RelieveAndMigrate},
				},
			},
			err:             fmt.Errorf("cannot declare %v and %v profiles simultaneously, ignoring", deschedulerv1.DevPreviewLongLifecycle, deschedulerv1.RelieveAndMigrate),
			forceDeployment: true,
		},
		{
			name: "LongLifecycleAndRelieveAndMigrateConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle, deschedulerv1.RelieveAndMigrate},
				},
			},
			err:             fmt.Errorf("cannot declare %v and %v profiles simultaneously, ignoring", deschedulerv1.LongLifecycle, deschedulerv1.RelieveAndMigrate),
			forceDeployment: true,
		},
		{
			name: "LifecycleAndUtilizationAndRelieveAndMigrateConflict",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization, deschedulerv1.RelieveAndMigrate},
				},
			},
			err:             fmt.Errorf("cannot declare %v and %v profiles simultaneously, ignoring", deschedulerv1.LifecycleAndUtilization, deschedulerv1.RelieveAndMigrate),
			forceDeployment: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.schedulerConfig == nil {
				tt.schedulerConfig = configLowNodeUtilization
			}

			objects := []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "openshift-kube-scheduler",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kube-system",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   operatorclient.OperatorNamespace,
						Labels: map[string]string{operatorclient.OpenshiftClusterMonitoringLabelKey: operatorclient.OpenshiftClusterMonitoringLabelValue},
					},
				},
			}
			objects = append(objects, tt.nodes...)

			ctx, cancelFunc := context.WithCancel(context.TODO())
			defer cancelFunc()

			targetConfigReconciler, _ := initTargetConfigReconciler(ctx, objects, []runtime.Object{tt.schedulerConfig}, tt.routes, nil)

			got, forceDeployment, err := targetConfigReconciler.manageConfigMap(tt.descheduler)
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
		{
			name: "RelieveAndMigrate enables EvictionsInBackground by default",
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
					Profiles:                    []deschedulerv1.DeschedulerProfile{deschedulerv1.RelieveAndMigrate},
					DeschedulingIntervalSeconds: utilptr.To[int32](10),
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

func TestManageSoftTainterDeployment(t *testing.T) {
	ctx, cancelFunc := context.WithCancel(context.TODO())
	defer cancelFunc()
	expectedSoftTainterDeployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "softtainter",
			Annotations:     map[string]string{"operator.openshift.io/spec-hash": "8876109bce3c5c9336bcde77e391abd07b117797b8646bd5d61322509f3df970"},
			Labels:          map[string]string{"app": "softtainer"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "operator.openshift.io/v1", Kind: "KubeDescheduler"}},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "softtainer",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "softtainer"},
					Annotations: map[string]string{"kubectl.kubernetes.io/default-container": "openshift-softtainer"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "openshift-softtainer",
							Command: []string{"/usr/bin/soft-tainter"},
							Args: []string{
								"--policy-config-file=/policy-dir/policy.yaml",
								"-v=2",
							},
							Image: "RELATED_IMAGE_SOFTTAINTER_IMAGE",
							LivenessProbe: &corev1.Probe{
								ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/livez", Port: intstr.FromInt32(6060), Scheme: corev1.URISchemeHTTP}},
								InitialDelaySeconds: 30,
								PeriodSeconds:       5,
								FailureThreshold:    1,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(6060), Scheme: corev1.URISchemeHTTP}},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
								FailureThreshold:    1,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("500Mi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: utilptr.To(false),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
								ReadOnlyRootFilesystem:   utilptr.To(true),
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
					PriorityClassName: "system-cluster-critical",
					RestartPolicy:     corev1.RestartPolicyAlways,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: utilptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					ServiceAccountName: "openshift-descheduler-softtainter",
					Volumes: []corev1.Volume{
						{
							Name:         "policy-volume",
							VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{}}},
						},
						{
							Name:         "certs-dir",
							VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "kube-descheduler-serving-cert"}},
						},
					},
				},
			},
		},
	}
	tests := []struct {
		name                   string
		want                   *appsv1.Deployment
		descheduler            *deschedulerv1.KubeDescheduler
		objects                []runtime.Object
		checkContainerOnly     bool
		checkContainerArgsOnly bool
	}{
		{
			name: "RelieveAndMigrate",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{deschedulerv1.RelieveAndMigrate},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds:      &deschedulerv1.LowDeviationThreshold,
						DevActualUtilizationProfile: deschedulerv1.PrometheusCPUCombinedProfile,
					},
					DeschedulingIntervalSeconds: utilptr.To[int32](10),
				},
			},
			checkContainerOnly:     false,
			checkContainerArgsOnly: false,
			want:                   expectedSoftTainterDeployment,
		},
		{
			name: "LifecycleAndUtilization (without the softtainer) and no leftovers on existing nodes",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds:      &deschedulerv1.LowDeviationThreshold,
						DevActualUtilizationProfile: deschedulerv1.PrometheusCPUCombinedProfile,
					},
					DeschedulingIntervalSeconds: utilptr.To[int32](10),
				},
			},
			objects: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{Key: "extra1", Value: "extra1", Effect: corev1.TaintEffectNoSchedule},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{Key: "extra2", Value: "extra2", Effect: corev1.TaintEffectPreferNoSchedule},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node3",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
			checkContainerOnly:     false,
			checkContainerArgsOnly: false,
			want:                   nil,
		},
		{
			name: "LifecycleAndUtilization (without the softtainer) but a leftover on existing nodes - 1",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds:      &deschedulerv1.LowDeviationThreshold,
						DevActualUtilizationProfile: deschedulerv1.PrometheusCPUCombinedProfile,
					},
					DeschedulingIntervalSeconds: utilptr.To[int32](10),
				},
			},
			objects: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{Key: softtainter.AppropriatelyUtilizedSoftTaintKey, Value: softtainter.AppropriatelyUtilizedSoftTaintKey, Effect: corev1.TaintEffectPreferNoSchedule},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node3",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
				},
			},
			checkContainerOnly:     false,
			checkContainerArgsOnly: false,
			want:                   expectedSoftTainterDeployment,
		},
		{
			name: "LifecycleAndUtilization (without the softtainer) but a leftover on existing nodes - 2",
			descheduler: &deschedulerv1.KubeDescheduler{
				Spec: deschedulerv1.KubeDeschedulerSpec{
					Profiles: []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization},
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds:      &deschedulerv1.LowDeviationThreshold,
						DevActualUtilizationProfile: deschedulerv1.PrometheusCPUCombinedProfile,
					},
					DeschedulingIntervalSeconds: utilptr.To[int32](10),
				},
			},
			objects: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node1",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{Key: softtainter.OverUtilizedSoftTaintKey, Value: softtainter.OverUtilizedSoftTaintKey, Effect: corev1.TaintEffectPreferNoSchedule},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{Key: softtainter.AppropriatelyUtilizedSoftTaintKey, Value: softtainter.AppropriatelyUtilizedSoftTaintKey, Effect: corev1.TaintEffectPreferNoSchedule},
							{Key: softtainter.OverUtilizedSoftTaintKey, Value: softtainter.OverUtilizedSoftTaintKey, Effect: corev1.TaintEffectPreferNoSchedule},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node3",
						Labels: map[string]string{"kubevirt.io/schedulable": "true"},
					},
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{Key: softtainter.AppropriatelyUtilizedSoftTaintKey, Value: softtainter.AppropriatelyUtilizedSoftTaintKey, Effect: corev1.TaintEffectPreferNoSchedule},
						},
					},
				},
			},
			checkContainerOnly:     false,
			checkContainerArgsOnly: false,
			want:                   expectedSoftTainterDeployment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			targetConfigReconciler, _ := initTargetConfigReconciler(ctx, tt.objects, nil, nil, nil)

			enabled, err := targetConfigReconciler.isSoftTainterNeeded(tt.descheduler)
			if err != nil {
				t.Fatalf("Unexpected error: %v\n", err)
			}
			got, _, err := targetConfigReconciler.manageSoftTainterDeployment(tt.descheduler, nil, enabled)
			if err != nil {
				t.Fatalf("Unexpected error: %v\n", err)
			}
			if tt.checkContainerOnly {
				if tt.checkContainerArgsOnly {
					if !apiequality.Semantic.DeepEqual(tt.want.Spec.Template.Spec.Containers[0].Args, got.Spec.Template.Spec.Containers[0].Args) {
						t.Errorf("manageSoftTainterDeployment diff \n\n %+v", cmp.Diff(tt.want.Spec.Template.Spec.Containers[0].Args, got.Spec.Template.Spec.Containers[0].Args))
					}
				} else {
					if !apiequality.Semantic.DeepEqual(tt.want.Spec.Template.Spec.Containers[0], got.Spec.Template.Spec.Containers[0]) {
						t.Errorf("manageSoftTainterDeployment diff \n\n %+v", cmp.Diff(tt.want.Spec.Template.Spec.Containers[0], got.Spec.Template.Spec.Containers[0]))
					}
				}
			} else {
				if !apiequality.Semantic.DeepEqual(tt.want, got) {
					t.Errorf("manageSoftTainterDeployment diff \n\n %+v", cmp.Diff(tt.want, got))
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
		condition              *operatorv1.OperatorCondition
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
		{
			name: "TargetConfigControllerDegraded kubevirt not deployed with RelieveAndMigrate profile",
			targetConfigReconciler: &TargetConfigReconciler{
				ctx:           context.TODO(),
				kubeClient:    fake.NewSimpleClientset(),
				eventRecorder: fakeRecorder,
				configSchedulerLister: &fakeSchedConfigLister{
					Items: map[string]*configv1.Scheduler{"cluster": configLowNodeUtilization},
				},
			},
			descheduler: &deschedulerv1.KubeDescheduler{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "KubeDescheduler"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      operatorclient.OperatorConfigName,
					Namespace: operatorclient.OperatorNamespace,
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: utilptr.To[int32](10),
					Profiles:                    []deschedulerv1.DeschedulerProfile{deschedulerv1.RelieveAndMigrate},
					ProfileCustomizations:       &deschedulerv1.ProfileCustomizations{},
				},
			},
			condition: &operatorv1.OperatorCondition{
				Type:   "TargetConfigControllerDegraded",
				Status: operatorv1.ConditionTrue,
				Reason: fmt.Sprintf("profile %v can only be used when KubeVirt is properly deployed", deschedulerv1.RelieveAndMigrate),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			ctx := context.TODO()

			targetConfigReconciler, operatorClient := initTargetConfigReconciler(
				ctx,
				[]runtime.Object{
					&appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      operatorclient.OperandName,
							Namespace: operatorclient.OperatorNamespace,
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: utilptr.To[int32](1),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{},
							},
						},
					},
				},
				[]runtime.Object{configLowNodeUtilization},
				nil,
				[]runtime.Object{tt.descheduler},
			)

			err := targetConfigReconciler.sync()
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
			if tt.condition != nil {
				kubeDeschedulerObj, err := operatorClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Unable to get kubedescheduler object: %v", err)
				}
				found := false
				for _, condition := range kubeDeschedulerObj.Status.Conditions {
					if condition.Type == tt.condition.Type {
						found = true
						if condition.Status != tt.condition.Status {
							t.Fatalf("Expected %q condition status to be %v, got %v instead", condition.Type, tt.condition.Status, condition.Status)
						}
						if condition.Reason != tt.condition.Reason {
							t.Fatalf("Expected %q condition reason to be %q, got %q instead", condition.Type, tt.condition.Reason, condition.Reason)
						}
						break
					}
				}
				if !found {
					t.Fatalf("Unable to find %q condition in the kubedescheduler's status", tt.condition.Type)
				}
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
