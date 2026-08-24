package profiles

import (
	"fmt"
	"slices"
	"strings"

	deschedulerv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	utilptr "k8s.io/utils/ptr"

	deschedulerapi "sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/api/v1alpha2"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/defaultevictor"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/podlifetime"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removeduplicates"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodshavingtoomanyrestarts"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatinginterpodantiaffinity"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatingnodeaffinity"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatingnodetaints"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatingtopologyspreadconstraint"
)

const kubeVirtShedulableLabelSelector = "kubevirt.io/schedulable=true"
const kubevirtMigrationAwarePluginName = "KubevirtMigrationAware"
const defaultKVParallelOutboundMigrationsPerNode = 2
const defaultKVParallelMigrationsPerCluster = 5

func hasKubeVirtRelieveAndMigrateProfile(profiles []deschedulerv1.DeschedulerProfile) bool {
	return slices.Contains(profiles, deschedulerv1.KubeVirtRelieveAndMigrate) || slices.Contains(profiles, deschedulerv1.DevKubeVirtRelieveAndMigrate)
}

func BuildDeschedulingPolicy(descheduler *deschedulerv1.KubeDescheduler, protectedNamespaces []string, prometheusHost string) (*v1alpha2.DeschedulerPolicy, error) {
	// override the included/excluded namespace
	excludedNamespaces := protectedNamespaces
	protectedNamespacesSet := sets.NewString(protectedNamespaces...)
	includedNamespaces := []string{}
	if descheduler.Spec.ProfileCustomizations != nil {
		if len(descheduler.Spec.ProfileCustomizations.Namespaces.Excluded) > 0 && len(descheduler.Spec.ProfileCustomizations.Namespaces.Included) > 0 {
			return nil, fmt.Errorf("It is forbidden to combine both included and excluded namespaces")
		}
		if len(descheduler.Spec.ProfileCustomizations.Namespaces.Included) > 0 {
			for _, ns := range descheduler.Spec.ProfileCustomizations.Namespaces.Included {
				if protectedNamespacesSet.Has(ns) {
					return nil, fmt.Errorf("Protected namespace %v included. It is forbidden to include any of the protected namespaces from %v", ns, protectedNamespaces)
				}
				includedNamespaces = append(includedNamespaces, ns)
			}
			excludedNamespaces = []string{}
		}
		for _, ns := range descheduler.Spec.ProfileCustomizations.Namespaces.Excluded {
			excludedNamespaces = append(excludedNamespaces, ns)
		}
	}

	policy := &v1alpha2.DeschedulerPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DeschedulerPolicy",
			APIVersion: "descheduler/v1alpha2",
		},
		Profiles: []v1alpha2.DeschedulerProfile{},
	}

	if len(prometheusHost) > 0 {
		policy.MetricsProviders = []v1alpha2.MetricsProvider{{
			Source: v1alpha2.PrometheusMetrics,
			Prometheus: &v1alpha2.Prometheus{
				URL: "https://" + prometheusHost,
			}},
		}
	}

	// ignore PVC pods by default
	ignorePVCPods := true
	evictLocalStoragePods := false
	for _, profileName := range descheduler.Spec.Profiles {
		if profileName == deschedulerv1.EvictPodsWithPVC {
			ignorePVCPods = false
			continue
		}
		if profileName == deschedulerv1.EvictPodsWithLocalStorage {
			evictLocalStoragePods = true
			continue
		}
	}

	for _, profileName := range descheduler.Spec.Profiles {
		var profile *v1alpha2.DeschedulerProfile
		var err error
		switch profileName {
		case deschedulerv1.AffinityAndTaints:
			profile, err = affinityAndTaintsProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.TopologyAndDuplicates:
			profile, err = topologyAndDuplicatesProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.SoftTopologyAndDuplicates:
			profile, err = softTopologyAndDuplicatesProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.LifecycleAndUtilization:
			profile, err = lifecycleAndUtilizationProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, protectedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.EvictPodsWithLocalStorage, deschedulerv1.EvictPodsWithPVC:
			continue
		case deschedulerv1.DevPreviewLongLifecycle, deschedulerv1.LongLifecycle:
			profile, err = longLifecycleProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, protectedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.CompactAndScale:
			profile, err = compactAndScaleProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
		case deschedulerv1.KubeVirtRelieveAndMigrate, deschedulerv1.DevKubeVirtRelieveAndMigrate:
			kubeVirtShedulable := kubeVirtShedulableLabelSelector
			policy.NodeSelector = &kubeVirtShedulable
			profile, err = kubeVirtRelieveAndMigrateProfile(descheduler.Spec.ProfileCustomizations, includedNamespaces, excludedNamespaces, protectedNamespaces)
		default:
			err = fmt.Errorf("Profile %q not recognized", profileName)
		}
		if err != nil {
			return nil, err
		}
		policy.Profiles = append(policy.Profiles, *profile)
	}

	setEvictionsLimits(descheduler, policy)

	return policy, nil
}

func affinityAndTaintsProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile := &v1alpha2.DeschedulerProfile{
		Name: string(deschedulerv1.AffinityAndTaints),
		PluginConfigs: []v1alpha2.PluginConfig{
			{
				Name: removepodsviolatinginterpodantiaffinity.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodsviolatinginterpodantiaffinity.RemovePodsViolatingInterPodAntiAffinityArgs{},
				},
			},
			{
				Name: removepodsviolatingnodetaints.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodsviolatingnodetaints.RemovePodsViolatingNodeTaintsArgs{},
				},
			},
			{
				Name: removepodsviolatingnodeaffinity.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodsviolatingnodeaffinity.RemovePodsViolatingNodeAffinityArgs{
						NodeAffinityType: []string{"requiredDuringSchedulingIgnoredDuringExecution"},
					},
				},
			},
			{
				Name: defaultevictor.PluginName,
				Args: runtime.RawExtension{
					Object: &defaultevictor.DefaultEvictorArgs{
						IgnorePvcPods:         ignorePVCPods,
						EvictLocalStoragePods: evictLocalStoragePods,
					},
				},
			},
		},
		Plugins: v1alpha2.Plugins{
			Filter: v1alpha2.PluginSet{
				Enabled: []string{
					defaultevictor.PluginName,
				},
			},
			Deschedule: v1alpha2.PluginSet{
				Enabled: []string{
					removepodsviolatinginterpodantiaffinity.PluginName,
					removepodsviolatingnodetaints.PluginName,
					removepodsviolatingnodeaffinity.PluginName,
				},
			},
		},
	}

	// exclude openshift namespaces from descheduling
	if len(includedNamespaces) > 0 || len(excludedNamespaces) > 0 {
		profile.PluginConfigs[0].Args.Object.(*removepodsviolatinginterpodantiaffinity.RemovePodsViolatingInterPodAntiAffinityArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		profile.PluginConfigs[1].Args.Object.(*removepodsviolatingnodetaints.RemovePodsViolatingNodeTaintsArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		profile.PluginConfigs[2].Args.Object.(*removepodsviolatingnodeaffinity.RemovePodsViolatingNodeAffinityArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
	}

	if profileCustomizations == nil {
		return profile, nil
	}

	if err := defaultEvictorOverrides(profileCustomizations, &profile.PluginConfigs[3]); err != nil {
		return nil, err
	}

	return profile, nil
}

func topologyAndDuplicatesProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile := &v1alpha2.DeschedulerProfile{
		Name: string(deschedulerv1.TopologyAndDuplicates),
		PluginConfigs: []v1alpha2.PluginConfig{
			{
				Name: removepodsviolatingtopologyspreadconstraint.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodsviolatingtopologyspreadconstraint.RemovePodsViolatingTopologySpreadConstraintArgs{
						Constraints: []v1.UnsatisfiableConstraintAction{v1.DoNotSchedule},
					},
				},
			},
			{
				Name: removeduplicates.PluginName,
				Args: runtime.RawExtension{
					Object: &removeduplicates.RemoveDuplicatesArgs{},
				},
			},
			{
				Name: defaultevictor.PluginName,
				Args: runtime.RawExtension{
					Object: &defaultevictor.DefaultEvictorArgs{
						IgnorePvcPods:         ignorePVCPods,
						EvictLocalStoragePods: evictLocalStoragePods,
					},
				},
			},
		},
		Plugins: v1alpha2.Plugins{
			Filter: v1alpha2.PluginSet{
				Enabled: []string{
					defaultevictor.PluginName,
				},
			},
			Balance: v1alpha2.PluginSet{
				Enabled: []string{
					removepodsviolatingtopologyspreadconstraint.PluginName,
					removeduplicates.PluginName,
				},
			},
		},
	}

	// exclude openshift namespaces from descheduling
	if len(includedNamespaces) > 0 || len(excludedNamespaces) > 0 {
		profile.PluginConfigs[0].Args.Object.(*removepodsviolatingtopologyspreadconstraint.RemovePodsViolatingTopologySpreadConstraintArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		profile.PluginConfigs[1].Args.Object.(*removeduplicates.RemoveDuplicatesArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
	}

	if profileCustomizations == nil {
		return profile, nil
	}

	if err := defaultEvictorOverrides(profileCustomizations, &profile.PluginConfigs[2]); err != nil {
		return nil, err
	}

	return profile, nil
}

func softTopologyAndDuplicatesProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile, err := topologyAndDuplicatesProfile(profileCustomizations, includedNamespaces, excludedNamespaces, ignorePVCPods, evictLocalStoragePods)
	if err != nil {
		return profile, err
	}
	profile.Name = string(deschedulerv1.SoftTopologyAndDuplicates)
	profile.PluginConfigs[0].Args.Object.(*removepodsviolatingtopologyspreadconstraint.RemovePodsViolatingTopologySpreadConstraintArgs).Constraints = []v1.UnsatisfiableConstraintAction{v1.DoNotSchedule, v1.ScheduleAnyway}
	return profile, err
}

func utilizationProfileToPrometheusQuery(profile deschedulerv1.ActualUtilizationProfile) (string, error) {
	switch profile {
	case deschedulerv1.PrometheusCPUUsageProfile:
		return "instance:node_cpu:rate:sum", nil
	case deschedulerv1.PrometheusCPUPSIPressureProfile:
		return "rate(node_pressure_cpu_waiting_seconds_total[1m])", nil
	case deschedulerv1.PrometheusCPUPSIPressureByUtilizationProfile:
		return "avg by (instance) ( rate(node_pressure_cpu_waiting_seconds_total[1m])) and (1 - avg by (instance) (rate(node_cpu_seconds_total{mode=\"idle\"}[1m]))) > 0.7 or avg by (instance) ( rate(node_pressure_cpu_waiting_seconds_total[1m])) * 0", nil
	case deschedulerv1.PrometheusMemoryPSIPressureProfile:
		return "rate(node_pressure_memory_waiting_seconds_total[1m])", nil
	case deschedulerv1.PrometheusIOPSIPressureProfile:
		return "rate(node_pressure_io_waiting_seconds_total[1m])", nil
	case deschedulerv1.PrometheusCPUCombinedProfile:
		return "descheduler:combined_utilization_and_pressure:avg1m", nil
	case deschedulerv1.PrometheusCPUMemoryCombinedProfile:
		return "descheduler:node:linear_amplified_ideal_point_positive_distance:k3:avg1m", nil
	default:
		if !strings.HasPrefix(string(profile), "query:") {
			return "", fmt.Errorf("unknown prometheus profile: %v", profile)
		}
		return strings.TrimPrefix(string(profile), "query:"), nil
	}
}

func setExcludedNamespacesForLowNodeUtilizationPlugin(lowNodeUtilizationArgs *nodeutilization.LowNodeUtilizationArgs, includedNamespaces, excludedNamespaces, protectedNamespaces []string) {
	if len(includedNamespaces) > 0 {
		// log a warning if user tries to enable ns inclusion with a profile that activates LowNodeUtilization
		klog.Warning("LowNodeUtilization is enabled, however it does not support namespace inclusion. Namespace inclusion will only be considered by other strategies (like RemovePodsHavingTooManyRestarts and PodLifeTime). Falling back to a list of excluded protected namespaces.")
		if len(excludedNamespaces) == 0 {
			lowNodeUtilizationArgs.EvictableNamespaces = &deschedulerapi.Namespaces{
				Exclude: protectedNamespaces,
			}
		}
	}
	if len(excludedNamespaces) > 0 {
		lowNodeUtilizationArgs.EvictableNamespaces = &deschedulerapi.Namespaces{
			Exclude: excludedNamespaces,
		}
	}
}

func getLowNodeUtilizationThresholds(profileCustomizations *deschedulerv1.ProfileCustomizations, ignoreDynamic bool) (deschedulerapi.Percentage, deschedulerapi.Percentage, error) {
	lowThreshold := deschedulerapi.Percentage(20)
	highThreshold := deschedulerapi.Percentage(50)

	if profileCustomizations != nil {
		if !ignoreDynamic && profileCustomizations.DevLowNodeUtilizationThresholds != nil && profileCustomizations.DevDeviationThresholds != nil {
			return 0, 0, fmt.Errorf("only one of DevLowNodeUtilizationThresholds and DevDeviationThresholds customizations can be configured simultaneously")
		}
		if profileCustomizations.DevLowNodeUtilizationThresholds != nil {
			switch *profileCustomizations.DevLowNodeUtilizationThresholds {
			case deschedulerv1.LowThreshold:
				lowThreshold = 10
				highThreshold = 30
			case deschedulerv1.MediumThreshold, "":
				lowThreshold = 20
				highThreshold = 50
			case deschedulerv1.HighThreshold:
				lowThreshold = 40
				highThreshold = 70
			default:
				return 0, 0, fmt.Errorf("unknown Descheduler LowNodeUtilization threshold %v, only 'Low', 'Medium' and 'High' are supported", *profileCustomizations.DevLowNodeUtilizationThresholds)
			}
		}
		if !ignoreDynamic && profileCustomizations.DevDeviationThresholds != nil {
			switch *profileCustomizations.DevDeviationThresholds {
			case deschedulerv1.LowDeviationThreshold:
				lowThreshold = 10
				highThreshold = 10
			case deschedulerv1.MediumDeviationThreshold:
				lowThreshold = 20
				highThreshold = 20
			case deschedulerv1.HighDeviationThreshold:
				lowThreshold = 30
				highThreshold = 30
			case deschedulerv1.AsymmetricLowDeviationThreshold:
				lowThreshold = 0
				highThreshold = 10
			case deschedulerv1.AsymmetricMediumDeviationThreshold:
				lowThreshold = 0
				highThreshold = 20
			case deschedulerv1.AsymmetricHighDeviationThreshold:
				lowThreshold = 0
				highThreshold = 30
			default:
				return 0, 0, fmt.Errorf("unknown Descheduler DeviationThresholds threshold %v, only 'Low', 'Medium' and 'High' are supported", *profileCustomizations.DevDeviationThresholds)
			}
		}
	}

	return lowThreshold, highThreshold, nil
}

func getKubeVirtRelieveAndMigrateThresholds(profileCustomizations *deschedulerv1.ProfileCustomizations, useDeviationThresholds bool) (deschedulerapi.Percentage, deschedulerapi.Percentage, error) {
	if profileCustomizations != nil && (profileCustomizations.DevLowNodeUtilizationThresholds != nil || profileCustomizations.DevDeviationThresholds != nil) {
		return getLowNodeUtilizationThresholds(profileCustomizations, false)
	}

	const defaultAssymetricDeviatonThresholdLow = 0
	const defaultAssymetricDeviatonThresholdHigh = 10
	const defaultThresholdLow = 20
	const defaultThresholdHigh = 50

	lowThreshold := deschedulerapi.Percentage(defaultAssymetricDeviatonThresholdLow)
	highThreshold := deschedulerapi.Percentage(defaultAssymetricDeviatonThresholdHigh)
	if !useDeviationThresholds {
		lowThreshold = deschedulerapi.Percentage(defaultThresholdLow)
		highThreshold = deschedulerapi.Percentage(defaultThresholdHigh)
	}

	return lowThreshold, highThreshold, nil
}

func lifecycleAndUtilizationProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces, protectedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile := &v1alpha2.DeschedulerProfile{
		Name: string(deschedulerv1.LifecycleAndUtilization),
		PluginConfigs: []v1alpha2.PluginConfig{
			{
				Name: podlifetime.PluginName,
				Args: runtime.RawExtension{
					Object: &podlifetime.PodLifeTimeArgs{
						MaxPodLifeTimeSeconds: utilptr.To[uint](86400), // 24 hours
					},
				},
			},
			{
				Name: removepodshavingtoomanyrestarts.PluginName,
				Args: runtime.RawExtension{
					Object: &removepodshavingtoomanyrestarts.RemovePodsHavingTooManyRestartsArgs{
						PodRestartThreshold:     100,
						IncludingInitContainers: true,
					},
				},
			},
			{
				Name: nodeutilization.LowNodeUtilizationPluginName,
				Args: runtime.RawExtension{
					Object: &nodeutilization.LowNodeUtilizationArgs{
						Thresholds:       deschedulerapi.ResourceThresholds{},
						TargetThresholds: deschedulerapi.ResourceThresholds{},
					},
				},
			},
			{
				Name: defaultevictor.PluginName,
				Args: runtime.RawExtension{
					Object: &defaultevictor.DefaultEvictorArgs{
						IgnorePvcPods:         ignorePVCPods,
						EvictLocalStoragePods: evictLocalStoragePods,
					},
				},
			},
		},
		Plugins: v1alpha2.Plugins{
			Filter: v1alpha2.PluginSet{
				Enabled: []string{
					defaultevictor.PluginName,
				},
			},
			Deschedule: v1alpha2.PluginSet{
				Enabled: []string{
					podlifetime.PluginName,
					removepodshavingtoomanyrestarts.PluginName,
				},
			},
			Balance: v1alpha2.PluginSet{
				Enabled: []string{
					nodeutilization.LowNodeUtilizationPluginName,
				},
			},
		},
	}

	// exclude openshift namespaces from descheduling
	if len(includedNamespaces) > 0 || len(excludedNamespaces) > 0 {
		profile.PluginConfigs[0].Args.Object.(*podlifetime.PodLifeTimeArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		profile.PluginConfigs[1].Args.Object.(*removepodshavingtoomanyrestarts.RemovePodsHavingTooManyRestartsArgs).Namespaces = &deschedulerapi.Namespaces{
			Include: includedNamespaces,
			Exclude: excludedNamespaces,
		}
		setExcludedNamespacesForLowNodeUtilizationPlugin(profile.PluginConfigs[2].Args.Object.(*nodeutilization.LowNodeUtilizationArgs), includedNamespaces, excludedNamespaces, protectedNamespaces)
	}

	lowThreshold, highThreshold, err := getLowNodeUtilizationThresholds(profileCustomizations, true)
	if err != nil {
		return nil, err
	}

	resourceNames := []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory, v1.ResourcePods}
	args := profile.PluginConfigs[2].Args.Object.(*nodeutilization.LowNodeUtilizationArgs)
	if profileCustomizations != nil {
		if profileCustomizations.PodLifetime != nil {
			profile.PluginConfigs[0].Args.Object.(*podlifetime.PodLifeTimeArgs).MaxPodLifeTimeSeconds = utilptr.To[uint](uint(profileCustomizations.PodLifetime.Seconds()))
		}

		if profileCustomizations.DevActualUtilizationProfile != "" {
			query, err := utilizationProfileToPrometheusQuery(profileCustomizations.DevActualUtilizationProfile)
			if err != nil {
				return nil, err
			}
			args.MetricsUtilization = &nodeutilization.MetricsUtilization{
				Source: deschedulerapi.MetricsSource(v1alpha2.PrometheusMetrics),
				Prometheus: &nodeutilization.Prometheus{
					Query: query,
				},
			}
			resourceNames = []v1.ResourceName{nodeutilization.MetricResource}
		}

		if err := defaultEvictorOverrides(profileCustomizations, &profile.PluginConfigs[3]); err != nil {
			return nil, err
		}
	}

	for _, resourceName := range resourceNames {
		args.Thresholds[resourceName] = lowThreshold
		args.TargetThresholds[resourceName] = highThreshold
	}

	return profile, nil
}

func kubeVirtRelieveAndMigrateProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces, protectedNamespaces []string) (*v1alpha2.DeschedulerProfile, error) {
	profile := &v1alpha2.DeschedulerProfile{
		Name: string(deschedulerv1.KubeVirtRelieveAndMigrate),
		PluginConfigs: []v1alpha2.PluginConfig{
			{
				Name: nodeutilization.LowNodeUtilizationPluginName,
				Args: runtime.RawExtension{
					Object: &nodeutilization.LowNodeUtilizationArgs{
						Thresholds:       deschedulerapi.ResourceThresholds{},
						TargetThresholds: deschedulerapi.ResourceThresholds{},
					},
				},
			},
			{
				Name: defaultevictor.PluginName,
				Args: runtime.RawExtension{
					Object: &defaultevictor.DefaultEvictorArgs{
						IgnorePvcPods:         false, // evict pvc pods by default
						EvictLocalStoragePods: true,  // evict pods with local storage by default
						NoEvictionPolicy:      defaultevictor.MandatoryNoEvictionPolicy,
					},
				},
			},
		},
		Plugins: v1alpha2.Plugins{
			Filter: v1alpha2.PluginSet{
				Enabled: []string{
					defaultevictor.PluginName,
					kubevirtMigrationAwarePluginName,
				},
			},
			PreEvictionFilter: v1alpha2.PluginSet{
				Enabled: []string{
					kubevirtMigrationAwarePluginName,
				},
			},
			Balance: v1alpha2.PluginSet{
				Enabled: []string{
					nodeutilization.LowNodeUtilizationPluginName,
				},
			},
		},
	}

	// exclude openshift namespaces from descheduling
	if len(includedNamespaces) > 0 || len(excludedNamespaces) > 0 {
		setExcludedNamespacesForLowNodeUtilizationPlugin(profile.PluginConfigs[0].Args.Object.(*nodeutilization.LowNodeUtilizationArgs), includedNamespaces, excludedNamespaces, protectedNamespaces)
	}

	args := profile.PluginConfigs[0].Args.Object.(*nodeutilization.LowNodeUtilizationArgs)

	// profile defaults
	const defaultActualUtilizationProfile = deschedulerv1.PrometheusCPUMemoryCombinedProfile
	args.UseDeviationThresholds = true
	args.EvictionLimits = &deschedulerapi.EvictionLimits{
		Node: utilptr.To[uint](uint(defaultKVParallelOutboundMigrationsPerNode)),
	}
	query, err := utilizationProfileToPrometheusQuery(defaultActualUtilizationProfile)
	if err != nil {
		return nil, err
	}
	args.MetricsUtilization = &nodeutilization.MetricsUtilization{
		Source: deschedulerapi.MetricsSource(v1alpha2.PrometheusMetrics),
		Prometheus: &nodeutilization.Prometheus{
			Query: query,
		},
	}
	resourceNames := []v1.ResourceName{nodeutilization.MetricResource}

	if profileCustomizations != nil {
		// enable deviation
		if profileCustomizations.DevDeviationThresholds != nil && profileCustomizations.DevLowNodeUtilizationThresholds != nil {
			return nil, fmt.Errorf("only one of DevLowNodeUtilizationThresholds and DevDeviationThresholds customizations can be configured simultaneously")
		}
		if profileCustomizations.DevDeviationThresholds != nil {
			args.UseDeviationThresholds = *profileCustomizations.DevDeviationThresholds != ""
		} else if profileCustomizations.DevLowNodeUtilizationThresholds != nil {
			args.UseDeviationThresholds = *profileCustomizations.DevLowNodeUtilizationThresholds == ""
		}

		if profileCustomizations.DevActualUtilizationProfile != "" {
			query, err := utilizationProfileToPrometheusQuery(profileCustomizations.DevActualUtilizationProfile)
			if err != nil {
				return nil, err
			}
			args.MetricsUtilization = &nodeutilization.MetricsUtilization{
				Source: deschedulerapi.MetricsSource(v1alpha2.PrometheusMetrics),
				Prometheus: &nodeutilization.Prometheus{
					Query: query,
				},
			}
			resourceNames = []v1.ResourceName{nodeutilization.MetricResource}
		}

		if err := defaultEvictorOverrides(profileCustomizations, &profile.PluginConfigs[1]); err != nil {
			return nil, err
		}
	}

	{
		var parts []string
		if profileCustomizations != nil {
			if profileCustomizations.DevMigrationCooldown != nil {
				parts = append(parts, fmt.Sprintf(`"migrationCooldown":%q`, profileCustomizations.DevMigrationCooldown.Duration.String()))
			}
			if profileCustomizations.DevMaxMigrationCooldown != nil {
				parts = append(parts, fmt.Sprintf(`"maxMigrationCooldown":%q`, profileCustomizations.DevMaxMigrationCooldown.Duration.String()))
			}
			if profileCustomizations.DevMigrationHistoryWindow != nil {
				parts = append(parts, fmt.Sprintf(`"migrationHistoryWindow":%q`, profileCustomizations.DevMigrationHistoryWindow.Duration.String()))
			}
		}
		profile.PluginConfigs = append(profile.PluginConfigs, v1alpha2.PluginConfig{
			Name: kubevirtMigrationAwarePluginName,
			Args: runtime.RawExtension{Raw: []byte("{" + strings.Join(parts, ",") + "}")},
		})
	}

	lowThreshold, highThreshold, err := getKubeVirtRelieveAndMigrateThresholds(profileCustomizations, args.UseDeviationThresholds)
	if err != nil {
		return nil, err
	}
	for _, resourceName := range resourceNames {
		args.Thresholds[resourceName] = lowThreshold
		args.TargetThresholds[resourceName] = highThreshold
	}

	return profile, nil
}

func longLifecycleProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces, protectedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile, err := lifecycleAndUtilizationProfile(profileCustomizations, includedNamespaces, excludedNamespaces, protectedNamespaces, ignorePVCPods, evictLocalStoragePods)
	if err != nil {
		return profile, err
	}
	profile.PluginConfigs = profile.PluginConfigs[1:]
	profile.Plugins.Deschedule.Enabled = profile.Plugins.Deschedule.Enabled[1:]
	profile.Name = string(deschedulerv1.LongLifecycle)
	return profile, err
}

func compactAndScaleProfile(profileCustomizations *deschedulerv1.ProfileCustomizations, includedNamespaces, excludedNamespaces []string, ignorePVCPods, evictLocalStoragePods bool) (*v1alpha2.DeschedulerProfile, error) {
	profile := &v1alpha2.DeschedulerProfile{
		Name: string(deschedulerv1.CompactAndScale),
		PluginConfigs: []v1alpha2.PluginConfig{
			{
				Name: nodeutilization.HighNodeUtilizationPluginName,
				Args: runtime.RawExtension{
					Object: &nodeutilization.HighNodeUtilizationArgs{
						Thresholds: deschedulerapi.ResourceThresholds{
							v1.ResourceCPU:    20,
							v1.ResourceMemory: 20,
							v1.ResourcePods:   20,
						},
					},
				},
			},
			{
				Name: defaultevictor.PluginName,
				Args: runtime.RawExtension{
					Object: &defaultevictor.DefaultEvictorArgs{
						IgnorePvcPods:         ignorePVCPods,
						EvictLocalStoragePods: evictLocalStoragePods,
					},
				},
			},
		},
		Plugins: v1alpha2.Plugins{
			Filter: v1alpha2.PluginSet{
				Enabled: []string{
					defaultevictor.PluginName,
				},
			},
			Balance: v1alpha2.PluginSet{
				Enabled: []string{
					nodeutilization.HighNodeUtilizationPluginName,
				},
			},
		},
	}

	// exclude openshift namespaces from descheduling
	if len(includedNamespaces) > 0 || len(excludedNamespaces) > 0 {
		if len(includedNamespaces) > 0 {
			// log a warning if user tries to enable ns inclusion with a profile that activates LowNodeUtilization
			klog.Warning("HighNodeUtilization is enabled, however it does not support namespace inclusion. Namespace inclusion will only be considered by other strategies (like RemovePodsHavingTooManyRestarts and PodLifeTime)")
		}
		if len(excludedNamespaces) > 0 {
			profile.PluginConfigs[0].Args.Object.(*nodeutilization.HighNodeUtilizationArgs).EvictableNamespaces = &deschedulerapi.Namespaces{
				Exclude: excludedNamespaces,
			}
		}
	}

	if profileCustomizations == nil {
		return profile, nil
	}

	if profileCustomizations.DevHighNodeUtilizationThresholds != nil {
		args := profile.PluginConfigs[0].Args.Object.(*nodeutilization.HighNodeUtilizationArgs)
		switch *profileCustomizations.DevHighNodeUtilizationThresholds {
		case deschedulerv1.CompactMinimalThreshold:
			args.Thresholds[v1.ResourceCPU] = 10
			args.Thresholds[v1.ResourceMemory] = 10
			args.Thresholds[v1.ResourcePods] = 10
		case deschedulerv1.CompactModestThreshold, "":
			args.Thresholds[v1.ResourceCPU] = 20
			args.Thresholds[v1.ResourceMemory] = 20
			args.Thresholds[v1.ResourcePods] = 20
		case deschedulerv1.CompactModerateThreshold:
			args.Thresholds[v1.ResourceCPU] = 30
			args.Thresholds[v1.ResourceMemory] = 30
			args.Thresholds[v1.ResourcePods] = 30
		default:
			return nil, fmt.Errorf("unknown Descheduler HighNodeUtilization threshold %v, only 'Minimal', 'Modest' and 'Moderate' are supported", *profileCustomizations.DevHighNodeUtilizationThresholds)
		}
	}

	if err := defaultEvictorOverrides(profileCustomizations, &profile.PluginConfigs[1]); err != nil {
		return nil, err
	}

	return profile, nil
}

func defaultEvictorOverrides(profileCustomizations *deschedulerv1.ProfileCustomizations, pluginConfig *v1alpha2.PluginConfig) error {
	// set priority class threshold if customized
	if profileCustomizations.ThresholdPriority != nil && profileCustomizations.ThresholdPriorityClassName != "" {
		return fmt.Errorf("It is invalid to set both .spec.profileCustomizations.thresholdPriority and .spec.profileCustomizations.ThresholdPriorityClassName fields")
	}

	if profileCustomizations.ThresholdPriority != nil || profileCustomizations.ThresholdPriorityClassName != "" {
		pluginConfig.Args.Object.(*defaultevictor.DefaultEvictorArgs).PriorityThreshold = &deschedulerapi.PriorityThreshold{
			Value: profileCustomizations.ThresholdPriority,
			Name:  profileCustomizations.ThresholdPriorityClassName,
		}
	}

	return nil
}

func setEvictionsLimits(descheduler *deschedulerv1.KubeDescheduler, policy *v1alpha2.DeschedulerPolicy) {
	if descheduler == nil || policy == nil {
		return
	}

	if hasKubeVirtRelieveAndMigrateProfile(descheduler.Spec.Profiles) {
		policy.MaxNoOfPodsToEvictTotal = utilptr.To[uint](uint(defaultKVParallelMigrationsPerCluster))
		policy.MaxNoOfPodsToEvictPerNode = utilptr.To[uint](uint(defaultKVParallelOutboundMigrationsPerNode))
	}

	if descheduler.Spec.EvictionLimits != nil {
		if descheduler.Spec.EvictionLimits.Total != nil {
			policy.MaxNoOfPodsToEvictTotal = utilptr.To[uint](uint(*descheduler.Spec.EvictionLimits.Total))
		}
		if descheduler.Spec.EvictionLimits.Node != nil {
			policy.MaxNoOfPodsToEvictPerNode = utilptr.To[uint](uint(*descheduler.Spec.EvictionLimits.Node))
			for i := range policy.Profiles {
				for j := range policy.Profiles[i].PluginConfigs {
					if policy.Profiles[i].PluginConfigs[j].Name == nodeutilization.LowNodeUtilizationPluginName {
						args := policy.Profiles[i].PluginConfigs[j].Args.Object.(*nodeutilization.LowNodeUtilizationArgs)
						if args.EvictionLimits == nil {
							args.EvictionLimits = &deschedulerapi.EvictionLimits{}
						}
						args.EvictionLimits.Node = utilptr.To[uint](uint(*descheduler.Spec.EvictionLimits.Node))
					}
				}
			}
		}
	}
}
