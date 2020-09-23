package operator

import (
	"strings"
	"testing"

	deschedulerv1beta1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1beta1"
)

func TestValidateStrategies(t *testing.T) {
	tests := []struct {
		name        string
		strategies  []deschedulerv1beta1.Strategy
		expectedErr string
	}{
		{
			name: "valid strategies",
			strategies: []deschedulerv1beta1.Strategy{
				{Name: "RemoveDuplicates"},
				{Name: "RemovePodsViolatingInterPodAntiAffinity"},
				{Name: "LowNodeUtilization"},
				{Name: "RemovePodsViolatingNodeAffinity"},
				{Name: "RemovePodsViolatingNodeTaints"},
			},
		},
		{
			name: "invalid strategies",
			strategies: []deschedulerv1beta1.Strategy{
				{Name: "foo"},
			},
			expectedErr: "invalid strategies",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateStrategies(test.strategies)
			switch {
			case err == nil && len(test.expectedErr) == 0:
			case err == nil && len(test.expectedErr) != 0:
				t.Errorf("expected error %+v, got none", test.expectedErr)
			case err != nil && len(test.expectedErr) == 0:
				t.Errorf("unexpected error %+v", err)
			case err != nil && len(test.expectedErr) != 0 && !strings.Contains(err.Error(), test.expectedErr):
				t.Errorf("expected error %+v, got %+v", test.expectedErr, err)
			}
		})
	}
}

func TestGenerateConfigMapString(t *testing.T) {
	tests := []struct {
		requestedStrategies []deschedulerv1beta1.Strategy
		expectedErr         string
	}{
		{
			requestedStrategies: []deschedulerv1beta1.Strategy{
				{
					Name: "LowNodeUtilization",
					Params: []deschedulerv1beta1.Param{
						{
							Name:  "thresholdPriorityClassName",
							Value: "priorityclass1",
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		_, err := generateConfigMapString(test.requestedStrategies)
		switch {
		case err == nil && len(test.expectedErr) == 0:
		case err == nil && len(test.expectedErr) != 0:
			t.Errorf("expected error %+v, got none", test.expectedErr)
		case err != nil && len(test.expectedErr) == 0:
			t.Errorf("unexpected error %+v", err)
		case err != nil && len(test.expectedErr) != 0 && !strings.Contains(err.Error(), test.expectedErr):
			t.Errorf("expected error %+v, got %+v", test.expectedErr, err)
		}
	}
}
