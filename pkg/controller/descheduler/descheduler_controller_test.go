package descheduler

import (
	"reflect"
	"testing"
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
			description:    "valid strategies",
			strategies:     []string{"duplicates", "non-duplicates"},
			stringExpected: "  \"RemoveDuplicates\":\n     enabled: true\n",
		},
		{
			description: "valid strategies",
			strategies:  validStrategies,
			stringExpected: "  \"RemoveDuplicates\":\n     enabled: true\n" + "  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true\n" +
				"  \"LowNodeUtilization\":\n     enabled: true\n" + "  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution\n",
		},
		{
			description: "valid strategies with order changed",
			strategies:  []string{"duplicates", "lownodeutilization", "nodeaffinity", "interpodantiaffinity"},
			stringExpected: "  \"RemoveDuplicates\":\n     enabled: true\n" +
				"  \"LowNodeUtilization\":\n     enabled: true\n" +
				"  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution\n" +
				"  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true\n",
		},
		{
			description: "valid strategies with order changed",
			strategies:  []string{"duplicates", "nodeaffinity", "interpodantiaffinity"},
			stringExpected: "  \"RemoveDuplicates\":\n     enabled: true\n" +
				"  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution\n" +
				"  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true\n",
		},
	}
	for _, test := range tests {
		actualInvalidStrategies := generateConfigMapString(test.strategies)
		if !reflect.DeepEqual(test.stringExpected, actualInvalidStrategies) {
			t.Fatalf("Expected %v as invalid strategies but got %v for %v", test.stringExpected, actualInvalidStrategies, test.description)
		}
	}
}

func TestCheckIfStrategyExistsInConfigMap(t *testing.T) {
	allStrategyConfigMapString := map[string]string{"policy.yaml": "  \"RemoveDuplicates\":\n     enabled: true\n" + "  \"RemovePodsViolatingInterPodAntiAffinity\":\n     enabled: true\n" +
		"  \"LowNodeUtilization\":\n     enabled: true\n" + "  \"RemovePodsViolatingNodeAffinity\":\n     enabled: true\n     params:\n       nodeAffinityType:\n       - requiredDuringSchedulingIgnoredDuringExecution\n"}
	tests := []struct {
		description     string
		strategies      []string
		configMapString map[string]string
		stringExists    bool
	}{
		{
			description:     "valid strategies",
			strategies:      validStrategies,
			configMapString: allStrategyConfigMapString,
			stringExists:    true,
		},
		{
			description:     "invalid strategies",
			strategies:      append(validStrategies, "non-valid-strategy"),
			configMapString: allStrategyConfigMapString,
			stringExists:    false,
		},
	}
	for _, test := range tests {
		if CheckIfStrategyExistsInConfigMap(test.strategies, test.configMapString) != test.stringExists {
			t.Fatalf("Strategy mismatch for %v", test.description)
		}
	}
}
