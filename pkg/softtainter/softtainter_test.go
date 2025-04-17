package softtainter

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	promapi "github.com/prometheus/client_golang/api"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"

	deschedulerv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	pkgDirectory = "pkg/softtainter"
	testFilesLoc = "testdata"
)

type MockNodeUtilization struct {
	mockResults map[string]map[corev1.ResourceName]*resource.Quantity
	mockErrors  error
}

func (mnu *MockNodeUtilization) NodeUsageFromPrometheusMetrics(_ context.Context) (map[string]map[corev1.ResourceName]*resource.Quantity, error) {
	return mnu.mockResults, mnu.mockErrors
}

func (mnu *MockNodeUtilization) newNodeUtilizationFactory(_ promapi.Client, _ string) NodeUtilization {
	return &MockNodeUtilization{
		mockResults: mnu.mockResults,
		mockErrors:  mnu.mockErrors,
	}
}

func TestReconcile(t *testing.T) {

	ctx := context.Background()

	resourcesSchemeFuncs := []func(*apiruntime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		operatorv1.Install,
		deschedulerv1.AddToScheme,
	}

	scheme := runtime.NewScheme()

	for _, f := range resourcesSchemeFuncs {
		err := f(scheme)
		if err != nil {
			t.Errorf("Failed building scheme for the fake client: %v", err)
		}
	}

	tests := []struct {
		description     string
		nodes           []*corev1.Node
		expectednodes   []*corev1.Node
		nodeUtilization MockNodeUtilization
		expectedErr     error
		kubedescheduler deschedulerv1.KubeDescheduler
		testfilename    string
	}{
		{
			description: "3 nodes, no taints, 1 underutilized, 1 appropriately utilized, 1 overutilized",
			nodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, false),
				buildTestNodeWithTaints("node2", true, false, false, false),
				buildTestNodeWithTaints("node3", true, false, false, false),
			},
			expectednodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, false),
				buildTestNodeWithTaints("node2", true, true, false, false),
				buildTestNodeWithTaints("node3", true, true, true, false),
			},
			nodeUtilization: MockNodeUtilization{
				mockResults: map[string]map[corev1.ResourceName]*resource.Quantity{
					"node1": {"MetricResource": resource.NewQuantity(10, resource.BinarySI)},
					"node2": {"MetricResource": resource.NewQuantity(50, resource.BinarySI)},
					"node3": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
				},
				mockErrors: nil,
			},
			expectedErr: nil,
			kubedescheduler: deschedulerv1.KubeDescheduler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorclient.OperatorNamespace,
					Name:      operatorclient.OperatorConfigName,
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: pointer.Int32(30),
					Mode:                        deschedulerv1.Automatic,
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
					},
				},
			},
			testfilename: "policy.yaml",
		},
		{
			description: "3 nodes, wrong taints, 1 underutilized, 1 appropriately utilized, 1 overutilized",
			nodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, true, true, false),
				buildTestNodeWithTaints("node2", true, false, true, false),
				buildTestNodeWithTaints("node3", true, false, false, false),
			},
			expectednodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, false),
				buildTestNodeWithTaints("node2", true, true, false, false),
				buildTestNodeWithTaints("node3", true, true, true, false),
			},
			nodeUtilization: MockNodeUtilization{
				mockResults: map[string]map[corev1.ResourceName]*resource.Quantity{
					"node1": {"MetricResource": resource.NewQuantity(10, resource.BinarySI)},
					"node2": {"MetricResource": resource.NewQuantity(50, resource.BinarySI)},
					"node3": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
				},
				mockErrors: nil,
			},
			expectedErr: nil,
			kubedescheduler: deschedulerv1.KubeDescheduler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorclient.OperatorNamespace,
					Name:      operatorclient.OperatorConfigName,
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: pointer.Int32(30),
					Mode:                        deschedulerv1.Automatic,
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
					},
				},
			},
			testfilename: "policy.yaml",
		},
		{
			description: "3 nodes, extra taints, 1 underutilized, 1 appropriately utilized, 1 overutilized",
			nodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, false, false, true),
				buildTestNodeWithTaints("node3", true, false, false, true),
			},
			expectednodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, true, false, true),
				buildTestNodeWithTaints("node3", true, true, true, true),
			},
			nodeUtilization: MockNodeUtilization{
				mockResults: map[string]map[corev1.ResourceName]*resource.Quantity{
					"node1": {"MetricResource": resource.NewQuantity(10, resource.BinarySI)},
					"node2": {"MetricResource": resource.NewQuantity(50, resource.BinarySI)},
					"node3": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
				},
				mockErrors: nil,
			},
			expectedErr: nil,
			kubedescheduler: deschedulerv1.KubeDescheduler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorclient.OperatorNamespace,
					Name:      operatorclient.OperatorConfigName,
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: pointer.Int32(30),
					Mode:                        deschedulerv1.Automatic,
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
					},
				},
			},
			testfilename: "policy.yaml",
		},
		{
			description: "It should ignore nodes that are not schedulable by KubeVirt",
			nodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, false, false, true),
				buildTestNodeWithTaints("node3", true, false, false, true),
				buildTestNodeWithTaints("nodeNS4", false, false, false, true),
				buildTestNodeWithTaints("nodeNS5", false, false, false, true),
			},
			expectednodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, true, false, true),
				buildTestNodeWithTaints("node3", true, true, true, true),
				buildTestNodeWithTaints("nodeNS4", false, false, false, true),
				buildTestNodeWithTaints("nodeNS5", false, false, false, true),
			},
			nodeUtilization: MockNodeUtilization{
				mockResults: map[string]map[corev1.ResourceName]*resource.Quantity{
					"node1":   {"MetricResource": resource.NewQuantity(10, resource.BinarySI)},
					"node2":   {"MetricResource": resource.NewQuantity(50, resource.BinarySI)},
					"node3":   {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
					"nodeNS4": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
					"nodeNS5": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
				},
				mockErrors: nil,
			},
			expectedErr: nil,
			kubedescheduler: deschedulerv1.KubeDescheduler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorclient.OperatorNamespace,
					Name:      operatorclient.OperatorConfigName,
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: pointer.Int32(30),
					Mode:                        deschedulerv1.Automatic,
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
					},
				},
			},
			testfilename: "policy.yaml",
		},
		{
			description: "3 nodes, equal metric value (low)",
			nodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, false, false, true),
				buildTestNodeWithTaints("node3", true, false, false, true),
			},
			expectednodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, false, false, true),
				buildTestNodeWithTaints("node3", true, false, false, true),
			},
			nodeUtilization: MockNodeUtilization{
				mockResults: map[string]map[corev1.ResourceName]*resource.Quantity{
					"node1": {"MetricResource": resource.NewQuantity(10, resource.BinarySI)},
					"node2": {"MetricResource": resource.NewQuantity(10, resource.BinarySI)},
					"node3": {"MetricResource": resource.NewQuantity(10, resource.BinarySI)},
				},
				mockErrors: nil,
			},
			expectedErr: nil,
			kubedescheduler: deschedulerv1.KubeDescheduler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorclient.OperatorNamespace,
					Name:      operatorclient.OperatorConfigName,
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: pointer.Int32(30),
					Mode:                        deschedulerv1.Automatic,
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
					},
				},
			},
			testfilename: "policy.yaml",
		},
		{
			description: "3 nodes, equal metric value (high)",
			nodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, false, false, true),
				buildTestNodeWithTaints("node3", true, false, false, true),
			},
			expectednodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, false, false, true),
				buildTestNodeWithTaints("node3", true, false, false, true),
			},
			nodeUtilization: MockNodeUtilization{
				mockResults: map[string]map[corev1.ResourceName]*resource.Quantity{
					"node1": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
					"node2": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
					"node3": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
				},
				mockErrors: nil,
			},
			expectedErr: nil,
			kubedescheduler: deschedulerv1.KubeDescheduler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorclient.OperatorNamespace,
					Name:      operatorclient.OperatorConfigName,
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: pointer.Int32(30),
					Mode:                        deschedulerv1.Automatic,
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
					},
				},
			},
			testfilename: "policy.yaml",
		},
		{
			description: "3 nodes, RelieveAndMigrate is disabled",
			nodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, false, false, true),
				buildTestNodeWithTaints("node3", true, false, false, true),
			},
			expectednodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, false, false, true),
				buildTestNodeWithTaints("node3", true, false, false, true),
			},
			nodeUtilization: MockNodeUtilization{
				mockResults: map[string]map[corev1.ResourceName]*resource.Quantity{
					"node1": {"MetricResource": resource.NewQuantity(10, resource.BinarySI)},
					"node2": {"MetricResource": resource.NewQuantity(50, resource.BinarySI)},
					"node3": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
				},
				mockErrors: nil,
			},
			expectedErr: nil,
			kubedescheduler: deschedulerv1.KubeDescheduler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorclient.OperatorNamespace,
					Name:      operatorclient.OperatorConfigName,
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: pointer.Int32(30),
					Mode:                        deschedulerv1.Automatic,
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
					},
				},
			},
			testfilename: "policyNoRelieveAndMigrate.yaml",
		},
		{
			description: "3 nodes, RelieveAndMigrate is disabled, leftover taints",
			nodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, true, false, true),
				buildTestNodeWithTaints("node2", true, false, true, true),
				buildTestNodeWithTaints("node3", true, true, true, true),
			},
			expectednodes: []*corev1.Node{
				buildTestNodeWithTaints("node1", true, false, false, true),
				buildTestNodeWithTaints("node2", true, false, false, true),
				buildTestNodeWithTaints("node3", true, false, false, true),
			},
			nodeUtilization: MockNodeUtilization{
				mockResults: map[string]map[corev1.ResourceName]*resource.Quantity{
					"node1": {"MetricResource": resource.NewQuantity(10, resource.BinarySI)},
					"node2": {"MetricResource": resource.NewQuantity(50, resource.BinarySI)},
					"node3": {"MetricResource": resource.NewQuantity(90, resource.BinarySI)},
				},
				mockErrors: nil,
			},
			expectedErr: nil,
			kubedescheduler: deschedulerv1.KubeDescheduler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: operatorclient.OperatorNamespace,
					Name:      operatorclient.OperatorConfigName,
				},
				Spec: deschedulerv1.KubeDeschedulerSpec{
					DeschedulingIntervalSeconds: pointer.Int32(30),
					Mode:                        deschedulerv1.Automatic,
					ProfileCustomizations: &deschedulerv1.ProfileCustomizations{
						DevDeviationThresholds: &deschedulerv1.LowDeviationThreshold,
					},
				},
			},
			testfilename: "policyNoRelieveAndMigrate.yaml",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {

			objects := []client.Object{&tc.kubedescheduler}
			for _, node := range tc.nodes {
				objects = append(objects, node)
			}
			cl := fake.NewClientBuilder().WithObjects(objects...).WithScheme(scheme).Build()
			st := softTainter{
				client:                 cl,
				resyncPeriod:           60 * time.Second,
				policyConfigFile:       path.Join(getTestFilesLocation(t), tc.testfilename),
				nodeUtilizationFactory: tc.nodeUtilization.newNodeUtilizationFactory,
			}
			result, err := st.Reconcile(ctx, reconcile.Request{})
			if !errors.Is(err, tc.expectedErr) {
				t.Errorf("Test %#v failed, test case error: %v, expected error: %v", tc.description, err, tc.expectedErr)
			}

			if tc.kubedescheduler.Spec.DeschedulingIntervalSeconds == nil || result.RequeueAfter != time.Duration(*tc.kubedescheduler.Spec.DeschedulingIntervalSeconds)*time.Second {
				t.Errorf("Test %#v failed, RequeueAfter is not correctly set", tc.description)
			}

			for _, expectedNode := range tc.expectednodes {
				node := &corev1.Node{}
				err := cl.Get(ctx, client.ObjectKeyFromObject(expectedNode), node)
				if err != nil {
					t.Errorf("Test %#v failed, test case error: %v", tc.description, err)
				}

				less := func(a, b corev1.Taint) bool { return a.Key < b.Key }
				diffIgnoreOrder := cmp.Diff(expectedNode.Spec.Taints, node.Spec.Taints, cmpopts.SortSlices(less))
				if diffIgnoreOrder != "" {
					t.Errorf("Test %#v failed, node %v should different taints: %v", tc.description, node.Name, diffIgnoreOrder)
				}
			}
		})
	}
}

func getTestFilesLocation(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Errorf("Test failed, error: %v", err)
	}
	if strings.HasSuffix(wd, pkgDirectory) {
		return testFilesLoc
	}
	return path.Join(pkgDirectory, testFilesLoc)
}

func buildTestNodeWithTaints(nodeName string, kubevirtShedulable, appropriatelyUtilized, overUtilized, extra bool) *corev1.Node {
	taints := []corev1.Taint{}
	if appropriatelyUtilized {
		taints = append(taints, corev1.Taint{
			Key:    AppropriatelyUtilizedSoftTaintKey,
			Value:  AppropriatelyUtilizedSoftTaintValue,
			Effect: corev1.TaintEffectPreferNoSchedule,
		})
	}
	if overUtilized {
		taints = append(taints, corev1.Taint{
			Key:    OverUtilizedSoftTaintKey,
			Value:  OverUtilizedSoftTaintValue,
			Effect: corev1.TaintEffectPreferNoSchedule,
		})
	}
	if extra {
		taints = append(taints, corev1.Taint{
			Key:    "extra",
			Value:  "extra",
			Effect: corev1.TaintEffectNoSchedule,
		})
	}
	if len(taints) == 0 {
		taints = nil
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
		Spec: corev1.NodeSpec{
			Taints: taints,
		},
		Status: corev1.NodeStatus{
			Phase: corev1.NodeRunning,
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	if kubevirtShedulable {
		node.SetLabels(map[string]string{"kubevirt.io/schedulable": "true"})
	}
	return node
}
