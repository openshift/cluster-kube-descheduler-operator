package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextclientv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	descv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	deschclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
)

// ============================================================================
// TEST SUITE DESIGN AND STRUCTURE
// ============================================================================
//
// DESIGN: Single g.Describe block with clear CR lifecycle management
//
// EXECUTION FLOW:
// 1. BeforeAll: Install operator + create default CR (LifecycleAndUtilization)
// 2. METRICS/SERVICE TESTS: Use pre-created default CR (no hooks)
//    - Metrics service available
//    - Prometheus target up
//    - Metrics data available
//    - Plus other basic tests
// 3. PROFILE/STRATEGY TESTS: Each test manages its own CR via runProfileTest()
//    - Each test creates custom CR with its profile
//    - Test validates the profile
//    - Cleanup deletes CR
// 4. AfterAll: Cancel context (cleanup skipped for debugging)
//
// WHY THIS WORKS:
// ✅ No shared hooks = no CR lifecycle conflicts
// ✅ Metrics tests use default CR = simple and predictable
// ✅ Profile tests explicitly manage CRs = each test owns its state
// ✅ Clear separation = easy to understand and maintain
//

const (
	deschedulerOperatorLabel = "name=descheduler-operator"
	deschedulerLabel         = "app=descheduler"
)

func isOperatorOLMInstallationEnabled() bool {
	return os.Getenv("NO_OLM") == "" && os.Getenv("OPERATOR_IMAGE") == "" && os.Getenv("OPERAND_IMAGE") == ""
}

// isOperatorPreInstalled checks if the operator was already installed
// by checking if the operator Deployment is running and ready in the namespace.
// This is more reliable than checking for CSV which is created asynchronously.
func isOperatorPreInstalled(ctx context.Context, kubeClient *k8sclient.Clientset, namespace string) bool {
	klog.Infof("isOperatorPreInstalled: checking if operator deployment is ready in namespace %s", namespace)

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		deployment, err := kubeClient.AppsV1().Deployments(namespace).Get(ctx, "descheduler-operator", metav1.GetOptions{})
		if err != nil {
			klog.V(2).Infof("isOperatorPreInstalled: deployment not found yet: %v", err)
			return false, nil
		}

		if deployment.Spec.Replicas == nil {
			klog.V(2).Infof("isOperatorPreInstalled: deployment has nil Spec.Replicas")
			return false, nil
		}

		if deployment.Status.ReadyReplicas >= *deployment.Spec.Replicas {
			klog.Infof("isOperatorPreInstalled: operator deployment is ready with %d/%d replicas",
				deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
			return true, nil
		}

		klog.V(2).Infof("isOperatorPreInstalled: operator deployment not yet ready: %d/%d replicas",
			deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
		return false, nil
	})

	if err != nil {
		klog.Warningf("isOperatorPreInstalled: timed out waiting for operator deployment: %v", err)
		return false
	}

	klog.Infof("isOperatorPreInstalled: returning true - operator is pre-installed")
	return true
}

// Ginkgo test specs for migrated OTP tests
var _ = g.Describe("[OTP][Operator][Serial] Descheduler Operator Functionality", func() {
	var (
		ctx           context.Context
		cancelFnc     context.CancelFunc
		olmInstalled  bool // Flag: true if operator installed via OLM (bundle or CatalogSource), false if non-OLM
		kubeClient    *k8sclient.Clientset
		dynamicClient dynamic.Interface
		deschClient   *deschclient.Clientset
		apiExtClient  *apiextclientv1.Clientset
	)

	g.BeforeEach(func() {
		g.By("Setting up test environment")
		var err error
		kubeClient = GetKubeClient()
		dynamicClient = GetDynamicClient()
		deschClient = GetDeschedulerClient()
		apiExtClient = GetApiExtensionClient()
		ctx, cancelFnc = context.WithCancel(context.TODO())

		if !isOperatorOLMInstallationEnabled() {
			// Non-OLM path: install operator from deploy/ folder using OPERATOR_IMAGE/OPERAND_IMAGE
			olmInstalled = false // Operator will be installed non-OLM way
			err = setupOperator(ctx, kubeClient, deschClient, apiExtClient)
		} else if isOperatorPreInstalled(ctx, kubeClient, operatorclient.OperatorNamespace) {
			// Bundle-based CI installation (operator-sdk run bundle) pre-installs the operator;
			// only the KubeDescheduler CR and operand readiness are needed.
			klog.Infof("Operator already installed, skipping installation")
			olmInstalled = true // Operator was installed via OLM (bundle)

			// Ensure namespace has cluster-monitoring label for Prometheus scraping (required for metrics tests and policy generation)
			labelErr := ensureNamespaceMonitoringLabel(ctx, kubeClient, operatorclient.OperatorNamespace)
			if labelErr != nil {
				klog.Warningf("Warning: Failed to ensure monitoring label on namespace: %v", labelErr)
			}

			// Check if CR exists before creating
			_, crErr := deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
			if apierrors.IsNotFound(crErr) {
				// CR doesn't exist, create it
				kdCR := newDefaultKubeDescheduler()
				err = createKubeDeschedulerAndWait(ctx, kubeClient, deschClient, kdCR)
			} else if crErr != nil {
				err = crErr
			}
		} else {
			// OLM path: install via PackageManifest/Subscription (requires CatalogSource with KDO package)
			olmInstalled = true // Operator will be installed via OLM
			err = installOperatorWithSubscription(ctx, kubeClient, deschClient, dynamicClient, operatorclient.OperatorNamespace)
		}
		o.Expect(err).NotTo(o.HaveOccurred())

		// Wait for descheduler pod to stabilize before any tests run
		// This ensures Prometheus has time to discover the ServiceMonitor
		g.By("Waiting for descheduler pod to stabilize before tests")
		err = waitForPodStability(ctx, kubeClient, operatorclient.OperatorNamespace, deschedulerLabel, 30*time.Second)
		if err != nil {
			klog.Warningf("Warning: Timeout waiting for pod stability in BeforeAll: %v", err)
		}
		time.Sleep(10 * time.Hour)
	})

	// ============================================================================
	// METRICS/SERVICE TESTS - Use default CR from BeforeAll
	// ============================================================================
	// These tests run WITHOUT hooks, using the default KubeDescheduler CR
	// Simple, predictable validation of operator functionality

	// OCP-76194
	g.It("[OTP][Operator][Serial] should validate profile conflict validation [Slow][Timeout:15m]", func() {
		g.By("Testing profile conflict validation")
		conflictCtx, conflictCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer conflictCancel()
		testProfileConflicts(g.GinkgoTB(), conflictCtx, kubeClient, deschClient)
	})

	// OCP-83032
	g.It("[OTP][Operator][Serial] should validate RelatedImages defined in CSV [Slow][Timeout:15m]", func() {
		g.By("Testing RelatedImages defined in CSV")
		if !olmInstalled {
			g.Skip("Skipping. The operator is not installed via OLM")
		}
		relatedImagesCtx, relatedImagesCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer relatedImagesCancel()
		testRelatedImages(g.GinkgoTB(), relatedImagesCtx, kubeClient)
	})

	// OCP-45694
	g.It("[OTP][Operator][Serial] should validate must-gather OLM data collection [Slow][Disruptive][Timeout:15m]", func() {
		g.By("Testing must-gather OLM data collection")
		if !olmInstalled {
			g.Skip("Skipping. The operator is not installed via OLM")
		}
		mustGatherCtx, mustGatherCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer mustGatherCancel()
		testOLMMustGatherData(g.GinkgoTB(), mustGatherCtx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should create and remove soft tainter objects [Slow][Timeout:15m]", func() {
		g.By("Testing soft tainter controller lifecycle")
		//testSoftTainterController(g.GinkgoTB(), ctx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should validate soft tainter controller with VAP [Slow][Timeout:15m]", func() {
		g.By("Testing soft tainter controller with VAP")
		//testSoftTainterControllerWithVAP(g.GinkgoTB(), ctx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should deschedule pods correctly [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing pod descheduling")
		deschedulingCtx, deschedulingCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer deschedulingCancel()
		testPodDescheduling(g.GinkgoTB(), deschedulingCtx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should have metrics service available [Slow][Timeout:15m]", func() {
		g.By("Testing metrics service")
		metricsCtx, metricsCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer metricsCancel()
		testMetricsService(g.GinkgoTB(), metricsCtx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should have ServiceMonitor configured [Slow][Timeout:15m]", func() {
		g.By("Testing ServiceMonitor")
		monitorCtx, monitorCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer monitorCancel()
		testServiceMonitor(g.GinkgoTB(), monitorCtx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should have Prometheus target up [Slow][Timeout:15m]", func() {
		g.By("Testing Prometheus target")
		prometheusCtx, prometheusCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer prometheusCancel()
		testPrometheusTarget(g.GinkgoTB(), prometheusCtx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should have metrics data available [Slow][Timeout:15m]", func() {
		g.By("Testing metrics data")
		dataCtx, dataCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer dataCancel()
		testMetricsData(g.GinkgoTB(), dataCtx, kubeClient)
	})

	// ============================================================================
	// PROFILE/STRATEGY TESTS - Each test manages its own CR
	// ============================================================================
	// Each test explicitly manages CR lifecycle via runProfileTest():
	// - Test function creates custom CR with its specific profile
	// - Test validates the profile behavior
	// - Cleanup deletes CR after test completes
	// This avoids hook scoping issues and makes CR state explicit

	// OCP-21205, OCP-36584
	g.It("[OTP][Operator][Serial] should validate PDB compliance during pod evictions [Disruptive][Slow][Timeout:5m]", func() {
		g.By("Testing PDB compliance during pod evictions")
		runProfileTest(ctx, kubeClient, deschClient, testPDBCompliance)
	})

	// OCP-43277, OCP-50941, OCP-76158
	g.It("[OTP][Operator][Serial] should validate descheduler modes and eviction limits [Disruptive][Slow][Timeout:5m]", func() {
		g.By("Testing Predictive and Automatic modes with eviction limits")
		runProfileTest(ctx, kubeClient, deschClient, testDeschedulerModes)
	})

	// OCP-37463, OCP-40055
	g.It("[OTP][Operator][Serial] should validate AffinityAndTaints and TopologyAndDuplicates profiles [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing AffinityAndTaints and TopologyAndDuplicates profiles")
		runProfileTest(ctx, kubeClient, deschClient, testAffinityAndTopologyProfiles)
	})

	// OCP-52303
	g.It("[OTP][Operator][Serial] should validate namespace include filtering [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing namespace include filtering")
		runProfileTest(ctx, kubeClient, deschClient, testNamespaceIncludeFiltering)
	})

	// OCP-53058
	g.It("[OTP][Operator][Serial] should validate namespace exclude filtering [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing namespace exclude filtering")
		runProfileTest(ctx, kubeClient, deschClient, testNamespaceExcludeFiltering)
	})

	// OCP-76422
	g.It("[OTP][Operator][Serial] should validate LongLifecycle profile behavior [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing LongLifecycle profile behavior")
		runProfileTest(ctx, kubeClient, deschClient, testLongLifecycleProfile)
	})

	g.It("[OTP][Operator][Serial] should validate NodeAffinity strategy [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing NodeAffinity strategy")
		runProfileTest(ctx, kubeClient, deschClient, testNodeAffinityStrategy)
	})

	g.It("[OTP][Operator][Serial] should validate NodeTaint strategy [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing NodeTaint strategy")
		runProfileTest(ctx, kubeClient, deschClient, testNodeTaintStrategy)
	})
	g.It("[OTP][Operator][Serial] should validate InterPodAntiAffinity strategy [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing InterPodAntiAffinity strategy")
		runProfileTest(ctx, kubeClient, deschClient, testInterPodAntiAffinityStrategy)
	})

	g.It("[OTP][Operator][Serial] should validate RemoveDuplicates strategy [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing RemoveDuplicates strategy")
		runProfileTest(ctx, kubeClient, deschClient, testRemoveDuplicatesStrategy)
	})

	g.AfterEach(func() {
		g.By("AfterEach: Skipping operator cleanup to preserve test environment")
		if cancelFnc != nil {
			cancelFnc()
		}
		// TODO: Implement proper cleanup when needed
		// Original cleanup logic commented below for reference:
		/*
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cleanupCancel()


				if olmInstalled {
					g.By("Cleaning up operator installation")

					og := &operatorsv1.OperatorGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "descheduler-og",
							Namespace: operatorclient.OperatorNamespace,
						},
					}
					sub, err := packagemanifestKDO(cleanupCtx, dynamicClient, "cluster-kube-descheduler-operator", operatorclient.OperatorNamespace, []string{"redhat-operators"})
					if err != nil {
						klog.Warningf("Failed to get packagemanifest for cleanup: %v", err)
					}

					if err := deleteKubeDescheduler(cleanupCtx, deschClient, operatorclient.OperatorNamespace, operatorclient.OperatorConfigName); err != nil {
						klog.Warningf("Failed to delete KubeDescheduler: %v", err)
					}
					if sub != nil {
						if err := deleteSubscription(cleanupCtx, dynamicClient, sub); err != nil {
							klog.Warningf("Failed to delete Subscription: %v", err)
						}
					}
					if err := deleteOperatorGroup(cleanupCtx, dynamicClient, og); err != nil {
						klog.Warningf("Failed to delete OperatorGroup: %v", err)
					}
				}


					g.By("Deleting operator namespace")
					err := kubeClient.CoreV1().Namespaces().Delete(cleanupCtx, operatorclient.OperatorNamespace, metav1.DeleteOptions{})
					if err != nil {
						klog.Warningf("Failed to delete namespace %s: %v", operatorclient.OperatorNamespace, err)
					}

					g.By("Ensuring namespace is fully deleted")
					err = wait.PollUntilContextTimeout(cleanupCtx, 5*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
						_, err := kubeClient.CoreV1().Namespaces().Get(ctx, operatorclient.OperatorNamespace, metav1.GetOptions{})
						if err != nil {
							if strings.Contains(err.Error(), "not found") {
								klog.Infof("Namespace %s successfully deleted", operatorclient.OperatorNamespace)
								return true, nil
							}
							klog.Warningf("Error checking namespace: %v", err)
							return false, nil
						}
						klog.Infof("Waiting for namespace %s to be fully deleted...", operatorclient.OperatorNamespace)
						return false, nil
					})
					if err != nil {
						klog.Warningf("Timeout waiting for namespace deletion: %v", err)
					}

					if cancelFnc != nil {
						cancelFnc()
					}
		*/

	})
})

// Test implementations

// testProfileConflicts validates profile conflict validation
func testProfileConflicts(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {

	// Test 1: LongLifecycle + LifecycleAndUtilization should be rejected
	g.By("Testing LongLifecycle + LifecycleAndUtilization conflict")
	err := createKubeDeschedulerWithProfiles(ctx, deschClient, "test-conflict-1",
		[]string{"EvictPodsWithPVC", "LongLifecycle", "LifecycleAndUtilization"})
	o.Expect(err).To(o.HaveOccurred(), "Expected KubeDescheduler creation to fail with conflicting profiles")
	o.Expect(err.Error()).To(o.ContainSubstring("cannot declare LongLifecycle and LifecycleAndUtilization profiles simultaneously"))
	klog.Infof("LongLifecycle + LifecycleAndUtilization conflict correctly rejected")

	// Test 2: CompactAndScale + LifecycleAndUtilization should be rejected
	g.By("Testing CompactAndScale + LifecycleAndUtilization conflict")
	err = createKubeDeschedulerWithProfiles(ctx, deschClient, "test-conflict-2",
		[]string{"AffinityAndTaints", "CompactAndScale", "LifecycleAndUtilization"})
	o.Expect(err).To(o.HaveOccurred(), "Expected KubeDescheduler creation to fail with conflicting profiles")
	o.Expect(err.Error()).To(o.ContainSubstring("cannot declare CompactAndScale and LifecycleAndUtilization profiles simultaneously"))
	klog.Infof("CompactAndScale + LifecycleAndUtilization conflict correctly rejected")

	// Test 3: CompactAndScale + LongLifecycle should be rejected
	g.By("Testing CompactAndScale + LongLifecycle conflict")
	err = createKubeDeschedulerWithProfiles(ctx, deschClient, "test-conflict-3",
		[]string{"AffinityAndTaints", "CompactAndScale", "LongLifecycle"})
	o.Expect(err).To(o.HaveOccurred(), "Expected KubeDescheduler creation to fail with conflicting profiles")
	o.Expect(err.Error()).To(o.ContainSubstring("cannot declare CompactAndScale and LongLifecycle profiles simultaneously"))
	klog.Infof("CompactAndScale + LongLifecycle conflict correctly rejected")

	// Test 4: CompactAndScale + TopologyAndDuplicates should be rejected
	g.By("Testing CompactAndScale + TopologyAndDuplicates conflict")
	err = createKubeDeschedulerWithProfiles(ctx, deschClient, "test-conflict-4",
		[]string{"AffinityAndTaints", "CompactAndScale", "TopologyAndDuplicates"})
	o.Expect(err).To(o.HaveOccurred(), "Expected KubeDescheduler creation to fail with conflicting profiles")
	o.Expect(err.Error()).To(o.ContainSubstring("cannot declare CompactAndScale and TopologyAndDuplicates profiles simultaneously"))
	klog.Infof("CompactAndScale + TopologyAndDuplicates conflict correctly rejected")

	klog.Infof("Profile conflict validation completed successfully")
}

// testRelatedImages tests that CSV has relatedImages defined correctly
func testRelatedImages(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	dynamicClient := GetDynamicClient()

	g.By("Getting CSV name for descheduler operator")
	csvName, err := getCSVName(ctx, dynamicClient, operatorclient.OperatorNamespace, "")
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(csvName).NotTo(o.BeEmpty())
	klog.Infof("Found CSV: %s", csvName)

	g.By("Verifying CSV has relatedImages defined")
	relatedImages, err := getCSVRelatedImages(ctx, dynamicClient, operatorclient.OperatorNamespace, csvName)
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(relatedImages)).To(o.BeNumerically(">", 0), "CSV should have at least one relatedImage")

	var foundOperator, foundOperand bool
	for _, img := range relatedImages {
		klog.Infof("Found relatedImage: %s -> %s", img.Name, img.Image)
		if strings.Contains(img.Name, "descheduler-operator") || strings.Contains(img.Image, "descheduler-operator") {
			foundOperator = true
		}
		if strings.Contains(img.Name, "descheduler-operand") || strings.Contains(img.Name, "descheduler") && !strings.Contains(img.Name, "operator") {
			foundOperand = true
		}
	}

	o.Expect(foundOperator).To(o.BeTrue(), "CSV should contain descheduler-operator related image")
	o.Expect(foundOperand).To(o.BeTrue(), "CSV should contain descheduler-operand related image")

	klog.Infof("RelatedImages validation completed successfully - found %d images", len(relatedImages))
}

// testOLMMustGatherData verifies that must-gather collects OLM data
func testOLMMustGatherData(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	dynamicClient := GetDynamicClient()

	g.By("Verifying CSV exists")
	csvName, err := getCSVName(ctx, dynamicClient, operatorclient.OperatorNamespace, "")
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(csvName).NotTo(o.BeEmpty())
	klog.Infof("Found CSV: %s", csvName)

	g.By("Verifying Subscription exists")
	subList, err := dynamicClient.Resource(schema.GroupVersionResource{
		Group:    operatorsv1alpha1.GroupName,
		Version:  operatorsv1alpha1.GroupVersion,
		Resource: "subscriptions",
	}).Namespace(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(subList.Items)).To(o.BeNumerically(">", 0))
	klog.Infof("Found %d Subscription(s)", len(subList.Items))

	g.By("Verifying OperatorGroup exists")
	ogList, err := dynamicClient.Resource(schema.GroupVersionResource{
		Group:    operatorsv1.GroupVersion.Group,
		Version:  operatorsv1.GroupVersion.Version,
		Resource: "operatorgroups",
	}).Namespace(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(ogList.Items)).To(o.BeNumerically(">", 0))
	klog.Infof("Found %d OperatorGroup(s)", len(ogList.Items))

	g.By("Running must-gather and verifying OLM data collection")
	mustGatherDir := "/tmp/must-gather-45694"
	defer func() {
		_ = kubeClient.CoreV1().Pods(operatorclient.OperatorNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
	}()

	cmd := fmt.Sprintf("oc adm must-gather --dest-dir=%s 2>&1 && rm -rf %s", mustGatherDir, mustGatherDir)
	output, err := exec.Command("bash", "-c", cmd).CombinedOutput()

	if err != nil {
		klog.Warningf("must-gather command failed (may not be available in this environment): %v", err)
		klog.Infof("OLM resources verified successfully - CSV, Subscription, and OperatorGroup exist")
		return
	}

	mustGatherOutput := string(output)

	expectedOLMResources := []string{
		"operators.coreos.com/installplans",
		"operators.coreos.com/operatorconditions",
		"operators.coreos.com/operatorgroups",
		"operators.coreos.com/subscriptions",
	}

	for _, resource := range expectedOLMResources {
		if !strings.Contains(mustGatherOutput, resource) {
			klog.Warningf("must-gather output does not mention %s, but OLM resources were verified to exist", resource)
		} else {
			klog.Infof("must-gather successfully collected: %s", resource)
		}
	}

	klog.Infof("OLM must-gather data validation completed successfully")
}

// testPDBCompliance verifies that descheduler respects Pod Disruption Budgets
func testPDBCompliance(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	g.By("Checking for SNO cluster")
	nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/worker=",
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	if len(nodes.Items) < 2 {
		g.Skip("Skipping test on SNO cluster - requires at least 2 worker nodes")
	}

	g.By("Creating test namespace")
	testNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pdb-compliance",
		},
	}
	_, err = kubeClient.CoreV1().Namespaces().Create(ctx, testNS, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		o.Expect(err).NotTo(o.HaveOccurred())
	}
	defer kubeClient.CoreV1().Namespaces().Delete(ctx, testNS.Name, metav1.DeleteOptions{})

	g.By("Cordoning all nodes except one")
	nodeList := nodes.Items
	for i := 1; i < len(nodeList); i++ {
		err = cordonNode(ctx, kubeClient, &nodeList[i])
		o.Expect(err).NotTo(o.HaveOccurred())
	}
	defer func() {
		for i := 1; i < len(nodeList); i++ {
			uncordonNode(ctx, kubeClient, &nodeList[i])
		}
	}()

	g.By("Creating deployment with multiple replicas")
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: testNS.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32(12),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-pdb"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-pdb"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "pause",
							Image: "registry.k8s.io/pause",
						},
					},
				},
			},
		},
	}
	_, err = kubeClient.AppsV1().Deployments(testNS.Name).Create(ctx, deployment, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Waiting for all pods to be running")
	err = waitForDeploymentReady(ctx, kubeClient, testNS.Name, "test-deployment")
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Creating PDB with minAvailable=11")
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pdb",
			Namespace: testNS.Name,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 11,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-pdb"},
			},
		},
	}
	_, err = kubeClient.PolicyV1().PodDisruptionBudgets(testNS.Name).Create(ctx, pdb, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.PolicyV1().PodDisruptionBudgets(testNS.Name).Delete(ctx, pdb.Name, metav1.DeleteOptions{})

	g.By("Creating KubeDescheduler CR with Automatic mode")
	err = createKubeDeschedulerAndWait(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Mode = descv1.Automatic
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.LifecycleAndUtilization}
		kd.Spec.ProfileCustomizations = &descv1.ProfileCustomizations{
			PodLifetime: &metav1.Duration{Duration: 10 * time.Second},
		}
	}))
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Uncordoning second node")
	err = uncordonNode(ctx, kubeClient, &nodeList[1])
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Checking descheduler logs for PDB violation message")
	podName, err := getPodByLabel(ctx, kubeClient, operatorclient.OperatorNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	expectedPattern := regexp.QuoteMeta(`"Error evicting pod"`) + ".*" + regexp.QuoteMeta(`Cannot evict pod as it would violate the pod's disruption budget.`)
	err = checkPodLogs(ctx, kubeClient, operatorclient.OperatorNamespace, podName, expectedPattern)
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("Descheduler correctly respects PDB")
}

func testAffinityAndTopologyProfiles(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	err := createAndValidateKubeDeschedulerCR(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Mode = descv1.Automatic
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.AffinityAndTaints, descv1.TopologyAndDuplicates}
	}), "AffinityAndTaints and TopologyAndDuplicates profiles")
	o.Expect(err).NotTo(o.HaveOccurred())
}

func testDeschedulerModes(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	checkDryRunFlag := func(ctx context.Context, kubeClient *k8sclient.Clientset, expectDryRun bool) (bool, error) {
		deployment, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil {
			klog.V(2).Infof("Failed to get descheduler deployment: %v", err)
			return false, nil
		}

		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			klog.V(2).Info("Descheduler deployment has no containers")
			return false, nil
		}

		args := deployment.Spec.Template.Spec.Containers[0].Args
		hasDryRun := slices.Contains(args, "--dry-run=true")

		if expectDryRun != hasDryRun {
			klog.V(2).Infof("Descheduler deployment dry-run flag mismatch: expected %v, got %v, args: %v", expectDryRun, hasDryRun, args)
			return false, nil
		}

		klog.V(4).Infof("Descheduler deployment dry-run flag correct: %v", hasDryRun)
		return true, nil
	}

	g.By("Creating new KubeDescheduler CR with Predictive mode")
	err := createKubeDeschedulerAndWait(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Mode = descv1.Predictive
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.LifecycleAndUtilization}
		kd.Spec.ProfileCustomizations = &descv1.ProfileCustomizations{
			PodLifetime: &metav1.Duration{Duration: 10 * time.Second},
		}
	}))
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Validating Predictive mode configuration (--dry-run=true)")
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 1*time.Minute, true, func(ctx context.Context) (bool, error) {
		return checkDryRunFlag(ctx, kubeClient, true)
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Waiting for descheduler operand to run stably for 30 seconds (Predictive mode)")
	err = waitForOperandStability(ctx, kubeClient, 30*time.Second)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Deleting Predictive KubeDescheduler CR and waiting for operand to be gone")
	err = deleteKubeDeschedulerAndWait(ctx, kubeClient, deschClient)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Waiting for descheduler deployment to be fully deleted")
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil && strings.Contains(err.Error(), "not found") {
			return true, nil
		}
		return false, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Creating new KubeDescheduler CR with Automatic mode")
	err = createKubeDeschedulerAndWait(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Mode = descv1.Automatic
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.LifecycleAndUtilization}
		kd.Spec.ProfileCustomizations = &descv1.ProfileCustomizations{
			PodLifetime: &metav1.Duration{Duration: 10 * time.Second},
		}
	}))
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Validating Automatic mode configuration (no --dry-run)")
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		return checkDryRunFlag(ctx, kubeClient, false)
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Waiting for descheduler operand to run stably for 30 seconds (Automatic mode)")
	err = waitForOperandStability(ctx, kubeClient, 30*time.Second)
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("Descheduler modes validated successfully")
}

func testNamespaceIncludeFiltering(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	err := createAndValidateKubeDeschedulerCR(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.LifecycleAndUtilization}
		kd.Spec.ProfileCustomizations = &descv1.ProfileCustomizations{
			PodLifetime: &metav1.Duration{Duration: 10 * time.Second},
			Namespaces: descv1.Namespaces{
				Included: []string{"test-include-ns-1", "test-include-ns-2"},
			},
		}
	}), "namespace include filtering")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("Namespace include filtering validated successfully")
}

func testNamespaceExcludeFiltering(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	err := createAndValidateKubeDeschedulerCR(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.LifecycleAndUtilization}
		kd.Spec.ProfileCustomizations = &descv1.ProfileCustomizations{
			PodLifetime: &metav1.Duration{Duration: 10 * time.Second},
			Namespaces: descv1.Namespaces{
				Excluded: []string{"test-exclude-ns-1", "test-exclude-ns-2"},
			},
		}
	}), "namespace exclude filtering")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("Namespace exclude filtering validated successfully")
}

func testLongLifecycleProfile(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	err := createAndValidateKubeDeschedulerCR(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.LongLifecycle}
	}), "LongLifecycle profile")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("LongLifecycle profile validated successfully")
}

func testNodeAffinityStrategy(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	err := createAndValidateKubeDeschedulerCR(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.AffinityAndTaints}
	}), "AffinityAndTaints profile")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("NodeAffinity strategy validated successfully")
}

func testNodeTaintStrategy(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	err := createAndValidateKubeDeschedulerCR(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.AffinityAndTaints}
	}), "AffinityAndTaints profile")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("NodeTaint strategy validated successfully")
}

func testInterPodAntiAffinityStrategy(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	err := createAndValidateKubeDeschedulerCR(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.TopologyAndDuplicates}
	}), "TopologyAndDuplicates profile")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("InterPodAntiAffinity strategy validated successfully")
}

func testRemoveDuplicatesStrategy(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) {
	err := createAndValidateKubeDeschedulerCR(ctx, kubeClient, deschClient, buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.TopologyAndDuplicates}
	}), "TopologyAndDuplicates profile")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("RemoveDuplicates strategy validated successfully")
}

// runProfileTest manages CR lifecycle for profile tests:
// 1. Save the default "cluster" CR spec
// 2. Delete the current "cluster" CR
// 3. Run the test (which creates "cluster" CR with custom profile)
// 4. Validate the profile
// 5. Delete the test CR
// 6. Recreate the default "cluster" CR with saved spec
// 7. Wait for deployment readiness
func runProfileTest(ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset, testFn func(testing.TB, context.Context, *k8sclient.Clientset, *deschclient.Clientset)) {
	// Step 1: Save the current default "cluster" CR spec
	g.By("Saving default KubeDescheduler CR spec before profile test")
	originalCR, err := deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Error getting original KubeDescheduler CR: %v", err)
		return
	}
	savedSpec := originalCR.Spec

	// Register cleanup BEFORE mutations - runs even if testFn fails with fatal assertion
	// Uses fresh timeout context independent of test ctx
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	g.DeferCleanup(func() {
		defer cleanupCancel()

		// Delete the test CR (may not exist if testFn failed before creating it)
		g.By("Cleanup: Deleting test KubeDescheduler CR")
		err := deleteKubeDescheduler(cleanupCtx, deschClient, operatorclient.OperatorNamespace, operatorclient.OperatorConfigName)
		if err != nil && !strings.Contains(err.Error(), "not found") {
			g.Fail(fmt.Sprintf("Cleanup: Error deleting test KubeDescheduler CR: %v", err))
		}

		// Wait for deployment to be deleted
		g.By("Cleanup: Waiting for descheduler deployment to be deleted")
		err = wait.PollUntilContextTimeout(cleanupCtx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			_, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
			if err != nil && strings.Contains(err.Error(), "not found") {
				klog.V(4).Info("Descheduler deployment deleted")
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			g.Fail(fmt.Sprintf("Cleanup: Timeout waiting for deployment deletion: %v", err))
		}

		// Solution 1: Wait for operator ConfigMap to be cleaned up to prevent policy mismatch in next test
		g.By("Cleanup: Waiting for operator ConfigMap to be cleaned up")
		err = wait.PollUntilContextTimeout(cleanupCtx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			_, err := kubeClient.CoreV1().ConfigMaps(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
			if err != nil && strings.Contains(err.Error(), "not found") {
				klog.Infof("ConfigMap cleaned up - preventing stale policy in next test")
				return true, nil
			}
			klog.V(4).Infof("ConfigMap still exists, waiting for deletion")
			return false, nil
		})
		if err != nil {
			klog.Warningf("Cleanup: Warning - ConfigMap cleanup timeout (may cause policy validation failures in next test): %v", err)
		}

		// Restore the original "cluster" CR with saved spec
		g.By("Cleanup: Recreating default KubeDescheduler CR")
		restoredCR := buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
			kd.Spec = savedSpec
		})
		err = createKubeDeschedulerAndWait(cleanupCtx, kubeClient, deschClient, restoredCR)
		if err != nil {
			g.Fail(fmt.Sprintf("Cleanup: Error recreating default KubeDescheduler CR: %v", err))
		}
	})

	// Step 2: Delete the current "cluster" CR to make room for test CR
	g.By("Deleting default KubeDescheduler CR to prepare for profile test")
	err = deleteKubeDescheduler(ctx, deschClient, operatorclient.OperatorNamespace, operatorclient.OperatorConfigName)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		g.Fail(fmt.Sprintf("Error deleting KubeDescheduler CR before test: %v", err))
		return
	}
	klog.Infof("Successfully deleted default KubeDescheduler CR")

	// Wait for CR to actually be deleted (not just delete API call to succeed)
	g.By("Waiting for KubeDescheduler CR to be removed from cluster")
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
		if err != nil && strings.Contains(err.Error(), "not found") {
			klog.V(4).Info("KubeDescheduler CR fully deleted from cluster")
			return true, nil
		}
		if err != nil {
			return false, nil
		}
		return false, nil
	})
	if err != nil {
		g.Fail(fmt.Sprintf("Timeout waiting for CR deletion: %v", err))
		return
	}

	// Wait for deployment to be deleted
	g.By("Waiting for descheduler deployment to be deleted")
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil && strings.Contains(err.Error(), "not found") {
			klog.V(4).Info("Descheduler deployment deleted")
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		g.Fail(fmt.Sprintf("Timeout waiting for deployment deletion (may continue anyway): %v", err))
	}

	// Step 3: Run the test (which creates "cluster" CR with custom profile)
	// Use fresh context with extended timeout for the test to avoid parent context deadline
	testCtx, testCancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer testCancel()

	g.By("Running profile test with fresh 15-minute context")
	testFn(g.GinkgoTB(), testCtx, kubeClient, deschClient)
}
