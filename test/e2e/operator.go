package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	o "github.com/onsi/gomega"

	"github.com/openshift/cluster-kube-descheduler-operator/pkg/softtainter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextclientv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
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
	"k8s.io/utils/clock"
	utilpointer "k8s.io/utils/pointer"

	descv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	deschclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	ssscheme "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned/scheme"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/cluster-kube-descheduler-operator/test/e2e/bindata"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
)

const (
	baseConf                                           = "base"
	kubeVirtRelieveAndMigrateConf                      = "devKubeVirtRelieveAndMigrate"
	kubeVirtLabelKey                                   = "kubevirt.io/schedulable"
	kubeVirtLabelValue                                 = "true"
	workersLabelSelector                               = "node-role.kubernetes.io/worker="
	EXPERIMENTAL_DISABLE_PSI_CHECK                     = "EXPERIMENTAL_DISABLE_PSI_CHECK"
	deschedulerOperandServiceAccountName               = "openshift-descheduler-operand"
	deschedulerOperandClusterRoleName                  = "openshift-descheduler-operand"
	deschedulerOperandClusterRoleBindingName           = "openshift-descheduler-operand"
	deschedulerOperandRoleName                         = "openshift-descheduler-operand"
	deschedulerOperandRoleBindingName                  = "openshift-descheduler-operand"
	softTainterDeploymentName                          = "softtainter"
	softTainterServiceAccountName                      = "openshift-descheduler-softtainter"
	softTainterClusterRoleName                         = "openshift-descheduler-softtainter"
	softTainterClusterRoleBindingName                  = "openshift-descheduler-softtainter"
	softTainterRoleName                                = "openshift-descheduler-softtainter"
	softTainterRoleBindingName                         = "openshift-descheduler-softtainter"
	softTainterClusterMonitoringViewClusterRoleBinding = "openshift-descheduler-softtainter-monitoring"
	softTainterValidatingAdmissionPolicyName           = "openshift-descheduler-softtainter-vap"
	softTainterValidatingAdmissionPolicyBindingName    = "openshift-descheduler-softtainter-vap-binding"
	allowNetworkPolicyOperandName                      = "allow-all-egress-and-metrics-ingress-operand"
	allowNetworkPolicySoftTainterName                  = "allow-all-egress-and-health-ingress-operand-softtainter"
	softTainterAppLabel                                = "softtainer"
	deschedulerMetricsPort                             = int32(10258)
	softTainterHealthPort                              = int32(6060)
	monitoringNamespace                                = "openshift-monitoring"
	deschedulerMetricsURL                              = "https://metrics.openshift-kube-descheduler-operator.svc:10258/metrics"
	networkPolicyCurlImage                             = "curlimages/curl:8.5.0"
)

var operatorConfigsAppliers = map[string]func(context.Context, *deschclient.Clientset) error{
	baseConf:                      operatorConfigsApplier("assets/07_descheduler-operator.cr.yaml"),
	kubeVirtRelieveAndMigrateConf: operatorConfigsApplier("assets/07_descheduler-operator.cr.devKubeVirtRelieveAndMigrate.yaml"),
}

func operatorConfigsApplier(path string) func(context.Context, *deschclient.Clientset) error {
	return func(ctx context.Context, deschClient *deschclient.Clientset) error {
		requiredObj, err := runtime.Decode(ssscheme.Codecs.UniversalDecoder(descv1.SchemeGroupVersion), bindata.MustAsset(path))
		if err != nil {
			klog.Errorf("Unable to decode %v: %v", path, err)
			return err
		}
		requiredDesch := requiredObj.(*descv1.KubeDescheduler)
		existingDesch, err := deschClient.KubedeschedulersV1().KubeDeschedulers(requiredDesch.Namespace).Get(ctx, requiredDesch.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				_, err = deschClient.KubedeschedulersV1().KubeDeschedulers(requiredDesch.Namespace).Create(ctx, requiredDesch, metav1.CreateOptions{})
				return err
			} else {
				return err
			}
		}
		requiredDesch.Spec.DeepCopyInto(&existingDesch.Spec)
		existingDesch.ObjectMeta.Annotations = requiredDesch.ObjectMeta.Annotations
		existingDesch.ObjectMeta.Labels = requiredDesch.ObjectMeta.Labels
		_, err = deschClient.KubedeschedulersV1().KubeDeschedulers(requiredDesch.Namespace).Update(ctx, existingDesch, metav1.UpdateOptions{})
		// retry once on conflicts
		if apierrors.IsConflict(err) {
			existingDesch, err = deschClient.KubedeschedulersV1().KubeDeschedulers(requiredDesch.Namespace).Get(ctx, requiredDesch.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			requiredDesch.Spec.DeepCopyInto(&existingDesch.Spec)
			existingDesch.ObjectMeta.Annotations = requiredDesch.ObjectMeta.Annotations
			existingDesch.ObjectMeta.Labels = requiredDesch.ObjectMeta.Labels
			_, err = deschClient.KubedeschedulersV1().KubeDeschedulers(requiredDesch.Namespace).Update(ctx, existingDesch, metav1.UpdateOptions{})
		}
		return err
	}
}

// setupOperator sets up the operator and waits for it to be ready.
// This function works with both standard Go testing and Ginkgo.
func setupOperator(
	ctx context.Context,
	kubeClient *k8sclient.Clientset,
	deschClient *deschclient.Clientset,
	apiExtClient *apiextclientv1.Clientset,
) error {
	eventRecorder := events.NewKubeRecorder(kubeClient.CoreV1().Events("default"), "test-e2e", &corev1.ObjectReference{}, clock.RealClock{})

	assets := []struct {
		path           string
		readerAndApply func(objBytes []byte) error
	}{
		{
			path: "assets/00_kube-descheduler-operator-crd.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyCustomResourceDefinitionV1(ctx, apiExtClient.ApiextensionsV1(), eventRecorder, resourceread.ReadCustomResourceDefinitionV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/01_namespace.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyNamespace(ctx, kubeClient.CoreV1(), eventRecorder, resourceread.ReadNamespaceV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/02_serviceaccount.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyServiceAccount(ctx, kubeClient.CoreV1(), eventRecorder, resourceread.ReadServiceAccountV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/03_clusterrole.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRole(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/04_clusterrolebinding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/04_prometheus-cluster-role-binding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/03_role.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyRole(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadRoleV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/03_rolebinding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/05_deployment.yaml",
			readerAndApply: func(objBytes []byte) error {
				required := resourceread.ReadDeploymentV1OrDie(objBytes)
				// override the operator image with the one built in the CI

				var operator_image, operand_image string

				if os.Getenv("OPERATOR_IMAGE") != "" {
					operator_image = os.Getenv("OPERATOR_IMAGE")
				} else {
					// E.g. RELEASE_IMAGE_LATEST=registry.build03.ci.openshift.org/ci-op-52fj47p4/stable:${component}
					registry := strings.Split(os.Getenv("RELEASE_IMAGE_LATEST"), "/")[0]
					operator_image = registry + "/" + os.Getenv("NAMESPACE") + "/" + "pipeline:cluster-kube-descheduler-operator"
				}

				if os.Getenv("OPERAND_IMAGE") != "" {
					operand_image = os.Getenv("OPERAND_IMAGE")
				} else {
					operand_image = "quay.io/jchaloup/descheduler:v5.3.2-0"
				}

				required.Spec.Template.Spec.Containers[0].Image = operator_image
				// OPERAND_IMAGE env
				for i, env := range required.Spec.Template.Spec.Containers[0].Env {
					if env.Name == "RELATED_IMAGE_OPERAND_IMAGE" {
						required.Spec.Template.Spec.Containers[0].Env[i].Value = operand_image
					} else if env.Name == "RELATED_IMAGE_SOFTTAINTER_IMAGE" {
						required.Spec.Template.Spec.Containers[0].Env[i].Value = operator_image
					}
				}
				_, _, err := resourceapply.ApplyDeployment(
					ctx,
					kubeClient.AppsV1(),
					eventRecorder,
					required,
					1000, // any random high number
				)
				return err
			},
		},
		{
			path: "assets/06_configmap.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyConfigMap(ctx, kubeClient.CoreV1(), eventRecorder, resourceread.ReadConfigMapV1OrDie(objBytes))
				return err
			},
		},
	}

	// create required resources, e.g. namespace, crd, roles
	o.Eventually(func() bool {
		for _, asset := range assets {
			klog.Infof("Creating %v", asset.path)
			if err := asset.readerAndApply(bindata.MustAsset(asset.path)); err != nil {
				klog.Errorf("Unable to create %v: %v", asset.path, err)
				return false
			}
		}
		return true
	}).WithTimeout(10*time.Second).WithPolling(1*time.Second).Should(o.BeTrue(), "Unable to create Descheduler operator resources")

	// apply base CR for the operator
	err := operatorConfigsAppliers[baseConf](ctx, deschClient)
	if err != nil {
		klog.Errorf("Unable to apply a CR for Descheduler operator: %v", err)
		return fmt.Errorf("Unable to apply a CR for Descheduler operator: %v", err)
	}

	// wait for descheduler operator pod to be running
	deschOpPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName+"-operator", "")
	if err != nil {
		klog.Errorf("Unable to wait for the Descheduler operator pod to run")
		return fmt.Errorf("Unable to wait for the Descheduler operator pod to run")
	}
	klog.Infof("Descheduler operator pod running in %v", deschOpPod.Name)

	// wait for descheduler pod to be running
	deschPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName, operatorclient.OperandName+"-operator")
	if err != nil {
		klog.Errorf("Unable to wait for the Descheduler pod to run")
		return fmt.Errorf("Unable to wait for the Descheduler pod to run")
	}
	klog.Infof("Descheduler (operand) pod running in %v", deschPod.Name)

	// ensure that all the descheduler operand objects are there
	if err = checkDeschedulerOperandObjects(ctx, kubeClient, operatorclient.OperatorNamespace, true); err != nil {
		klog.Errorf("Missing expected descheduler operand object: %v", err)
		return fmt.Errorf("Missing expected descheduler operand object: %v", err)
	}
	klog.Infof("All the descheduler operand objects got properly created")

	return nil
}

// testSoftTainterController tests the soft tainter controller lifecycle.
// This function works with both standard Go testing and Ginkgo.
func testSoftTainterController(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
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
}

// testSoftTainterControllerWithVAP tests the soft tainter controller with Validating Admission Policy.
// This function works with both standard Go testing and Ginkgo.
func testSoftTainterControllerWithVAP(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
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
}

// testPodDescheduling tests that pods can be descheduled.
// This function works with both standard Go testing and Ginkgo.
func testPodDescheduling(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
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
}

// testMetricsService tests that the metrics service exists and is properly configured.
// This function works with both standard Go testing and Ginkgo.
func testMetricsService(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	// Get expected resources from bindata
	metricsService := getMetricsService()

	// Extract pod labels from deployment spec and validate there's exactly one label selector
	deployment := getDeschedulerDeployment()
	podLabels := deployment.Spec.Template.Labels
	if len(podLabels) != 1 {
		t.Fatalf("Expected exactly one label selector for descheduler pods, got %d", len(podLabels))
	}

	// Get the metrics port (must be exactly one port)
	if len(metricsService.Spec.Ports) != 1 {
		t.Fatalf("Expected exactly one port in metrics service, got %d", len(metricsService.Spec.Ports))
	}
	metricsPort := metricsService.Spec.Ports[0]

	testMetricsServiceExists(t, ctx, kubeClient, metricsService.Name, metricsService.Labels, metricsPort)
}

// testServiceMonitor tests that the ServiceMonitor exists and is properly configured.
// This function works with both standard Go testing and Ginkgo.
func testServiceMonitor(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	// Get expected resources from bindata
	metricsService := getMetricsService()
	serviceMonitor := getServiceMonitor()

	testServiceMonitorExists(t, ctx, kubeClient, serviceMonitor.Name, metricsService.Labels)
}

// testOperandNetworkPolicy verifies the descheduler operand NetworkPolicy exists with the
// expected podSelector, egress allow-all, and metrics ingress on TCP 10258 from monitoring namespaces.
func testOperandNetworkPolicy(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	klog.Infof("Verifying operand NetworkPolicy %q", allowNetworkPolicyOperandName)

	var policy *networkingv1.NetworkPolicy
	o.Eventually(func(g o.Gomega) {
		var err error
		policy, err = kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicyOperandName, metav1.GetOptions{})
		g.Expect(err).NotTo(o.HaveOccurred(), "operand NetworkPolicy should exist")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())

	o.Expect(policy.Spec.PodSelector.MatchLabels).To(o.HaveKeyWithValue("app", operatorclient.OperandName))
	o.Expect(policy.Spec.PolicyTypes).To(o.ContainElements(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress))
	o.Expect(policy.Spec.Egress).To(o.HaveLen(1), "operand NetworkPolicy should allow all egress")
	o.Expect(policy.Spec.Egress[0].To).To(o.BeEmpty())
	o.Expect(policy.Spec.Egress[0].Ports).To(o.BeEmpty())

	o.Expect(policy.Spec.Ingress).To(o.HaveLen(1), "operand NetworkPolicy should have exactly one ingress rule")
	rule := policy.Spec.Ingress[0]
	o.Expect(rule.Ports).To(o.HaveLen(1), "operand ingress should allow a single port")
	o.Expect(rule.Ports[0].Protocol).NotTo(o.BeNil())
	o.Expect(*rule.Ports[0].Protocol).To(o.Equal(corev1.ProtocolTCP), "metrics ingress must be TCP")
	o.Expect(rule.Ports[0].Port).NotTo(o.BeNil())
	o.Expect(rule.Ports[0].Port.IntVal).To(o.Equal(deschedulerMetricsPort))

	o.Expect(rule.From).To(o.HaveLen(3), "metrics ingress should allow exactly three monitoring namespace peers")
	expectedNSSelectors := []map[string]string{
		{"openshift.io/cluster-monitoring": "true"},
		{"kubernetes.io/metadata.name": monitoringNamespace},
		{"kubernetes.io/metadata.name": "openshift-user-workload-monitoring"},
	}
	for _, expected := range expectedNSSelectors {
		o.Expect(hasNamespaceSelectorPeer(rule.From, expected)).To(o.BeTrue(),
			"metrics ingress missing namespaceSelector %#v", expected)
	}
	for _, peer := range rule.From {
		o.Expect(peer.NamespaceSelector).NotTo(o.BeNil(), "metrics peers must use namespaceSelector")
		o.Expect(peer.IPBlock).To(o.BeNil(), "metrics peers must not use IPBlock")
		// PodSelector-only peers are scoped to the NetworkPolicy namespace and do not
		// prove access from monitoring namespaces.
		if peer.PodSelector != nil {
			o.Expect(peer.PodSelector.MatchLabels).To(o.BeEmpty())
			o.Expect(peer.PodSelector.MatchExpressions).To(o.BeEmpty())
		}
	}

	o.Expect(policy.OwnerReferences).NotTo(o.BeEmpty(), "operand NetworkPolicy should have owner reference")
	o.Expect(policy.OwnerReferences[0].Kind).To(o.Equal("KubeDescheduler"))
	o.Expect(policy.OwnerReferences[0].Name).To(o.Equal(operatorclient.OperatorConfigName))

	klog.Infof("Operand NetworkPolicy verified successfully")
}

// testOperandNetworkPolicyAllowFromMonitoring verifies TCP 10258 metrics is reachable from openshift-monitoring.
func testOperandNetworkPolicyAllowFromMonitoring(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	ensureDeschedulerOperandReady(t, ctx, kubeClient)

	podName := "np-allow-metrics"
	cleanup := createNetworkPolicyCurlPod(t, ctx, kubeClient, monitoringNamespace, podName)
	defer cleanup()

	assertHTTPSEndpointReachable(ctx, kubeClient, monitoringNamespace, podName, deschedulerMetricsURL,
		"metrics scrape from monitoring namespace")
	klog.Infof("Allow path from %s verified", monitoringNamespace)
}

// testOperandNetworkPolicyDenyFromNonMonitoring verifies TCP 10258 metrics is blocked outside monitoring namespaces.
func testOperandNetworkPolicyDenyFromNonMonitoring(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	ensureDeschedulerOperandReady(t, ctx, kubeClient)

	nsName := fmt.Sprintf("np-deny-%d", time.Now().Unix())
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce": "baseline",
			},
		},
	}, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should create deny test namespace")
	defer func() {
		if delErr := kubeClient.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
			klog.Warningf("Failed to delete deny test namespace %s: %v", nsName, delErr)
		}
	}()

	podName := "np-deny-metrics"
	cleanup := createNetworkPolicyCurlPod(t, ctx, kubeClient, nsName, podName)
	defer cleanup()

	_, stderr, err := execCurlInPod(ctx, kubeClient, nsName, podName,
		[]string{"curl", "-sk", "--connect-timeout", "5", "--max-time", "5", deschedulerMetricsURL})
	assertNetworkPolicyBlocked(deschedulerMetricsPort, stderr, err)
	klog.Infof("Deny path from namespace %s verified (err=%v)", nsName, err)
}

// testOperandNetworkPolicyDenyWrongPorts verifies non-10258 ports to the descheduler pod are blocked from monitoring.
func testOperandNetworkPolicyDenyWrongPorts(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	ensureDeschedulerOperandReady(t, ctx, kubeClient)

	deschPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName, operatorclient.OperandName+"-operator")
	o.Expect(err).NotTo(o.HaveOccurred(), "descheduler operand pod should be running")
	o.Expect(deschPod.Status.PodIP).NotTo(o.BeEmpty(), "descheduler pod should have an IP")

	podName := "np-deny-ports"
	cleanup := createNetworkPolicyCurlPod(t, ctx, kubeClient, monitoringNamespace, podName)
	defer cleanup()

	// Probe non-metrics ports. NetworkPolicy drops should time out; "connection refused"
	// means the packet reached the pod (closed port), so the policy is not blocking.
	for _, port := range []int32{443, 8080, 8443, 10259} {
		url := fmt.Sprintf("https://%s:%d/", deschPod.Status.PodIP, port)
		_, stderr, curlErr := execCurlInPod(ctx, kubeClient, monitoringNamespace, podName,
			[]string{"curl", "-sk", "--connect-timeout", "3", "--max-time", "3", url})
		assertNetworkPolicyBlocked(port, stderr, curlErr)
		klog.Infof("Deny path for port %d verified (err=%v)", port, curlErr)
	}

	// Sanity: correct metrics port still allowed via pod IP (HTTP status proves L4 allow).
	metricsURL := fmt.Sprintf("https://%s:%d/metrics", deschPod.Status.PodIP, deschedulerMetricsPort)
	assertHTTPSEndpointReachable(ctx, kubeClient, monitoringNamespace, podName, metricsURL,
		fmt.Sprintf("port %d should remain allowed", deschedulerMetricsPort))
	klog.Infof("Wrong-port deny path verified")
}

func ensureDeschedulerOperandReady(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	_, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName, operatorclient.OperandName+"-operator")
	o.Expect(err).NotTo(o.HaveOccurred(), "descheduler operand should be running")

	o.Eventually(func(g o.Gomega) {
		_, getErr := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicyOperandName, metav1.GetOptions{})
		g.Expect(getErr).NotTo(o.HaveOccurred(), "operand NetworkPolicy should exist")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())
}

func createNetworkPolicyCurlPod(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, namespace, name string) func() {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "curl",
					Image:   networkPolicyCurlImage,
					Command: []string{"sleep", "3600"},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: utilpointer.Bool(false),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
						RunAsNonRoot: utilpointer.Bool(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: utilpointer.Bool(true),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
		},
	}

	_, err := kubeClient.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should create curl pod %s/%s", namespace, name)

	o.Eventually(func(g o.Gomega) {
		p, getErr := kubeClient.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		g.Expect(getErr).NotTo(o.HaveOccurred())
		g.Expect(p.Status.Phase).To(o.Equal(corev1.PodRunning), "curl pod should be Running")
	}).WithTimeout(3 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())

	return func() {
		if delErr := kubeClient.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
			klog.Warningf("Failed to delete curl pod %s/%s: %v", namespace, name, delErr)
		}
	}
}

func execCurlInPod(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, podName string, command []string) (string, string, error) {
	_ = kubeClient

	args := []string{"exec", "-n", namespace, podName, "--"}
	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "oc", args...)
	if _, err := exec.LookPath("oc"); err != nil {
		cmd = exec.CommandContext(ctx, "kubectl", args...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// assertHTTPSEndpointReachable checks that TCP/TLS to the URL succeeds.
// Metrics endpoints may return 401/403 without a bearer token; any HTTP status
// proves NetworkPolicy allowed the connection (unlike a connect failure).
func assertHTTPSEndpointReachable(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, podName, url, msg string) {
	stdout, stderr, err := execCurlInPod(ctx, kubeClient, namespace, podName,
		[]string{"curl", "-sk", "-o", "/tmp/np-curl-body", "-w", "%{http_code}",
			"--connect-timeout", "5", "--max-time", "10", url})
	o.Expect(err).NotTo(o.HaveOccurred(), "%s should connect: stderr=%s stdout=%s", msg, stderr, stdout)

	code := strings.TrimSpace(stdout)
	o.Expect(code).To(o.BeElementOf("200", "401", "403"),
		"%s: expected HTTP 200/401/403 proving L4 allow, got %q (stderr=%s)", msg, code, stderr)

	if code == "200" {
		body, _, catErr := execCurlInPod(ctx, kubeClient, namespace, podName, []string{"cat", "/tmp/np-curl-body"})
		o.Expect(catErr).NotTo(o.HaveOccurred(), "%s: should read response body", msg)
		o.Expect(body).To(o.ContainSubstring("# HELP"), "%s: expected Prometheus metrics body", msg)
	}
}

// assertNetworkPolicyBlocked requires a connect failure that is not "connection refused".
// Connection refused means the packet reached a closed port (policy allowed it).
func assertNetworkPolicyBlocked(port int32, stderr string, curlErr error) {
	o.Expect(curlErr).To(o.HaveOccurred(), "port %d should be denied/unreachable: stderr=%s", port, stderr)
	combined := strings.ToLower(stderr)
	if curlErr != nil {
		combined += " " + strings.ToLower(curlErr.Error())
	}
	o.Expect(combined).NotTo(o.ContainSubstring("connection refused"),
		"port %d: connection refused means traffic reached the pod; expected NetworkPolicy drop/timeout (stderr=%s)", port, stderr)
}

func hasNamespaceSelectorPeer(peers []networkingv1.NetworkPolicyPeer, matchLabels map[string]string) bool {
	for _, peer := range peers {
		if peer.NamespaceSelector == nil {
			continue
		}
		if mapsEqualStringString(peer.NamespaceSelector.MatchLabels, matchLabels) {
			return true
		}
	}
	return false
}

func mapsEqualStringString(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// testOperandNetworkPolicySelfHeal deletes the operand NetworkPolicy and asserts the operator recreates it.
func testOperandNetworkPolicySelfHeal(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	klog.Infof("Verifying operand NetworkPolicy self-heal after deletion")

	err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
		Delete(ctx, allowNetworkPolicyOperandName, metav1.DeleteOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should delete operand NetworkPolicy")

	o.Eventually(func(g o.Gomega) {
		_, getErr := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicyOperandName, metav1.GetOptions{})
		g.Expect(getErr).NotTo(o.HaveOccurred(), "operator should recreate operand NetworkPolicy")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())

	testOperandNetworkPolicy(t, ctx, kubeClient)
	klog.Infof("Operand NetworkPolicy self-heal verified successfully")
}

// testOperandNetworkPolicySelfHealOnModification patches the operand NetworkPolicy and asserts the operator restores it.
func testOperandNetworkPolicySelfHealOnModification(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	klog.Infof("Verifying operand NetworkPolicy self-heal after modification")

	policy, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
		Get(ctx, allowNetworkPolicyOperandName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "operand NetworkPolicy should exist before patch")

	patched := policy.DeepCopy()
	patched.Spec.Egress = nil
	_, err = kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
		Update(ctx, patched, metav1.UpdateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should patch operand NetworkPolicy egress")

	o.Eventually(func(g o.Gomega) {
		restored, getErr := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicyOperandName, metav1.GetOptions{})
		g.Expect(getErr).NotTo(o.HaveOccurred())
		g.Expect(restored.Spec.Egress).To(o.HaveLen(1), "operator should restore allow-all egress")
		g.Expect(restored.Spec.Egress[0].To).To(o.BeEmpty())
		g.Expect(restored.Spec.Egress[0].Ports).To(o.BeEmpty())
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())

	klog.Infof("Operand NetworkPolicy modification self-heal verified successfully")
}

// testSoftTainterNetworkPolicyConfigured verifies the softtainter NetworkPolicy exists with the
// expected podSelector, egress allow-all, and health ingress on TCP 6060 from any source.
func testSoftTainterNetworkPolicyConfigured(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	klog.Infof("Verifying softtainter NetworkPolicy %q", allowNetworkPolicySoftTainterName)

	var policy *networkingv1.NetworkPolicy
	o.Eventually(func(g o.Gomega) {
		var err error
		policy, err = kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicySoftTainterName, metav1.GetOptions{})
		g.Expect(err).NotTo(o.HaveOccurred(), "softtainter NetworkPolicy should exist")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())

	o.Expect(policy.Spec.PodSelector.MatchLabels).To(o.HaveKeyWithValue("app", softTainterAppLabel))
	o.Expect(policy.Spec.PolicyTypes).To(o.ContainElements(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress))
	o.Expect(policy.Spec.Egress).To(o.HaveLen(1), "softtainter NetworkPolicy should allow all egress")
	o.Expect(policy.Spec.Egress[0].To).To(o.BeEmpty())
	o.Expect(policy.Spec.Egress[0].Ports).To(o.BeEmpty())

	o.Expect(policy.Spec.Ingress).To(o.HaveLen(1), "softtainter NetworkPolicy should have exactly one ingress rule")
	rule := policy.Spec.Ingress[0]
	o.Expect(rule.From).To(o.BeEmpty(), "softtainter health ingress should not restrict sources")
	o.Expect(rule.Ports).To(o.HaveLen(1), "softtainter ingress should allow a single port")
	o.Expect(rule.Ports[0].Protocol).NotTo(o.BeNil())
	o.Expect(*rule.Ports[0].Protocol).To(o.Equal(corev1.ProtocolTCP), "softtainter health ingress must be TCP")
	o.Expect(rule.Ports[0].Port).NotTo(o.BeNil())
	o.Expect(rule.Ports[0].Port.IntVal).To(o.Equal(softTainterHealthPort))

	o.Expect(policy.OwnerReferences).NotTo(o.BeEmpty(), "softtainter NetworkPolicy should have owner reference")
	o.Expect(policy.OwnerReferences[0].Kind).To(o.Equal("KubeDescheduler"))
	o.Expect(policy.OwnerReferences[0].Name).To(o.Equal(operatorclient.OperatorConfigName))

	klog.Infof("Softtainter NetworkPolicy configured verified successfully")
}

// testSoftTainterNetworkPolicyAllowHealth verifies TCP 6060 /livez is reachable from a non-operator namespace.
func testSoftTainterNetworkPolicyAllowHealth(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	ensureSoftTainterOperandReady(t, ctx, kubeClient)

	stPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.SoftTainterOperandName, "")
	o.Expect(err).NotTo(o.HaveOccurred(), "softtainter pod should be running")
	o.Expect(stPod.Status.PodIP).NotTo(o.BeEmpty(), "softtainter pod should have an IP")

	nsName := fmt.Sprintf("np-st-allow-%d", time.Now().Unix())
	_, err = kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce": "baseline",
			},
		},
	}, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should create softtainter allow test namespace")
	defer func() {
		if delErr := kubeClient.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
			klog.Warningf("Failed to delete softtainter allow test namespace %s: %v", nsName, delErr)
		}
	}()

	podName := "np-st-allow-health"
	cleanup := createNetworkPolicyCurlPod(t, ctx, kubeClient, nsName, podName)
	defer cleanup()

	healthURL := fmt.Sprintf("http://%s:%d/livez", stPod.Status.PodIP, softTainterHealthPort)
	stdout, stderr, err := execCurlInPod(ctx, kubeClient, nsName, podName,
		[]string{"curl", "-s", "--connect-timeout", "5", "--max-time", "10", healthURL})
	o.Expect(err).NotTo(o.HaveOccurred(), "softtainter health from any namespace should succeed: stderr=%s stdout=%s", stderr, stdout)
	klog.Infof("Softtainter allow health path from %s verified", nsName)
}

// testSoftTainterNetworkPolicyDenyWrongPorts verifies non-6060 ports to the softtainter pod are blocked.
func testSoftTainterNetworkPolicyDenyWrongPorts(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	ensureSoftTainterOperandReady(t, ctx, kubeClient)

	stPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.SoftTainterOperandName, "")
	o.Expect(err).NotTo(o.HaveOccurred(), "softtainter pod should be running")
	o.Expect(stPod.Status.PodIP).NotTo(o.BeEmpty(), "softtainter pod should have an IP")

	nsName := fmt.Sprintf("np-st-deny-%d", time.Now().Unix())
	_, err = kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce": "baseline",
			},
		},
	}, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should create softtainter deny test namespace")
	defer func() {
		if delErr := kubeClient.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
			klog.Warningf("Failed to delete softtainter deny test namespace %s: %v", nsName, delErr)
		}
	}()

	podName := "np-st-deny-ports"
	cleanup := createNetworkPolicyCurlPod(t, ctx, kubeClient, nsName, podName)
	defer cleanup()

	for _, port := range []int32{443, 8080, 8443, 10258} {
		url := fmt.Sprintf("http://%s:%d/", stPod.Status.PodIP, port)
		_, stderr, curlErr := execCurlInPod(ctx, kubeClient, nsName, podName,
			[]string{"curl", "-s", "--connect-timeout", "3", "--max-time", "3", url})
		assertNetworkPolicyBlocked(port, stderr, curlErr)
		klog.Infof("Softtainter deny path for port %d verified (err=%v)", port, curlErr)
	}

	// Sanity: health port still allowed.
	healthURL := fmt.Sprintf("http://%s:%d/livez", stPod.Status.PodIP, softTainterHealthPort)
	_, stderr, err := execCurlInPod(ctx, kubeClient, nsName, podName,
		[]string{"curl", "-s", "--connect-timeout", "5", "--max-time", "10", healthURL})
	o.Expect(err).NotTo(o.HaveOccurred(), "softtainter port %d should remain allowed: stderr=%s", softTainterHealthPort, stderr)
	klog.Infof("Softtainter wrong-port deny path verified")
}

func ensureSoftTainterOperandReady(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	_, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.SoftTainterOperandName, "")
	o.Expect(err).NotTo(o.HaveOccurred(), "softtainter operand should be running")

	o.Eventually(func(g o.Gomega) {
		_, getErr := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicySoftTainterName, metav1.GetOptions{})
		g.Expect(getErr).NotTo(o.HaveOccurred(), "softtainter NetworkPolicy should exist")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())
}

// testSoftTainterNetworkPolicySelfHeal deletes the softtainter NetworkPolicy and asserts the operator recreates it.
func testSoftTainterNetworkPolicySelfHeal(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	klog.Infof("Verifying softtainter NetworkPolicy self-heal after deletion")
	ensureSoftTainterOperandReady(t, ctx, kubeClient)

	err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
		Delete(ctx, allowNetworkPolicySoftTainterName, metav1.DeleteOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should delete softtainter NetworkPolicy")

	o.Eventually(func(g o.Gomega) {
		_, getErr := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicySoftTainterName, metav1.GetOptions{})
		g.Expect(getErr).NotTo(o.HaveOccurred(), "operator should recreate softtainter NetworkPolicy")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())

	testSoftTainterNetworkPolicyConfigured(t, ctx, kubeClient)
	klog.Infof("Softtainter NetworkPolicy self-heal verified successfully")
}

// testSoftTainterNetworkPolicySelfHealOnModification patches the softtainter NetworkPolicy and asserts the operator restores it.
func testSoftTainterNetworkPolicySelfHealOnModification(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	klog.Infof("Verifying softtainter NetworkPolicy self-heal after modification")
	ensureSoftTainterOperandReady(t, ctx, kubeClient)

	policy, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
		Get(ctx, allowNetworkPolicySoftTainterName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "softtainter NetworkPolicy should exist before patch")

	patched := policy.DeepCopy()
	patched.Spec.Egress = nil
	_, err = kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
		Update(ctx, patched, metav1.UpdateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "should patch softtainter NetworkPolicy egress")

	o.Eventually(func(g o.Gomega) {
		restored, getErr := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicySoftTainterName, metav1.GetOptions{})
		g.Expect(getErr).NotTo(o.HaveOccurred())
		g.Expect(restored.Spec.Egress).To(o.HaveLen(1), "operator should restore allow-all egress")
		g.Expect(restored.Spec.Egress[0].To).To(o.BeEmpty())
		g.Expect(restored.Spec.Egress[0].Ports).To(o.BeEmpty())
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())

	klog.Infof("Softtainter NetworkPolicy modification self-heal verified successfully")
}

// testSoftTainterNetworkPolicy verifies softtainter NetworkPolicy lifecycle with the KubeVirt profile.
func testSoftTainterNetworkPolicy(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	deschClient := GetDeschedulerClient()

	// Softtainter NetworkPolicy must be absent with the base profile.
	o.Eventually(func(g o.Gomega) {
		_, err := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicySoftTainterName, metav1.GetOptions{})
		g.Expect(apierrors.IsNotFound(err)).To(o.BeTrue(), "softtainter NetworkPolicy should be absent before enable")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())

	cleanup := setupSoftTainterController(ctx, t, kubeClient, deschClient)
	defer cleanup()

	testSoftTainterNetworkPolicyConfigured(t, ctx, kubeClient)

	// Revert to base profile and assert softtainter NetworkPolicy is deleted.
	err := operatorConfigsAppliers[baseConf](ctx, deschClient)
	o.Expect(err).NotTo(o.HaveOccurred(), "should restore base profile")

	o.Eventually(func(g o.Gomega) {
		_, getErr := kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
			Get(ctx, allowNetworkPolicySoftTainterName, metav1.GetOptions{})
		g.Expect(apierrors.IsNotFound(getErr)).To(o.BeTrue(), "softtainter NetworkPolicy should be deleted when softtainter is disabled")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.Succeed())

	// Operand NetworkPolicy must remain.
	_, err = kubeClient.NetworkingV1().NetworkPolicies(operatorclient.OperatorNamespace).
		Get(ctx, allowNetworkPolicyOperandName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "operand NetworkPolicy should remain after softtainter disable")

	klog.Infof("Softtainter NetworkPolicy lifecycle verified successfully")
}

// testPrometheusTarget tests that the Prometheus target is up and running.
// This function works with both standard Go testing and Ginkgo.
func testPrometheusTarget(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
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

	testPrometheusTargetUp(t, ctx, kubeClient, serviceMonitor.Name, metricsService.Name, metricsService.Labels, metricsPort, podLabels)
}

// testMetricsData tests that metrics data is available and has expected content.
// This function works with both standard Go testing and Ginkgo.
func testMetricsData(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	deployment := getDeschedulerDeployment()

	// Extract pod labels from deployment spec and validate there's exactly one label selector
	podLabels := deployment.Spec.Template.Labels
	if len(podLabels) != 1 {
		t.Fatalf("Expected exactly one label selector for descheduler pods, got %d", len(podLabels))
	}

	testMetricsDataAvailable(t, ctx, kubeClient, podLabels)
}

// cleanupTestNamespace deletes the test namespace.
// This function works with both standard Go testing and Ginkgo.
func cleanupTestNamespace(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, testNamespace string) {
	if testNamespace == "" {
		return
	}
	klog.Infof("Cleaning up test namespace: %s", testNamespace)
	if err := kubeClient.CoreV1().Namespaces().Delete(ctx, testNamespace, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to delete namespace %s: %v", testNamespace, err)
	}
	o.Eventually(func() bool {
		_, err := kubeClient.CoreV1().Namespaces().Get(ctx, testNamespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true
		}
		return false
	}, time.Minute, 1*time.Second).Should(o.BeTrue(), "namespace not deleted after timeout")
}

func waitForPodRunningByNamePrefix(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, nameprefix, excludedprefix string) (*v1.Pod, error) {
	var expectedPod *corev1.Pod
	// Wait until the expected pod is running
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
			klog.Infof("Checking pod: %v, phase: %v, deletionTS: %v\n", pod.Name, pod.Status.Phase, pod.GetDeletionTimestamp())
			if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
				expectedPod = pod.DeepCopy()
				return true
			}
		}
		return false
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(o.BeTrue())
	return expectedPod, nil
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

func waitForPodsRunning(ctx context.Context, t testing.TB, clientSet *k8sclient.Clientset, labelMap map[string]string, desireRunningPodNum int, namespace string) {
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
	obj, err = kubeClient.RbacV1().Roles(namespace).Get(ctx, softTainterRoleName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.RbacV1().RoleBindings(namespace).Get(ctx, softTainterRoleBindingName, metav1.GetOptions{})
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
	obj, err = kubeClient.NetworkingV1().NetworkPolicies(namespace).Get(ctx, allowNetworkPolicySoftTainterName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}

	return nil

}

func checkDeschedulerOperandObjects(ctx context.Context, kubeClient *k8sclient.Clientset, namespace string, expected bool) error {
	var obj runtime.Object
	var err error

	obj, err = kubeClient.CoreV1().ServiceAccounts(namespace).Get(ctx, deschedulerOperandServiceAccountName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.RbacV1().ClusterRoles().Get(ctx, deschedulerOperandClusterRoleName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, deschedulerOperandClusterRoleBindingName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.RbacV1().Roles(namespace).Get(ctx, deschedulerOperandRoleName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.RbacV1().RoleBindings(namespace).Get(ctx, deschedulerOperandRoleBindingName, metav1.GetOptions{})
	if cerr := checkExpected(expected, obj, err); cerr != nil {
		return cerr
	}
	obj, err = kubeClient.NetworkingV1().NetworkPolicies(namespace).Get(ctx, allowNetworkPolicyOperandName, metav1.GetOptions{})
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

func tryUpdatingTaintWithExpectation(ctx context.Context, t testing.TB, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint, add, expectedSuccess bool) {
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

func tryAddingTaintWithExpectedSuccess(ctx context.Context, t testing.TB, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint) {
	tryUpdatingTaintWithExpectation(ctx, t, clientSet, node, taint, true, true)
}

func tryAddingTaintWithExpectedFailure(ctx context.Context, t testing.TB, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint) {
	tryUpdatingTaintWithExpectation(ctx, t, clientSet, node, taint, true, false)
}

func tryRemovingTaintWithExpectedSuccess(ctx context.Context, t testing.TB, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint) {
	tryUpdatingTaintWithExpectation(ctx, t, clientSet, node, taint, false, true)
}

func tryRemovingTaintWithExpectedFailure(ctx context.Context, t testing.TB, clientSet *k8sclient.Clientset, node *corev1.Node, taint *corev1.Taint) {
	tryUpdatingTaintWithExpectation(ctx, t, clientSet, node, taint, false, false)
}

func applyKubeVirtNodeLabel(ctx context.Context, kubeClient *k8sclient.Clientset) error {
	nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: workersLabelSelector})
	if err != nil {
		return err
	}
	for _, node := range nodes.Items {
		if node.Labels[kubeVirtLabelKey] != kubeVirtLabelValue {
			node.Labels[kubeVirtLabelKey] = kubeVirtLabelValue
			uerr := updateNodeAndRetryOnConflicts(ctx, kubeClient, &node, metav1.UpdateOptions{})
			if uerr != nil {
				return uerr
			}
		}
	}
	return nil
}

func dropKubeVirtNodeLabel(ctx context.Context, kubeClient *k8sclient.Clientset) error {
	nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: workersLabelSelector})
	if err != nil {
		return err
	}
	for _, node := range nodes.Items {
		if node.Labels[kubeVirtLabelKey] == kubeVirtLabelValue {
			delete(node.Labels, kubeVirtLabelKey)
			uerr := updateNodeAndRetryOnConflicts(ctx, kubeClient, &node, metav1.UpdateOptions{})
			if uerr != nil {
				return uerr
			}
		}
	}
	return nil
}

func updateNodeAndRetryOnConflicts(ctx context.Context, kubeClient *k8sclient.Clientset, node *corev1.Node, opts metav1.UpdateOptions) error {
	uNode, uerr := kubeClient.CoreV1().Nodes().Update(ctx, node, opts)
	if uerr != nil {
		if apierrors.IsConflict(uerr) {
			if uNode.Name == "" {
				uNode, uerr = kubeClient.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
				if uerr != nil {
					return uerr
				}
			}
			node.Spec.DeepCopyInto(&uNode.Spec)
			uNode.ObjectMeta.Labels = node.ObjectMeta.Labels
			uNode.ObjectMeta.Annotations = node.ObjectMeta.Annotations
			_, err := kubeClient.CoreV1().Nodes().Update(ctx, uNode, opts)
			return err
		}
		return uerr
	}
	return nil
}

func updateDeploymentAndRetryOnConflicts(ctx context.Context, kubeClient *k8sclient.Clientset, deployment *appsv1.Deployment, opts metav1.UpdateOptions) error {
	uDeployment, uerr := kubeClient.AppsV1().Deployments(deployment.Namespace).Update(ctx, deployment, opts)
	if uerr != nil {
		if apierrors.IsConflict(uerr) {
			if uDeployment.Name == "" {
				uDeployment, uerr = kubeClient.AppsV1().Deployments(uDeployment.Namespace).Get(ctx, uDeployment.Name, metav1.GetOptions{})
				if uerr != nil {
					return uerr
				}
			}
			deployment.Spec.DeepCopyInto(&uDeployment.Spec)
			uDeployment.ObjectMeta.Labels = deployment.ObjectMeta.Labels
			uDeployment.ObjectMeta.Annotations = deployment.ObjectMeta.Annotations
			_, err := kubeClient.AppsV1().Deployments(deployment.Namespace).Update(ctx, deployment, opts)
			return err
		}
		return uerr
	}
	return nil
}

func mockPSIEnv(ctx context.Context, kubeClient *k8sclient.Clientset) error {
	operatorDeployment, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName+"-operator", metav1.GetOptions{})
	if err != nil {
		return err
	}
	operatorDeployment.Spec.Template.Spec.Containers[0].Env = append(
		operatorDeployment.Spec.Template.Spec.Containers[0].Env,
		v1.EnvVar{
			Name:  EXPERIMENTAL_DISABLE_PSI_CHECK,
			Value: "true",
		})
	return updateDeploymentAndRetryOnConflicts(ctx, kubeClient, operatorDeployment, metav1.UpdateOptions{})
}

func unmockPSIEnv(ctx context.Context, kubeClient *k8sclient.Clientset, prevDisablePSIcheck string, foundDisablePSIcheck bool) error {
	operatorDeployment, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName+"-operator", metav1.GetOptions{})
	if err != nil {
		return err
	}
	var envVars []v1.EnvVar
	for _, e := range operatorDeployment.Spec.Template.Spec.Containers[0].Env {
		if e.Name != EXPERIMENTAL_DISABLE_PSI_CHECK {
			envVars = append(envVars, e)
		}
	}
	if foundDisablePSIcheck {
		operatorDeployment.Spec.Template.Spec.Containers[0].Env = append(
			envVars,
			v1.EnvVar{
				Name:  EXPERIMENTAL_DISABLE_PSI_CHECK,
				Value: prevDisablePSIcheck,
			})
	}
	operatorDeployment.Spec.Template.Spec.Containers[0].Env = envVars
	return updateDeploymentAndRetryOnConflicts(ctx, kubeClient, operatorDeployment, metav1.UpdateOptions{})
}

// setupSoftTainterController sets up the common prerequisites for soft tainter tests.
// It collects cleanup functions as it progresses and runs them if any step fails.
// Returns a cleanup function that should be deferred by the caller.
func setupSoftTainterController(ctx context.Context, t testing.TB, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) func() {
	var cleanups []func()

	// Helper to run all cleanups in reverse order (LIFO, like defer)
	runCleanups := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	// label all the nodes to mock a KubeVirt deployment
	if err := applyKubeVirtNodeLabel(ctx, kubeClient); err != nil {
		t.Fatalf("Failed applying KubeVirt node label: %v", err)
	}
	cleanups = append(cleanups, func() {
		if err := dropKubeVirtNodeLabel(ctx, kubeClient); err != nil {
			t.Fatalf("Failed reverting KubeVirt node label: %v", err)
		}
	})

	// patch the operator deployment to mock PSI
	prevDisablePSIcheck, foundDisablePSIcheck := os.LookupEnv(EXPERIMENTAL_DISABLE_PSI_CHECK)
	if err := mockPSIEnv(ctx, kubeClient); err != nil {
		runCleanups()
		t.Fatalf("Failed mocking PSI path enviromental variable to the operator depoyment: %v", err)
	}
	cleanups = append(cleanups, func() {
		if err := unmockPSIEnv(ctx, kubeClient, prevDisablePSIcheck, foundDisablePSIcheck); err != nil {
			t.Fatalf("Failed PSI path enviromental variable: %v", err)
		}
	})

	// wait for descheduler operator pod to be running
	deschOpPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName, operatorclient.OperandName+"-operator")
	if err != nil {
		runCleanups()
		t.Fatalf("Unable to wait for the Descheduler operator pod to run")
	}
	klog.Infof("Descheduler pod running in %v", deschOpPod.Name)

	// Capture the existing CR Spec so cleanup restores the caller's configuration
	// instead of always overwriting with the base asset.
	var previousSpec *descv1.KubeDeschedulerSpec
	existingDesch, getErr := deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	if getErr == nil {
		previousSpec = existingDesch.Spec.DeepCopy()
	} else if !apierrors.IsNotFound(getErr) {
		runCleanups()
		t.Fatalf("Failed to get existing KubeDescheduler before softtainter setup: %v", getErr)
	}

	// apply devKubeVirtRelieveAndMigrate CR for the operator
	if err := operatorConfigsAppliers[kubeVirtRelieveAndMigrateConf](ctx, deschClient); err != nil {
		runCleanups()
		t.Fatalf("Unable to apply a CR for Descheduler operator: %v", err)
	}
	klog.Infof("Descheduler operator is now configured with devKubeVirtRelieveAndMigrate profile")
	cleanups = append(cleanups, func() {
		current, err := deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get KubeDescheduler during softtainter cleanup: %v", err)
		}
		if previousSpec != nil {
			previousSpec.DeepCopyInto(&current.Spec)
			if _, err := deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Update(ctx, current, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("Failed restoring prior KubeDescheduler Spec: %v", err)
			}
			return
		}
		if err := operatorConfigsAppliers[baseConf](ctx, deschClient); err != nil {
			t.Fatalf("Failed restoring base profile: %v", err)
		}
	})

	// wait for softtainter pod to be running
	stPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.SoftTainterOperandName, "")
	if err != nil {
		runCleanups()
		t.Fatalf("Unable to wait for the softtainter pod to run")
	}
	klog.Infof("SoftTainter pod running in %v", stPod.Name)

	// Return cleanup function that runs all cleanups in reverse order
	return runCleanups
}

// testMetricsServiceExists verifies that the metrics service exists
func testMetricsServiceExists(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, serviceName string, expectedLabels map[string]string, expectedPort corev1.ServicePort) {
	klog.Infof("Verifying metrics service exists")

	// Get the actual service from the cluster
	service, err := kubeClient.CoreV1().Services(operatorclient.OperatorNamespace).Get(ctx, serviceName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "metrics service should exist")

	// Verify service has the correct labels
	for key, value := range expectedLabels {
		o.Expect(service.Labels).To(o.HaveKeyWithValue(key, value), "service should have label %s=%s", key, value)
	}

	// Verify service exposes the expected port
	o.Expect(service.Spec.Ports).NotTo(o.BeEmpty(), "service should have at least one port")
	found := false
	for _, actualPort := range service.Spec.Ports {
		if actualPort.Name == expectedPort.Name && actualPort.Port == expectedPort.Port {
			found = true
			break
		}
	}
	o.Expect(found).To(o.BeTrue(), "service should expose port %s on port %d", expectedPort.Name, expectedPort.Port)

	klog.Infof("Metrics service verified successfully")
}

// testServiceMonitorExists verifies that the ServiceMonitor exists and its selector matches the service labels
func testServiceMonitorExists(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, serviceMonitorName string, expectedServiceLabels map[string]string) {
	klog.Infof("Verifying ServiceMonitor exists")

	// Get monitoring client to access ServiceMonitor (CRD from prometheus-operator)
	monitoringClient := GetMonitoringClient()

	// Get the ServiceMonitor
	serviceMonitor, err := monitoringClient.MonitoringV1().ServiceMonitors(operatorclient.OperatorNamespace).
		Get(ctx, serviceMonitorName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "ServiceMonitor should exist")

	// Verify ServiceMonitor has the correct spec
	o.Expect(serviceMonitor.Spec.Selector).NotTo(o.BeNil(), "ServiceMonitor should have selector")
	o.Expect(serviceMonitor.Spec.Selector.MatchLabels).NotTo(o.BeNil(), "selector should have matchLabels")

	// Verify ServiceMonitor selector matches the service labels
	for key, value := range expectedServiceLabels {
		o.Expect(serviceMonitor.Spec.Selector.MatchLabels).To(o.HaveKeyWithValue(key, value), "ServiceMonitor selector should match service label %s=%s", key, value)
	}

	klog.Infof("ServiceMonitor verified successfully")
}

// testPrometheusTargetUp verifies that the Prometheus target is up and scraping metrics
func testPrometheusTargetUp(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, serviceMonitorName string, metricsServiceName string, serviceLabels map[string]string, metricsPort corev1.ServicePort, podLabels map[string]string) {
	klog.Infof("Verifying Prometheus target is up")

	// Get the Prometheus token for authentication
	token, err := getPrometheusToken(ctx, kubeClient, podLabels)
	o.Expect(err).NotTo(o.HaveOccurred(), "should get Prometheus token")

	// Get the Route client
	routeClient := GetRouteClient()

	// Query Prometheus targets API
	klog.Infof("Querying Prometheus for descheduler target status")

	o.Eventually(func() bool {
		// First, get all EndpointSlices matching the service labels
		endpointAddresses, err := getEndpointAddressesForService(ctx, kubeClient, operatorclient.OperatorNamespace, serviceLabels)
		if err != nil {
			klog.Errorf("Failed to get endpoint addresses: %v", err)
			return false
		}

		if len(endpointAddresses) == 0 {
			klog.Infof("No endpoints found for service yet, waiting...")
			return false
		}

		klog.Infof("Found %d endpoint(s) for service", len(endpointAddresses))

		// Query each endpoint individually
		allEndpointsHealthy := true
		for _, endpointIP := range endpointAddresses {
			// Construct the instance string as "IP:port"
			instance := fmt.Sprintf("%s:%d", endpointIP, metricsPort.TargetPort.IntVal)

			klog.Infof("Querying Prometheus for instance: %s", instance)

			result, err := queryPrometheusTarget(ctx, kubeClient, routeClient, token, serviceMonitorName, instance)
			if err != nil {
				klog.Errorf("Failed to query Prometheus target for instance %s: %v", instance, err)
				allEndpointsHealthy = false
				continue
			}

			// Check if we got any results (target exists)
			if len(result.Data.Result) == 0 {
				klog.Infof("Target for instance %s not found yet in Prometheus, waiting...", instance)
				allEndpointsHealthy = false
				continue
			}

			// The 'up' metric returns 1 if target is healthy, 0 if down
			series := result.Data.Result[0]
			if len(series.Value) < 2 {
				klog.Warningf("Instance %s: unexpected metric value format: %v", instance, series.Value)
				allEndpointsHealthy = false
				continue
			}

			valueStr, ok := series.Value[1].(string)
			if !ok {
				klog.Warningf("Instance %s: metric value is not a string: %v", instance, series.Value[1])
				allEndpointsHealthy = false
				continue
			}

			klog.Infof("Instance %s: up=%s, labels=%v", instance, valueStr, series.Metric)

			if valueStr != "1" {
				klog.Warningf("Instance %s is not up yet", instance)
				allEndpointsHealthy = false
			} else {
				klog.Infof("Instance %s is healthy", instance)
			}
		}

		if allEndpointsHealthy {
			klog.Infof("All endpoints have healthy Prometheus targets")
			return true
		}

		klog.Warningf("Not all endpoints are healthy yet, waiting...")
		return false
	}, 5*time.Minute, 10*time.Second).Should(o.BeTrue(), "Prometheus target should be up")

	klog.Infof("Prometheus target verified successfully")
}

// testMetricsDataAvailable verifies that specific metrics are available and have data
func testMetricsDataAvailable(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, podLabels map[string]string) {
	klog.Infof("Verifying metrics data is available")

	// Get the Prometheus token for authentication
	token, err := getPrometheusToken(ctx, kubeClient, podLabels)
	o.Expect(err).NotTo(o.HaveOccurred(), "should get Prometheus token")

	// Get the Route client
	routeClient := GetRouteClient()

	// Query for the specific metric
	metricQuery := `descheduler_descheduler_loop_duration_seconds_bucket`

	klog.Infof("Querying Prometheus for metric: %s", metricQuery)

	o.Eventually(func() bool {
		result, err := queryPrometheusMetric(ctx, kubeClient, routeClient, token, metricQuery)
		if err != nil {
			klog.Errorf("Failed to query Prometheus metric: %v", err)
			return false
		}

		// Check if we have any results
		if len(result.Data.Result) == 0 {
			klog.Infof("No results found for metric %s yet, waiting...", metricQuery)
			return false
		}

		// Verify we have data
		klog.Infof("Found %d metric series for %s", len(result.Data.Result), metricQuery)
		for i, series := range result.Data.Result {
			if len(series.Value) > 0 {
				klog.Infof("Series %d: metric=%v, value=%v", i, series.Metric, series.Value)
			}
		}

		return true
	}, 5*time.Minute, 10*time.Second).Should(o.BeTrue(), "Metric data should be available")

	klog.Infof("Metrics data verified successfully")
}
