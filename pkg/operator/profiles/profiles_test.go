package profiles

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	deschedulerv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilptr "k8s.io/utils/ptr"
)

const (
	testPrometheusHost    = "prometheus-k8s.openshift-monitoring.svc:9091"
	testIncludedNamespace = "includedNamespace"
	testExcludedNamespace = "excludedNamespace"
	testPriorityClassName = "className"
)

var protectedNamespaces = []string{"kube-system", "hypershift", "openshift", "openshift-kube-descheduler-operator", "openshift-kube-scheduler"}

func TestAffinityAndTaints(t *testing.T) {
	tests := []struct {
		name             string
		descheduler      *deschedulerv1.KubeDescheduler
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "When no customizations are provided, it should generate default profile",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints}
			}),
		},
		{
			name: "When included namespaces are specified, it should set namespace include filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
					},
				}
			}),
		},
		{
			name: "When excluded namespaces are specified, it should set namespace exclude filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
		},
		{
			name: "When both included and excluded namespaces are specified, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is forbidden to combine both included and excluded namespaces",
		},
		{
			name: "When threshold priority class name is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
		},
		{
			name: "When threshold priority value is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority: utilptr.To(int32(1000)),
				}
			}),
		},
		{
			name: "When both priority class name and value are set, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority:          utilptr.To(int32(1000)),
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields",
		},
		{
			name: "When ignorePvcPods is false, it should allow eviction of pods with PVC",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints, deschedulerv1.EvictPodsWithPVC}
			}),
		},
		{
			name: "When evictLocalStoragePods is true, it should allow eviction of pods with local storage",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints, deschedulerv1.EvictPodsWithLocalStorage}
			}),
		},
		{
			name: "When all options are specified, it should generate profile with all customizations",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.AffinityAndTaints, deschedulerv1.EvictPodsWithPVC, deschedulerv1.EvictPodsWithLocalStorage}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority: utilptr.To(int32(500)),
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{"ns1", "ns2"},
					},
				}
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := BuildDeschedulingPolicy(
				tt.descheduler,
				protectedNamespaces,
				"",
			)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.expectedErrorMsg != "" && err.Error() != tt.expectedErrorMsg {
					t.Errorf("expected error %q, got %q", tt.expectedErrorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			testutil.CompareWithFixture(t, policy)
		})
	}
}

func TestLifecycleAndUtilization(t *testing.T) {
	fiveMinutes := metav1.Duration{Duration: 5 * time.Minute}

	tests := []struct {
		name             string
		descheduler      *deschedulerv1.KubeDescheduler
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "When no customizations are provided, it should generate default profile",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
			}),
		},
		{
			name: "When pod lifetime is customized, it should set MaxPodLifeTimeSeconds",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					PodLifetime: &fiveMinutes,
				}
			}),
		},
		{
			name: "When ignorePvcPods is false, it should allow eviction of pods with PVC",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization, deschedulerv1.EvictPodsWithPVC}
			}),
		},
		{
			name: "When evictLocalStoragePods is true, it should allow eviction of pods with local storage",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization, deschedulerv1.EvictPodsWithLocalStorage}
			}),
		},
		{
			name: "When both EvictPodsWithPVC and EvictPodsWithLocalStorage are enabled, it should configure DefaultEvictor accordingly",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization, deschedulerv1.EvictPodsWithPVC, deschedulerv1.EvictPodsWithLocalStorage}
			}),
		},
		{
			name: "When threshold priority class name is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
		},
		{
			name: "When threshold priority value is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority: utilptr.To(int32(1000)),
				}
			}),
		},
		{
			name: "When both priority class name and value are set, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority:          utilptr.To(int32(1000)),
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields",
		},
		{
			name: "When included namespaces are specified, it should set namespace include filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
					},
				}
			}),
		},
		{
			name: "When excluded namespaces are specified, it should set namespace exclude filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
		},
		{
			name: "When both included and excluded namespaces are specified, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is forbidden to combine both included and excluded namespaces",
		},
		{
			name: "When LowNodeUtilization threshold is Low, it should set thresholds to 10 and 30",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold,
				}
			}),
		},
		{
			name: "When LowNodeUtilization threshold is Medium, it should set thresholds to 20 and 50",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: &deschedulerv1.MediumThreshold,
				}
			}),
		},
		{
			name: "When LowNodeUtilization threshold is empty string, it should default to Medium thresholds",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: utilptr.To[deschedulerv1.LowNodeUtilizationThresholdsType](""),
				}
			}),
		},
		{
			name: "When LowNodeUtilization threshold is High, it should set thresholds to 40 and 70",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: &deschedulerv1.HighThreshold,
				}
			}),
		},
		{
			name: "When eviction limits are specified, it should set MaxNoOfPodsToEvict fields",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization}
				spec.EvictionLimits = &deschedulerv1.EvictionLimits{
					Total: utilptr.To(int32(10)),
					Node:  utilptr.To(int32(3)),
				}
			}),
		},
		{
			name: "When all options are specified, it should generate profile with all customizations",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LifecycleAndUtilization, deschedulerv1.EvictPodsWithPVC, deschedulerv1.EvictPodsWithLocalStorage}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					PodLifetime:                     &fiveMinutes,
					ThresholdPriority:               utilptr.To(int32(500)),
					DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold,
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{"kube-system", "openshift-*"},
					},
				}
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := BuildDeschedulingPolicy(
				tt.descheduler,
				protectedNamespaces,
				"",
			)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.expectedErrorMsg != "" && err.Error() != tt.expectedErrorMsg {
					t.Errorf("expected error %q, got %q", tt.expectedErrorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			testutil.CompareWithFixture(t, policy)
		})
	}
}

func TestKubeVirtRelieveAndMigrate(t *testing.T) {
	migrationCooldown := metav1.Duration{Duration: 5 * time.Minute}
	maxMigrationCooldown := metav1.Duration{Duration: 15 * time.Minute}
	migrationHistoryWindow := metav1.Duration{Duration: 30 * time.Minute}

	tests := []struct {
		name             string
		descheduler      *deschedulerv1.KubeDescheduler
		prometheusHost   string
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "When no customizations are provided, it should generate default profile with DevKubeVirtRelieveAndMigrate",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When no customizations are provided, it should generate default profile with KubeVirtRelieveAndMigrate",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.KubeVirtRelieveAndMigrate}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When prometheus host is not provided, it should generate profile without metrics providers",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
			}),
			prometheusHost: "",
		},
		{
			name: "When LowNodeUtilization threshold is Low, it should set thresholds to 10 and 30",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold,
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When LowNodeUtilization threshold is Medium, it should set thresholds to 20 and 50",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: &deschedulerv1.MediumThreshold,
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When LowNodeUtilization threshold is High, it should set thresholds to 40 and 70",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: &deschedulerv1.HighThreshold,
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When DeviationThreshold is Low, it should set thresholds to 10 and 10",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When DeviationThreshold is Medium, it should set thresholds to 20 and 20",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevDeviationThresholds: &deschedulerv1.MediumDeviationThreshold,
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When DeviationThreshold is High, it should set thresholds to 40 and 40",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevDeviationThresholds: &deschedulerv1.HighDeviationThreshold,
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When both DevDeviationThresholds and DevLowNodeUtilizationThresholds are set, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevDeviationThresholds:          &deschedulerv1.LowDeviationThreshold,
					DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold,
				}
			}),
			prometheusHost:   testPrometheusHost,
			expectError:      true,
			expectedErrorMsg: "only one of DevLowNodeUtilizationThresholds and DevDeviationThresholds customizations can be configured simultaneously",
		},
		{
			name: "When ActualUtilizationProfile is CPU combined, it should configure prometheus query",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevActualUtilizationProfile: deschedulerv1.PrometheusCPUCombinedProfile,
					DevDeviationThresholds:      &deschedulerv1.LowDeviationThreshold,
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When included namespaces are specified, it should set namespace exclude filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
					},
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When excluded namespaces are specified, it should set namespace exclude filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When migration cooldown settings are specified, it should configure KubevirtMigrationAware plugin",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevMigrationCooldown:      &migrationCooldown,
					DevMaxMigrationCooldown:   &maxMigrationCooldown,
					DevMigrationHistoryWindow: &migrationHistoryWindow,
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When eviction limits are specified, it should set MaxNoOfPodsToEvict fields",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.EvictionLimits = &deschedulerv1.EvictionLimits{
					Total: utilptr.To(int32(10)),
					Node:  utilptr.To(int32(3)),
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When all options are specified, it should generate profile with all customizations",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevKubeVirtRelieveAndMigrate}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevDeviationThresholds:      &deschedulerv1.LowDeviationThreshold,
					DevActualUtilizationProfile: deschedulerv1.PrometheusCPUCombinedProfile,
					DevMigrationCooldown:        &migrationCooldown,
					DevMaxMigrationCooldown:     &maxMigrationCooldown,
					DevMigrationHistoryWindow:   &migrationHistoryWindow,
					ThresholdPriority:           utilptr.To(int32(500)),
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{"kube-system", "openshift-*"},
					},
				}
				spec.EvictionLimits = &deschedulerv1.EvictionLimits{
					Total: utilptr.To(int32(10)),
					Node:  utilptr.To(int32(3)),
				}
			}),
			prometheusHost: testPrometheusHost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := BuildDeschedulingPolicy(
				tt.descheduler,
				protectedNamespaces,
				tt.prometheusHost,
			)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.expectedErrorMsg != "" && err.Error() != tt.expectedErrorMsg {
					t.Errorf("expected error %q, got %q", tt.expectedErrorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			testutil.CompareWithFixture(t, policy)
		})
	}
}

func TestLongLifecycle(t *testing.T) {
	tests := []struct {
		name             string
		descheduler      *deschedulerv1.KubeDescheduler
		prometheusHost   string
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "When no customizations are provided, it should generate default profile",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
			}),
		},
		{
			name: "When DevPreviewLongLifecycle is used, it should generate the same profile as LongLifecycle",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.DevPreviewLongLifecycle}
			}),
		},
		{
			name: "When included namespaces are specified, it should set namespace include filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
					},
				}
			}),
		},
		{
			name: "When excluded namespaces are specified, it should set namespace exclude filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
		},
		{
			name: "When both included and excluded namespaces are specified, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is forbidden to combine both included and excluded namespaces",
		},
		{
			name: "When evictLocalStoragePods is true, it should allow eviction of pods with local storage",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle, deschedulerv1.EvictPodsWithLocalStorage}
			}),
		},
		{
			name: "When ignorePvcPods is false, it should allow eviction of pods with PVC",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle, deschedulerv1.EvictPodsWithPVC}
			}),
		},
		{
			name: "When both EvictPodsWithPVC and EvictPodsWithLocalStorage are enabled, it should configure DefaultEvictor accordingly",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle, deschedulerv1.EvictPodsWithPVC, deschedulerv1.EvictPodsWithLocalStorage}
			}),
		},
		{
			name: "When threshold priority class name is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
		},
		{
			name: "When threshold priority value is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority: utilptr.To(int32(1000)),
				}
			}),
		},
		{
			name: "When both priority class name and value are set, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority:          utilptr.To(int32(1000)),
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields",
		},
		{
			name: "When LowNodeUtilization threshold is Low, it should set thresholds to 10 and 30",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold,
				}
			}),
		},
		{
			name: "When LowNodeUtilization threshold is Medium, it should set thresholds to 20 and 50",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: &deschedulerv1.MediumThreshold,
				}
			}),
		},
		{
			name: "When LowNodeUtilization threshold is High, it should set thresholds to 40 and 70",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevLowNodeUtilizationThresholds: &deschedulerv1.HighThreshold,
				}
			}),
		},
		{
			name: "When ActualUtilizationProfile is CPU usage, it should configure prometheus query",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevActualUtilizationProfile: deschedulerv1.PrometheusCPUUsageProfile,
				}
			}),
			prometheusHost: testPrometheusHost,
		},
		{
			name: "When eviction limits are specified, it should set MaxNoOfPodsToEvict fields",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle}
				spec.EvictionLimits = &deschedulerv1.EvictionLimits{
					Total: utilptr.To(int32(10)),
					Node:  utilptr.To(int32(3)),
				}
			}),
		},
		{
			name: "When all options are specified, it should generate profile with all customizations",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.LongLifecycle, deschedulerv1.EvictPodsWithPVC, deschedulerv1.EvictPodsWithLocalStorage}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority:               utilptr.To(int32(500)),
					DevLowNodeUtilizationThresholds: &deschedulerv1.LowThreshold,
					DevActualUtilizationProfile:     deschedulerv1.PrometheusCPUUsageProfile,
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{"kube-system", "openshift-*"},
					},
				}
				spec.EvictionLimits = &deschedulerv1.EvictionLimits{
					Total: utilptr.To(int32(10)),
					Node:  utilptr.To(int32(3)),
				}
			}),
			prometheusHost: testPrometheusHost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := BuildDeschedulingPolicy(
				tt.descheduler,
				protectedNamespaces,
				tt.prometheusHost,
			)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.expectedErrorMsg != "" && err.Error() != tt.expectedErrorMsg {
					t.Errorf("expected error %q, got %q", tt.expectedErrorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			testutil.CompareWithFixture(t, policy)
		})
	}
}

func TestTopologyAndDuplicates(t *testing.T) {
	tests := []struct {
		name             string
		descheduler      *deschedulerv1.KubeDescheduler
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "When no customizations are provided, it should generate default profile",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates}
			}),
		},
		{
			name: "When included namespaces are specified, it should set namespace include filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
					},
				}
			}),
		},
		{
			name: "When excluded namespaces are specified, it should set namespace exclude filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
		},
		{
			name: "When both included and excluded namespaces are specified, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is forbidden to combine both included and excluded namespaces",
		},
		{
			name: "When threshold priority class name is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
		},
		{
			name: "When threshold priority value is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority: utilptr.To(int32(1000)),
				}
			}),
		},
		{
			name: "When both priority class name and value are set, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority:          utilptr.To(int32(1000)),
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields",
		},
		{
			name: "When ignorePvcPods is false, it should allow eviction of pods with PVC",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates, deschedulerv1.EvictPodsWithPVC}
			}),
		},
		{
			name: "When evictLocalStoragePods is true, it should allow eviction of pods with local storage",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates, deschedulerv1.EvictPodsWithLocalStorage}
			}),
		},
		{
			name: "When all options are specified, it should generate profile with all customizations",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.TopologyAndDuplicates, deschedulerv1.EvictPodsWithPVC, deschedulerv1.EvictPodsWithLocalStorage}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority: utilptr.To(int32(500)),
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{"ns1", "ns2"},
					},
				}
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := BuildDeschedulingPolicy(
				tt.descheduler,
				protectedNamespaces,
				"",
			)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.expectedErrorMsg != "" && err.Error() != tt.expectedErrorMsg {
					t.Errorf("expected error %q, got %q", tt.expectedErrorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			testutil.CompareWithFixture(t, policy)
		})
	}
}

func TestSoftTopologyAndDuplicates(t *testing.T) {
	tests := []struct {
		name             string
		descheduler      *deschedulerv1.KubeDescheduler
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "When no customizations are provided, it should generate default profile",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates}
			}),
		},
		{
			name: "When included namespaces are specified, it should set namespace include filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
					},
				}
			}),
		},
		{
			name: "When excluded namespaces are specified, it should set namespace exclude filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
		},
		{
			name: "When both included and excluded namespaces are specified, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is forbidden to combine both included and excluded namespaces",
		},
		{
			name: "When threshold priority class name is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
		},
		{
			name: "When threshold priority value is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority: utilptr.To(int32(1000)),
				}
			}),
		},
		{
			name: "When both priority class name and value are set, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority:          utilptr.To(int32(1000)),
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields",
		},
		{
			name: "When ignorePvcPods is false, it should allow eviction of pods with PVC",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates, deschedulerv1.EvictPodsWithPVC}
			}),
		},
		{
			name: "When evictLocalStoragePods is true, it should allow eviction of pods with local storage",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates, deschedulerv1.EvictPodsWithLocalStorage}
			}),
		},
		{
			name: "When all options are specified, it should generate profile with all customizations",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.SoftTopologyAndDuplicates, deschedulerv1.EvictPodsWithPVC, deschedulerv1.EvictPodsWithLocalStorage}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority: utilptr.To(int32(500)),
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{"ns1", "ns2"},
					},
				}
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := BuildDeschedulingPolicy(
				tt.descheduler,
				protectedNamespaces,
				"",
			)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.expectedErrorMsg != "" && err.Error() != tt.expectedErrorMsg {
					t.Errorf("expected error %q, got %q", tt.expectedErrorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			testutil.CompareWithFixture(t, policy)
		})
	}
}

func TestCompactAndScale(t *testing.T) {
	tests := []struct {
		name             string
		descheduler      *deschedulerv1.KubeDescheduler
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "When no customizations are provided, it should generate default profile",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
			}),
		},
		{
			name: "When included namespaces are specified, it should set namespace exclude filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
					},
				}
			}),
		},
		{
			name: "When excluded namespaces are specified, it should set namespace exclude filter",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
		},
		{
			name: "When both included and excluded namespaces are specified, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					Namespaces: deschedulerv1.Namespaces{
						Included: []string{testIncludedNamespace},
						Excluded: []string{testExcludedNamespace},
					},
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is forbidden to combine both included and excluded namespaces",
		},
		{
			name: "When threshold priority class name is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
		},
		{
			name: "When threshold priority value is set, it should configure priority threshold",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority: utilptr.To(int32(1000)),
				}
			}),
		},
		{
			name: "When both priority class name and value are set, it should return error",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority:          utilptr.To(int32(1000)),
					ThresholdPriorityClassName: testPriorityClassName,
				}
			}),
			expectError:      true,
			expectedErrorMsg: "It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields",
		},
		{
			name: "When ignorePvcPods is false, it should allow eviction of pods with PVC",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale, deschedulerv1.EvictPodsWithPVC}
			}),
		},
		{
			name: "When evictLocalStoragePods is true, it should allow eviction of pods with local storage",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale, deschedulerv1.EvictPodsWithLocalStorage}
			}),
		},
		{
			name: "When HighNodeUtilization threshold is Minimal, it should set thresholds to 10",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevHighNodeUtilizationThresholds: &deschedulerv1.CompactMinimalThreshold,
				}
			}),
		},
		{
			name: "When HighNodeUtilization threshold is Modest, it should set thresholds to 20",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevHighNodeUtilizationThresholds: &deschedulerv1.CompactModestThreshold,
				}
			}),
		},
		{
			name: "When HighNodeUtilization threshold is Moderate, it should set thresholds to 30",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					DevHighNodeUtilizationThresholds: &deschedulerv1.CompactModerateThreshold,
				}
			}),
		},
		{
			name: "When all options are specified, it should generate profile with all customizations",
			descheduler: buildKubeDescheduler(func(spec *deschedulerv1.KubeDeschedulerSpec) {
				spec.Profiles = []deschedulerv1.DeschedulerProfile{deschedulerv1.CompactAndScale, deschedulerv1.EvictPodsWithPVC, deschedulerv1.EvictPodsWithLocalStorage}
				spec.ProfileCustomizations = &deschedulerv1.ProfileCustomizations{
					ThresholdPriority:                utilptr.To(int32(500)),
					DevHighNodeUtilizationThresholds: &deschedulerv1.CompactMinimalThreshold,
					Namespaces: deschedulerv1.Namespaces{
						Excluded: []string{"kube-system", "openshift-*"},
					},
				}
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := BuildDeschedulingPolicy(
				tt.descheduler,
				protectedNamespaces,
				"",
			)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.expectedErrorMsg != "" && err.Error() != tt.expectedErrorMsg {
					t.Errorf("expected error %q, got %q", tt.expectedErrorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			testutil.CompareWithFixture(t, policy)
		})
	}
}

func buildKubeDescheduler(apply func(*deschedulerv1.KubeDeschedulerSpec)) *deschedulerv1.KubeDescheduler {
	descheduler := &deschedulerv1.KubeDescheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "openshift-kube-descheduler-operator",
		},
		Spec: deschedulerv1.KubeDeschedulerSpec{
			OperatorSpec: operatorv1.OperatorSpec{
				ManagementState: operatorv1.Managed,
			},
			Mode: deschedulerv1.Predictive,
		},
	}

	if apply != nil {
		apply(&descheduler.Spec)
	}

	return descheduler
}
