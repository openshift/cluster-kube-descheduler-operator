package e2e

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	o "github.com/onsi/gomega"

	"github.com/openshift/cluster-kube-descheduler-operator/pkg/softtainter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/util/taints"
	utilpointer "k8s.io/utils/pointer"

	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
)

const (
	softTainterDeploymentName                          = "softtainter"
	softTainterServiceAccountName                      = "openshift-descheduler-softtainter"
	softTainterClusterRoleName                         = "openshift-descheduler-softtainter"
	softTainterClusterRoleBindingName                  = "openshift-descheduler-softtainter"
	softTainterClusterMonitoringViewClusterRoleBinding = "openshift-descheduler-softtainter-monitoring"
	softTainterValidatingAdmissionPolicyName           = "openshift-descheduler-softtainter-vap"
	softTainterValidatingAdmissionPolicyBindingName    = "openshift-descheduler-softtainter-vap-binding"
)

// TestExtended runs the operator tests using standard Go testing.
func TestExtended(t *testing.T) {
	// Register Gomega with the testing framework for standard Go test mode
	o.RegisterTestingT(t)

	t.Run("Descheduler Operator", func(t *testing.T) {
		var ctx context.Context
		var cancelFnc context.CancelFunc
		var kubeClient *k8sclient.Clientset
		var err error

		if os.Getenv("SKIP_OPERATOR_INSTALL") == "true" {
			klog.Infof("SKIP_OPERATOR_INSTALL=true: skipping operator installation, verifying existing operator")
			kubeClient = GetKubeClient()
			ctx, cancelFnc = context.WithCancel(context.TODO())

			klog.Infof("Verifying deployments in %s namespace are running...", operatorclient.OperatorNamespace)
			o.Eventually(func() bool {
				deployments, listErr := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{})
				if listErr != nil {
					klog.Errorf("Unable to list deployments: %v", listErr)
					return false
				}
				if len(deployments.Items) == 0 {
					klog.Infof("No deployments found yet in %s", operatorclient.OperatorNamespace)
					return false
				}
				for _, dep := range deployments.Items {
					if dep.Spec.Replicas != nil && dep.Status.ReadyReplicas < *dep.Spec.Replicas {
						klog.Infof("Deployment %s not ready: %d/%d replicas", dep.Name, dep.Status.ReadyReplicas, *dep.Spec.Replicas)
						return false
					}
				}
				return true
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(o.BeTrue(), "Deployments in operator namespace not ready")
			klog.Infof("All deployments in %s namespace are running", operatorclient.OperatorNamespace)

			deschClient := GetDeschedulerClient()
			klog.Infof("Ensuring base KubeDescheduler CR exists...")
			if crErr := operatorConfigsAppliers[baseConf](ctx, deschClient); crErr != nil {
				t.Fatalf("Failed to apply base KubeDescheduler CR: %v", crErr)
			}
			klog.Infof("Base KubeDescheduler CR applied")

			klog.Infof("Waiting for operand deployment to be ready...")
			_, err = waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName, operatorclient.OperandName+"-operator")
			if err != nil {
				t.Fatalf("Operand not running: %v", err)
			}
			klog.Infof("Operand deployment verified")
		} else {
			ctx, cancelFnc, kubeClient, err = setupOperator(t)
			if err != nil {
				t.Fatalf("Failed to setup operator: %v", err)
			}
		}
		defer cancelFnc()

		t.Run("Deploying soft tainter controller", func(t *testing.T) {
			deschClient := GetDeschedulerClient()

			// ensure that softtainter additional objects are not there
			if err := checkSoftTainterObjects(ctx, kubeClient, operatorclient.OperatorNamespace, false); err != nil {
				t.Fatalf("Unexpected softTainter object: %v", err)
			}
			klog.Infof("No one of the softtainter additonal objects is there")

			cleanup := setupSoftTainterController(ctx, t, kubeClient, deschClient)
			defer cleanup()

			// wait for descheduler pod to be running
			deschPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName, operatorclient.OperandName+"-operator")
			if err != nil {
				t.Fatalf("Unable to wait for the Descheduler pod to run")
			}
			klog.Infof("Descheduler pod running in %v", deschPod.Name)

			// ensure that all the softtainter additional objects are there
			if err = checkSoftTainterObjects(ctx, kubeClient, operatorclient.OperatorNamespace, true); err != nil {
				t.Fatalf("Missing expected softTainter object: %v", err)
			}
			klog.Infof("All the softtainter additonal objects got properly created")

			// apply test soft taints
			if err = applySoftTaints(ctx, kubeClient); err != nil {
				t.Fatalf("Failed applying softtaints: %v", err)
			}
			defer func(ctx context.Context, kubeClient *k8sclient.Clientset) {
				err := removeSoftTaints(ctx, kubeClient)
				if err != nil {
					t.Fatalf("Failed cleaning softtaints: %v", err)
				}
			}(ctx, kubeClient)

			// revert to base confing for the operator
			err = operatorConfigsAppliers[baseConf](ctx, deschClient)
			if err != nil {
				t.Fatalf("Unable to apply a CR for Descheduler operator: %v", err)
			}
			klog.Infof("Descheduler operator is now configured with base profile")

			// wait for softtainter pod to disappear
			err = waitForPodGoneByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.SoftTainterOperandName, operatorclient.OperandName+"-operator")
			if err != nil {
				t.Fatalf("Unable to wait for the softtainter pod to disappear")
			}
			klog.Infof("softtainer pod disappeared")

			// ensure that all the softtainter additional objects are gone
			if err = checkSoftTainterObjects(ctx, kubeClient, operatorclient.OperatorNamespace, false); err != nil {
				t.Fatalf("Unexpected softTainter object: %v", err)
			}
			klog.Infof("No one of the softtainter additonal objects is there")

			// ensure that all the test soft taints got cleaned up
			softTaint := v1.Taint{Key: softtainter.AppropriatelyUtilizedSoftTaintKey, Value: softtainter.AppropriatelyUtilizedSoftTaintValue, Effect: v1.TaintEffectPreferNoSchedule}
			nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: workersLabelSelector})
			if err != nil {
				t.Fatalf("Unexpected error fetching nodes: %v", err)
			}
			for _, node := range nodes.Items {
				if taints.TaintExists(node.Spec.Taints, &softTaint) {
					t.Fatalf("Unexpected leftover softtaint on node %v", node.Name)
				}
			}
			klog.Infof("All the test softtaints got properly cleaned up")
		})

		t.Run("Deploying soft tainter controller with Validating Admission Policy", func(t *testing.T) {
			deschClient := GetDeschedulerClient()

			cleanup := setupSoftTainterController(ctx, t, kubeClient, deschClient)
			defer cleanup()

			saKubeconfig := os.Getenv("KUBECONFIG")
			saConfig, err := clientcmd.BuildConfigFromFlags("", saKubeconfig)
			if err != nil {
				t.Fatalf("Unable to build config: %v", err)
			}
			saConfig.Impersonate = rest.ImpersonationConfig{
				UserName: fmt.Sprintf("system:serviceaccount:%v:%v", operatorclient.OperatorNamespace, softTainterServiceAccountName),
			}

			stClientset, err := k8sclient.NewForConfig(saConfig)
			if err != nil {
				t.Fatalf("Unable to build client: %v", err)
			}

			nodes, err := stClientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: workersLabelSelector})
			if err != nil {
				t.Fatalf("Unable to fetch nodes: %v", err)
			}
			if len(nodes.Items) < 1 {
				t.Fatalf("Unable to find a test node: %v", err)
			}
			tNode := &nodes.Items[0]

			defer func(ctx context.Context, kubeClient *k8sclient.Clientset) {
				err := removeSoftTaints(ctx, kubeClient)
				if err != nil {
					t.Fatalf("Failed cleaning softtaints: %v", err)
				}
			}(ctx, kubeClient)

			softTaint := v1.Taint{Key: softtainter.AppropriatelyUtilizedSoftTaintKey, Value: softtainter.AppropriatelyUtilizedSoftTaintValue, Effect: v1.TaintEffectPreferNoSchedule}
			tryAddingTaintWithExpectedSuccess(ctx, t, stClientset, tNode, &softTaint)
			klog.Infof("softtainter SA is allowed to apply a softtaint with the right key prefix")

			tryRemovingTaintWithExpectedSuccess(ctx, t, stClientset, tNode, &softTaint)
			klog.Infof("softtainter SA is allowed to delete a softtaint with the right key prefix")

			badSoftTaint := v1.Taint{Key: "wrongKey", Value: softtainter.AppropriatelyUtilizedSoftTaintValue, Effect: v1.TaintEffectPreferNoSchedule}
			tryAddingTaintWithExpectedFailure(ctx, t, stClientset, tNode, &badSoftTaint)
			klog.Infof("softtainter SA is not allowed to apply a softtaint with a wrong key prefix")

			hardTaint := v1.Taint{Key: softtainter.AppropriatelyUtilizedSoftTaintKey, Value: softtainter.AppropriatelyUtilizedSoftTaintValue, Effect: v1.TaintEffectNoSchedule}
			tryAddingTaintWithExpectedFailure(ctx, t, stClientset, tNode, &hardTaint)
			klog.Infof("softtainter SA is not allowed to apply hard taints")

			// apply wrong softtaint and hard taint as test executor
			unremovableTaints := []*v1.Taint{&badSoftTaint, &hardTaint}
			for _, taint := range unremovableTaints {
				tryAddingTaintWithExpectedSuccess(ctx, t, kubeClient, tNode, taint)
			}
			defer func(ctx context.Context, kubeClient *k8sclient.Clientset, node *v1.Node, taints []*v1.Taint) {
				for _, taint := range taints {
					tryRemovingTaintWithExpectedSuccess(ctx, t, kubeClient, tNode, taint)
				}
			}(ctx, kubeClient, tNode, unremovableTaints)

			tryRemovingTaintWithExpectedFailure(ctx, t, stClientset, tNode, &badSoftTaint)
			klog.Infof("softtainter SA is not allowed to remove a softtaint with a wrong key prefix")

			tryRemovingTaintWithExpectedFailure(ctx, t, stClientset, tNode, &hardTaint)
			klog.Infof("softtainter SA is not allowed to remove a hard taint")
		})

		t.Run("Descheduling a pod", func(t *testing.T) {
			testNamespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "e2e-testdescheduling"}}
			if _, err := kubeClient.CoreV1().Namespaces().Create(ctx, testNamespace, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Unable to create ns %v", testNamespace.Name)
			}
			deploymentObj := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace.Name,
					Name:      "test-descheduler-operator-pod",
					Labels:    map[string]string{"app": "test-descheduler-operator-pod"},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: utilpointer.Int32(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test-descheduler-operator-pod"},
					},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test-descheduler-operator-pod"},
						},
						Spec: corev1.PodSpec{
							SecurityContext: &corev1.PodSecurityContext{
								RunAsNonRoot: utilpointer.BoolPtr(true),
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
							Containers: []corev1.Container{{
								SecurityContext: &corev1.SecurityContext{
									AllowPrivilegeEscalation: utilpointer.BoolPtr(false),
									Capabilities: &corev1.Capabilities{
										Drop: []corev1.Capability{
											"ALL",
										},
									},
								},
								Name:            "pause",
								ImagePullPolicy: "Always",
								Image:           "registry.k8s.io/pause",
								Ports:           []corev1.ContainerPort{{ContainerPort: 80}},
							}},
						},
					},
				},
			}
			defer cleanupTestNamespace(t, ctx, kubeClient, testNamespace.Name)
			if _, err := kubeClient.AppsV1().Deployments(testNamespace.Name).Create(ctx, deploymentObj, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Unable to create a deployment: %v", err)
			}
			defer kubeClient.AppsV1().Deployments(testNamespace.Name).Delete(ctx, deploymentObj.Name, metav1.DeleteOptions{})

			waitForPodsRunning(ctx, t, kubeClient, map[string]string{"app": "test-descheduler-operator-pod"}, 1, testNamespace.Name)

			podList, err := kubeClient.CoreV1().Pods(testNamespace.Name).List(ctx, metav1.ListOptions{})
			initialPodNames := getPodNames(podList.Items)
			t.Logf("Initial test pods: %v", initialPodNames)
			if err != nil {
				t.Fatalf("Unable to get pods: %v", err)
			}

			time.Sleep(40 * time.Second)

			o.Eventually(func() bool {
				klog.Infof("Listing pods...")
				podList, err := kubeClient.CoreV1().Pods(testNamespace.Name).List(ctx, metav1.ListOptions{})
				if err != nil {
					klog.Errorf("Unable to get pods: %v", err)
					return false
				}
				excludePodNames := getPodNames(podList.Items)
				sort.Strings(excludePodNames)
				t.Logf("Existing pods: %v", excludePodNames)
				// validate no pods were deleted
				if len(intersectStrings(initialPodNames, excludePodNames)) > 0 {
					t.Logf("Not every pod was evicted")
					return false
				}
				return true
			}).WithTimeout(3*time.Minute).WithPolling(1*time.Second).Should(o.BeTrue(), "error while waiting for pod")
		})

		// Get expected resources from bindata
		metricsService := getMetricsService()
		serviceMonitor := getServiceMonitor()
		deployment := getDeschedulerDeployment()

		// Extract pod labels from deployment spec and validate there's exactly one label selector
		podLabels := deployment.Spec.Template.Labels
		if len(podLabels) != 1 {
			t.Fatalf("Expected exactly one label selector for descheduler pods, got %d", len(podLabels))
		}

		// Get the metrics port (must be exactly one port)
		if len(metricsService.Spec.Ports) != 1 {
			t.Fatalf("Expected exactly one port in metrics service, got %d", len(metricsService.Spec.Ports))
		}
		metricsPort := metricsService.Spec.Ports[0]

		t.Run("Metrics service exists", func(t *testing.T) {
			testMetricsServiceExists(t, ctx, kubeClient, metricsService.Name, metricsService.Labels, metricsPort)
		})

		t.Run("ServiceMonitor exists", func(t *testing.T) {
			testServiceMonitorExists(t, ctx, kubeClient, serviceMonitor.Name, metricsService.Labels)
		})

		t.Run("Prometheus target is up", func(t *testing.T) {
			testPrometheusTargetUp(t, ctx, kubeClient, serviceMonitor.Name, metricsService.Name, metricsService.Labels, metricsPort, podLabels)
		})

		t.Run("Metrics data available", func(t *testing.T) {
			testMetricsDataAvailable(t, ctx, kubeClient, podLabels)
		})
	})
}

func getPodNames(pods []v1.Pod) []string {
	names := []string{}
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	return names
}

func intersectStrings(lista, listb []string) []string {
	commonNames := []string{}

	for _, stra := range lista {
		for _, strb := range listb {
			if stra == strb {
				commonNames = append(commonNames, stra)
				break
			}
		}
	}

	return commonNames
}

func waitForPodsRunning(ctx context.Context, t *testing.T, clientSet *k8sclient.Clientset, labelMap map[string]string, desireRunningPodNum int, namespace string) {
	o.Eventually(func() bool {
		podList, err := clientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labelMap).String(),
		})
		if err != nil {
			return false
		}
		if len(podList.Items) != desireRunningPodNum {
			t.Logf("Waiting for %v pods to be running, got %v instead", desireRunningPodNum, len(podList.Items))
			return false
		}
		for _, pod := range podList.Items {
			if pod.Status.Phase != v1.PodRunning {
				t.Logf("Pod %v not running yet, is %v instead", pod.Name, pod.Status.Phase)
				return false
			}
		}
		return true
	}).WithTimeout(60*time.Second).WithPolling(10*time.Second).Should(o.BeTrue(), "Error waiting for pods running")
}

func waitForPodGoneByNamePrefix(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, nameprefix, excludedprefix string) error {
	// Wait until a no pods match nameprefix
	o.Eventually(func() bool {
		klog.Infof("Listing pods...")
		podItems, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.Errorf("Unable to list pods: %v", err)
			return false
		}
		for _, pod := range podItems.Items {
			if !strings.HasPrefix(pod.Name, nameprefix) || (excludedprefix != "" && strings.HasPrefix(pod.Name, excludedprefix)) {
				continue
			}
			klog.Infof("Found pod: %v, phase: %v, deletionTS: %v\n", pod.Name, pod.Status.Phase, pod.GetDeletionTimestamp())
			return false
		}
		return true
	}).WithTimeout(1 * time.Minute).WithPolling(5 * time.Second).Should(o.BeTrue())
	return nil
}

func checkSoftTainterObjects(ctx context.Context, kubeClient *k8sclient.Clientset, namespace string, expected bool) error {
	var obj runtime.Object
	var err error

	obj, err = kubeClient.AppsV1().Deployments(namespace).Get(ctx, softTainterDeploymentName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.CoreV1().ServiceAccounts(namespace).Get(ctx, softTainterServiceAccountName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.RbacV1().ClusterRoles().Get(ctx, softTainterClusterRoleName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, softTainterClusterRoleBindingName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, softTainterClusterMonitoringViewClusterRoleBinding, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(ctx, softTainterValidatingAdmissionPolicyName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Get(ctx, softTainterValidatingAdmissionPolicyBindingName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}

	return nil

}

func checkExpected(expected bool, obj runtime.Object, err error) error {
	if err != nil {
		if expected {
			return err
		} else {
			if apierrors.IsNotFound(err) {
				return nil
			} else {
				return err
			}
		}
	} else {
		if expected {
			return nil
		} else {
			metaObj, merr := meta.Accessor(obj)
			if merr != nil {
				return fmt.Errorf("cannot get metadata: %w", merr)
			}
			return fmt.Errorf("Found unxepected %v %v", obj.GetObjectKind().GroupVersionKind().String(), metaObj.GetName())
		}
	}
}

func applySoftTaints(ctx context.Context, kubeClient *k8sclient.Clientset) error {
	softTaint := v1.Taint{Key: softtainter.AppropriatelyUtilizedSoftTaintKey, Value: softtainter.AppropriatelyUtilizedSoftTaintValue, Effect: v1.TaintEffectPreferNoSchedule}
	nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: workersLabelSelector})
	if err != nil {
		return err
	}
	for _, node := range nodes.Items {
		tNode, updated, terr := taints.AddOrUpdateTaint(&node, &softTaint)
		if terr != nil {
			return terr
		}
		if updated {
			uerr := updateNodeAndRetryOnConflicts(ctx, kubeClient, tNode, metav1.UpdateOptions{})
			if uerr != nil {
				return uerr
			}
		}
	}
	return nil
}

func removeSoftTaints(ctx context.Context, kubeClient *k8sclient.Clientset) error {
	softTaint := v1.Taint{Key: softtainter.AppropriatelyUtilizedSoftTaintKey, Value: softtainter.AppropriatelyUtilizedSoftTaintValue, Effect: v1.TaintEffectPreferNoSchedule}
	nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: workersLabelSelector})
	if err != nil {
		return err
	}
	for _, node := range nodes.Items {
		tNode, updated, terr := taints.RemoveTaint(&node, &softTaint)
		if terr != nil {
			return terr
		}
		if updated {
			uerr := updateNodeAndRetryOnConflicts(ctx, kubeClient, tNode, metav1.UpdateOptions{})
			if uerr != nil {
				return uerr
			}
		}
	}
	return nil
}

func tryUpdatingTaintWithExpectation(ctx context.Context, t *testing.T, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint, add, expectedSuccess bool) {
	rnode, err := clientSet.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed refreshing node: %v", err)
	}
	var tNode *corev1.Node
	var updated bool
	var tErr error
	var taintOperation string
	if add {
		tNode, updated, tErr = taints.AddOrUpdateTaint(rnode, taint)
		if tErr != nil {
			t.Fatalf("Failed applying taint: %v", tErr)
		}
		taintOperation = "apply"
	} else {
		tNode, updated, tErr = taints.RemoveTaint(rnode, taint)
		if tErr != nil {
			t.Fatalf("Failed removing taint: %v", tErr)
		}
		taintOperation = "remove"
	}
	if updated {
		uerr := updateNodeAndRetryOnConflicts(ctx, clientSet, tNode, metav1.UpdateOptions{})
		if expectedSuccess {
			if uerr != nil {
				t.Fatalf("Failed trying to %v taint to node: %v: %v", taintOperation, tNode.Name, uerr)
			}
		} else {
			expectedErr := fmt.Sprintf(
				"is forbidden: ValidatingAdmissionPolicy '%v' with binding '%v' denied request: User system:serviceaccount:%v:%v is",
				softTainterValidatingAdmissionPolicyName,
				softTainterValidatingAdmissionPolicyBindingName,
				operatorclient.OperatorNamespace,
				softTainterServiceAccountName)
			if uerr == nil {
				t.Fatalf("softtaint SA was allowed to %v taint %v, %v to node: %v", taintOperation, taint.Key, taint.Effect, tNode.Name)
			} else if !strings.Contains(uerr.Error(), expectedErr) {
				t.Fatalf("unexpected error: %v", uerr)
			}
		}
	} else {
		t.Fatalf("trying to %v taint %v, %v on/from node %v produces no changes", taintOperation, taint.Key, taint.Effect, tNode.Name)
	}
}

func tryAddingTaintWithExpectedSuccess(ctx context.Context, t *testing.T, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint) {
	tryUpdatingTaintWithExpectation(ctx, t, clientSet, node, taint, true, true)
}

func tryAddingTaintWithExpectedFailure(ctx context.Context, t *testing.T, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint) {
	tryUpdatingTaintWithExpectation(ctx, t, clientSet, node, taint, true, false)
}

func tryRemovingTaintWithExpectedSuccess(ctx context.Context, t *testing.T, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint) {
	tryUpdatingTaintWithExpectation(ctx, t, clientSet, node, taint, false, true)
}

func tryRemovingTaintWithExpectedFailure(ctx context.Context, t *testing.T, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint) {
	tryUpdatingTaintWithExpectation(ctx, t, clientSet, node, taint, false, false)
}
