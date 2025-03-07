package softtainter

import (
	"context"
	"errors"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"

	"sigs.k8s.io/descheduler/test"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAddOrUpdateSoftTaint(t *testing.T) {
	const testKey = "testTaintKey"
	const testValue = "testTaintValue"
	const otherValue = "testOtherValue"
	const otherKey1 = "testOtherKey1"
	const otherKey2 = "testOtherKey2"
	ctx := context.Background()
	node := test.BuildTestNode("node1", 1000, 2000, 9, nil)
	node.Labels = map[string]string{"type": "compute"}

	scheme := runtime.NewScheme()
	err := v1.AddToScheme(scheme)
	if err != nil {
		t.Errorf("Failed building scheme for the fake client: %v", err)
	}

	tests := []struct {
		description     string
		nodeTaints      []v1.Taint
		expectedTaints  []v1.Taint
		expectedUpdated bool
		expectedErr     error
	}{
		{
			description: "no taints - empty taints",
			nodeTaints:  []v1.Taint{},
			expectedTaints: []v1.Taint{
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedUpdated: true,
			expectedErr:     nil,
		},
		{
			description: "no taints - nil",
			nodeTaints:  nil,
			expectedTaints: []v1.Taint{
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedUpdated: true,
			expectedErr:     nil,
		},
		{
			description: "only the soft taint, same value",
			nodeTaints: []v1.Taint{
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedTaints: []v1.Taint{
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedUpdated: false,
			expectedErr:     nil,
		},
		{
			description: "only the soft taint, different value",
			nodeTaints: []v1.Taint{
				{Key: testKey, Value: otherValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedTaints: []v1.Taint{
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedUpdated: true,
			expectedErr:     nil,
		},
		{
			description: "only the soft taint, different effect",
			nodeTaints: []v1.Taint{
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectNoSchedule},
			},
			expectedTaints: []v1.Taint{
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedUpdated: true,
			expectedErr:     nil,
		},
		{
			description: "only a different taint",
			nodeTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
			},
			expectedTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedUpdated: true,
			expectedErr:     nil,
		},
		{
			description: "two other different taints",
			nodeTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoSchedule},
			},
			expectedTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoSchedule},
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedUpdated: true,
			expectedErr:     nil,
		},
		{
			description: "two other taints + update (begin)",
			nodeTaints: []v1.Taint{
				{Key: testKey, Value: otherValue, Effect: v1.TaintEffectPreferNoSchedule},
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedTaints: []v1.Taint{
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedUpdated: true,
			expectedErr:     nil,
		},
		{
			description: "two other taints + update (middle)",
			nodeTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: testKey, Value: otherValue, Effect: v1.TaintEffectPreferNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedUpdated: true,
			expectedErr:     nil,
		},
		{
			description: "two other taints + update (end)",
			nodeTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
				{Key: testKey, Value: otherValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
				{Key: testKey, Value: testValue, Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedUpdated: true,
			expectedErr:     nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			node.Spec.Taints = tc.nodeTaints

			cl := fake.NewClientBuilder().WithObjects(node).WithScheme(scheme).Build()

			removed, err := AddOrUpdateSoftTaint(ctx, cl, node, testKey, testValue)
			if removed != tc.expectedUpdated {
				t.Errorf("Test %#v failed, unexepcted updated", tc.description)
			}
			if !errors.Is(err, tc.expectedErr) {
				t.Errorf("Test %#v failed, unexpected error: %v", tc.description, err)
			}
			tNode := &v1.Node{}
			err = cl.Get(ctx, client.ObjectKeyFromObject(node), tNode)
			if err != nil {
				t.Errorf("Test %#v failed, test case error: %v", tc.description, err)
			}
			if !reflect.DeepEqual(tNode.Spec.Taints, tc.expectedTaints) {
				t.Errorf("Test %#v failed, the node should have taints %+v, but got: %+v", tc.description, tc.expectedTaints, tNode.Spec.Taints)
			}
		})
	}
}

func TestRemoveSoftTaint(t *testing.T) {
	const testKey = "testTaintKey"
	const otherKey1 = "testOtherKey1"
	const otherKey2 = "testOtherKey2"
	ctx := context.Background()
	node := test.BuildTestNode("node1", 1000, 2000, 9, nil)
	node.Labels = map[string]string{"type": "compute"}

	scheme := runtime.NewScheme()
	err := v1.AddToScheme(scheme)
	if err != nil {
		t.Errorf("Failed building scheme for the fake client: %v", err)
	}

	tests := []struct {
		description     string
		nodeTaints      []v1.Taint
		expectedTaints  []v1.Taint
		expectedRemoved bool
		expectedErr     error
	}{
		{
			description:     "no taints - empty taints",
			nodeTaints:      []v1.Taint{},
			expectedTaints:  nil,
			expectedRemoved: false,
			expectedErr:     nil,
		},
		{
			description:     "no taints - nil",
			nodeTaints:      nil,
			expectedTaints:  nil,
			expectedRemoved: false,
			expectedErr:     nil,
		},
		{
			description: "only the soft taint",
			nodeTaints: []v1.Taint{
				{Key: testKey, Value: "value1", Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedTaints:  nil,
			expectedRemoved: true,
			expectedErr:     nil,
		},
		{
			description: "only another taint",
			nodeTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
			},
			expectedTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
			},
			expectedRemoved: false,
			expectedErr:     nil,
		},
		{
			description: "two other taints",
			nodeTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedRemoved: false,
			expectedErr:     nil,
		},
		{
			description: "two other taints + soft taint (begin)",
			nodeTaints: []v1.Taint{
				{Key: testKey, Value: "value1", Effect: v1.TaintEffectPreferNoSchedule},
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedRemoved: true,
			expectedErr:     nil,
		},
		{
			description: "two other taints + soft taint (middle)",
			nodeTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: testKey, Value: "value1", Effect: v1.TaintEffectPreferNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedRemoved: true,
			expectedErr:     nil,
		},
		{
			description: "two other taints + soft taint (end)",
			nodeTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
				{Key: testKey, Value: "value1", Effect: v1.TaintEffectPreferNoSchedule},
			},
			expectedTaints: []v1.Taint{
				{Key: otherKey1, Value: "value1", Effect: v1.TaintEffectNoSchedule},
				{Key: otherKey2, Value: "value2", Effect: v1.TaintEffectNoExecute},
			},
			expectedRemoved: true,
			expectedErr:     nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			node.Spec.Taints = tc.nodeTaints
			cl := fake.NewClientBuilder().WithObjects(node).WithScheme(scheme).Build()

			removed, err := RemoveSoftTaint(ctx, cl, node, testKey)
			if removed != tc.expectedRemoved {
				t.Errorf("Test %#v failed, unexepcted removed", tc.description)
			}
			if !errors.Is(err, tc.expectedErr) {
				t.Errorf("Test %#v failed, unexpected error: %v", tc.description, err)
			}
			tNode := &v1.Node{}
			err = cl.Get(ctx, client.ObjectKeyFromObject(node), tNode)
			if err != nil {
				t.Errorf("Test %#v failed, test case error: %v", tc.description, err)
			}
			if !reflect.DeepEqual(tNode.Spec.Taints, tc.expectedTaints) {
				t.Errorf("Test %#v failed, the node should have taints %+v, but got: %+v", tc.description, tc.expectedTaints, tNode.Spec.Taints)
			}
		})
	}
}
