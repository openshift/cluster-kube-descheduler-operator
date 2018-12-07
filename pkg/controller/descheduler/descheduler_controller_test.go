package descheduler

import (
	"reflect"
	"testing"

	deschedulerv1alpha1 "github.com/openshift/descheduler-operator/pkg/apis/descheduler/v1alpha1"
)

func TestValidateStrategies(t *testing.T) {
	tests := []struct {
		description   string
		strategies    []string
		errorExpected bool
	}{
		{
			description:   "Empty strategy",
			strategies:    []string{},
			errorExpected: true,
		},
		{
			description:   "One working strategy",
			strategies:    []string{"duplicates"},
			errorExpected: false,
		},
		{
			description:   "Valid strategies strategy",
			strategies:    validStrategies,
			errorExpected: false,
		},
		{
			description:   "More than allowed strategies",
			strategies:    append(validStrategies, "test-strategy"),
			errorExpected: true,
		},
	}
	for _, test := range tests {
		var actualValidation bool
		if err := validateStrategies(test.strategies); err != nil {
			actualValidation = true
		}
		if test.errorExpected != actualValidation {
			t.Fatalf("Expected %v for %v, but got %v", test.errorExpected, test.description, actualValidation)
		}
	}
}

func TestIdentifyInvalidStrategies(t *testing.T) {
	tests := []struct {
		description               string
		strategies                []string
		invalidStrategiesExpected []string
	}{
		{
			description:               "invalid strategies",
			strategies:                []string{"test-strategy", "duplicates", "non-duplicates"},
			invalidStrategiesExpected: []string{"test-strategy", "non-duplicates"},
		},
		{
			description:               "valid strategies",
			strategies:                validStrategies,
			invalidStrategiesExpected: []string{},
		},
	}
	for _, test := range tests {
		actualInvalidStrategies := identifyInvalidStrategies(test.strategies)
		if !reflect.DeepEqual(test.invalidStrategiesExpected, actualInvalidStrategies) {
			t.Fatalf("Expected %v as invalid strategies but got %v for %v", test.invalidStrategiesExpected, actualInvalidStrategies, test.description)
		}
	}
}

func TestGeneratePolicyConfigMapString(t *testing.T) {
	tests := []struct {
		description    string
		strategies     []string
		stringExpected string
	}{
		{
			description:    "invalid strategies",
			strategies:     []string{"duplicates", "non-duplicates"},
			stringExpected: "",
		},
		{
			description: "valid strategies",
			strategies:  validStrategies,
			stringExpected: "  \"RemoveDuplicates\":\n     enabled: true\n" + "  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true\n" +
				"  \"LowNodeUtilization\":\n     enabled: true\n     params:\n" + "       nodeResourceUtilizationThresholds:\n" + "  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution",
		},
		{
			description: "valid strategies with order changed",
			strategies:  []string{"duplicates", "lownodeutilization", "nodeaffinity", "interpodantiaffinity"},
			stringExpected: "  \"RemoveDuplicates\":\n     enabled: true\n" +
				"  \"LowNodeUtilization\":\n     enabled: true\n     params:\n" + "       nodeResourceUtilizationThresholds:\n" +
				"  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution\n" +
				"  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true",
		},
		{
			description: "partial valid strategies",
			strategies:  []string{"duplicates", "nodeaffinity", "interpodantiaffinity"},
			stringExpected: "  \"RemoveDuplicates\":\n     enabled: true\n" +
				"  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution\n" +
				"  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true",
		},
		{
			description: "valid strategies with params",
			strategies:  []string{"duplicates", "lownodeutilization", "nodeaffinity", "interpodantiaffinity"},
			stringExpected: "  \"RemoveDuplicates\":\n     enabled: true\n" +
				"  \"LowNodeUtilization\":\n     enabled: true\n     params:\n" + "       nodeResourceUtilizationThresholds:\n" + "         thresholds:\n" + "           cpu: " + "20" + "\n" +
				"         targetThresholds:\n" + "           cpu: " + "40" + "\n" + "         numberOfNodes: 3\n" + "  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution\n" +
				"  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true",
		},
	}
	for _, test := range tests {
		setParams := false
		if test.description == "valid strategies with params" {
			setParams = true
		}
		actualInvalidStrategies := generateConfigMapString(buildDeschedulerStrategies(test.strategies, setParams))
		if !reflect.DeepEqual(test.stringExpected, actualInvalidStrategies) {
			t.Fatalf("Expected \n%v as invalid strategies but got \n%v for %v", test.stringExpected, actualInvalidStrategies, test.description)
		}
	}
}

func buildDeschedulerStrategies(strategyNames []string, setParams bool) []deschedulerv1alpha1.Strategy {
	strategies := make([]deschedulerv1alpha1.Strategy, 0)
	for _, strategyName := range strategyNames {

		if setParams && strategyName == "lownodeutilization" {
			strategies = append(strategies, deschedulerv1alpha1.Strategy{strategyName, []deschedulerv1alpha1.Param{{Name: "cputhreshold", Value: "20"}, {Name: "cputargetthreshold", Value: "40"}, {Name: "nodes", Value: "3"}}})
		} else {
			strategies = append(strategies, deschedulerv1alpha1.Strategy{strategyName, nil})
		}
	}
	return strategies
}

func TestCheckIfPropertyChanges(t *testing.T) {
	allStrategyConfigMapString := map[string]string{"policy.yaml": "apiVersion: \"descheduler/v1alpha1\"\nkind: \"DeschedulerPolicy\"\nstrategies:\n" + "  \"RemoveDuplicates\":\n     enabled: true\n" + "  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true\n" +
		"  \"LowNodeUtilization\":\n     enabled: true\n     params:\n" + "       nodeResourceUtilizationThresholds:\n" + "  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution"}
	tests := []struct {
		description     string
		strategies      []deschedulerv1alpha1.Strategy
		configMapString map[string]string
		stringExists    bool
	}{
		{
			description:     "valid strategies",
			strategies:      buildDeschedulerStrategies(validStrategies, false),
			configMapString: allStrategyConfigMapString,
			stringExists:    true,
		},
		{
			description:     "invalid strategies",
			strategies:      buildDeschedulerStrategies(append(validStrategies, "non-valid-strategy"), false),
			configMapString: allStrategyConfigMapString,
			stringExists:    false,
		},
	}
	for _, test := range tests {
		if CheckIfPropertyChanges(test.strategies, test.configMapString) != test.stringExists {
			t.Fatalf("Strategy mismatch for %v", test.description)
		}
	}
}
