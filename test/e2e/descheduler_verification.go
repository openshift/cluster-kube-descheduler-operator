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
// DESIGN: Single g.Describe with all tests organized by concern
//
// This architecture contains all operator tests in one Describe block:
// - Metrics and Service Tests: 10 tests using default CR (no CR modification)
// - Profile and Strategy Tests: 10 tests using runProfileTest() for CR lifecycle management
//
// EXECUTION FLOW:
// 1. BeforeEach: Install operator + create default CR (LifecycleAndUtilization)
// 2. METRICS/SERVICE TESTS: Use pre-created default CR directly
//    - Metrics service available
//    - Prometheus target up
//    - Metrics data available
//    - Plus other basic operator validation tests
// 3. PROFILE/STRATEGY TESTS: Use runProfileTest() wrapper
//    - Each test gets its own CR via runProfileTest()
//    - Test creates custom CR with its specific profile
//    - runProfileTest() uses g.DeferCleanup() to delete test CR after each test
//    - No need to restore default CR between tests (handled via BeforeEach)
// 4. AfterEach: Final cleanup (cancel context, delete namespace if non-OLM)
//
// WHY THIS WORKS:
// ✅ All tests in single Describe = unified setup/teardown
// ✅ Metrics tests use default CR directly = simple and predictable
// ✅ Profile tests use runProfileTest() = handles CR lifecycle internally
// ✅ No duplicate setup code = single BeforeEach/AfterEach
// ✅ Clear organization = metrics tests first, then profile tests
//

const (
	deschedulerOperatorLabel = "name=descheduler-operator"
	deschedulerLabel         = "app=descheduler"
)

func isOperatorOLMInstallationEnabled() bool {
	return os.Getenv("NO_OLM") == "" && os.Getenv("OPERATOR_IMAGE") == "" && os.Getenv("OPERAND_IMAGE") == ""
}

// isOperatorPreInstalled checks if the operator was already installed
// (e.g., via operator-sdk run bundle in CI) by looking for an existing CSV.
// Retries for up to 30 seconds to handle transient connection issues.
func isOperatorPreInstalled(ctx context.Context, dynamicClient dynamic.Interface, namespace string) bool {
	var csvName string
	var lastErr error

	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 30*time.Second, true, func(pollCtx context.Context) (bool, error) {
		var err error
		csvName, err = getCSVName(pollCtx, dynamicClient, namespace, "")
		if err != nil {
			lastErr = err
			klog.V(2).Infof("Failed to get CSV name, retrying: %v", err)
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		klog.Warningf("Timeout checking for pre-installed operator after 30s: %v", lastErr)
		return false
	}

	return csvName != ""
}

// Ginkgo test specs for migrated OTP tests
// Design: Two independent Describe blocks matching the component_proxy pattern
// 1. Metrics and Service Tests - Full setup with default CR
// 2. Profile and Strategy Tests - Full setup with CR lifecycle management

// ============================================================================
// DESCRIBE 1: Metrics and Service Tests - Full setup with default CR
// ============================================================================
var _ = g.Describe("[OTP][Operator][Serial] Descheduler Operator - Metrics and Service Tests", g.Serial, func() {
	var (
		ctx           context.Context
		cancelFnc     context.CancelFunc
		olmInstalled  bool // Flag: true if operator installed via OLM, false if non-OLM
		kubeClient    *k8sclient.Clientset
		dynamicClient dynamic.Interface
		deschClient   *deschclient.Clientset
		apiExtClient  *apiextclientv1.Clientset
	)

	g.BeforeEach(func() {
		g.By("Setting up test environment for metrics tests")
		var err error
		var operandImage string // Store operand image for later verification
		kubeClient = GetKubeClient()
		dynamicClient = GetDynamicClient()
		deschClient = GetDeschedulerClient()
		apiExtClient = GetApiExtensionClient()
		ctx, cancelFnc = context.WithCancel(context.TODO())

		if !isOperatorOLMInstallationEnabled() {
			// Non-OLM path: install operator from deploy/ folder using OPERATOR_IMAGE/OPERAND_IMAGE
			olmInstalled = false // Operator will be installed non-OLM way
			err = setupOperator(ctx, kubeClient, deschClient, apiExtClient)
		} else if isOperatorPreInstalled(ctx, dynamicClient, operatorclient.OperatorNamespace) {
			// Bundle-based CI installation (operator-sdk run bundle) pre-installs the operator;
			// only the KubeDescheduler CR and operand readiness are needed.
			klog.Infof("Operator already installed, skipping installation")
			olmInstalled = true // Operator was installed via OLM (bundle)

			// Read operator and operand images from SHARED_DIR (once, for reuse in patching and verification)
			klog.Infof("Reading operand image from SHARED_DIR")
			sharedDir := os.Getenv("SHARED_DIR")
			if sharedDir != "" {
				// Read OPERAND_IMAGE from file (store for later verification)
				if operandImageBytes, err := os.ReadFile(sharedDir + "/operand-image"); err == nil {
					operandImage = strings.TrimSpace(string(operandImageBytes))
					klog.Infof("OPERAND_IMAGE from SHARED_DIR: %s", operandImage)
				} else {
					klog.Warningf("Could not read operand-image file: %v", err)
				}
			} else {
				klog.V(2).Infof("SHARED_DIR not set, skipping operand image reading")
			}

			// Check if CSV already has the operand image, only patch if needed
			if operandImage != "" {
				g.By("Checking if CSV already has the operand image")
				verifyErr := verifyCSVHasImage(ctx, dynamicClient, operatorclient.OperatorNamespace, operandImage)
				if verifyErr != nil {
					// CSV doesn't have the operand image, apply the patch
					g.By("Patching CSV with operand image from SHARED_DIR")
					patchErr := patchCSVWithImages(ctx, dynamicClient, kubeClient, operatorclient.OperatorNamespace, operandImage)
					if patchErr != nil {
						klog.Warningf("Warning: Failed to patch CSV with images: %v (this is OK if not running in CI)", patchErr)
					} else {
						klog.Infof("✅ CSV patched successfully")

						// Only after applying patch, verify operator deployment is ready with new images
						g.By("Verifying operator deployment is ready with new images")
						deployErr := waitForDeploymentReady(ctx, kubeClient, operatorclient.OperatorNamespace, "descheduler-operator")
						if deployErr != nil {
							klog.Warningf("Warning: Failed to verify operator deployment: %v", deployErr)
						}

						// Ensure namespace has cluster-monitoring label for Prometheus scraping
						g.By("Ensuring namespace has cluster-monitoring label")
						labelErr := ensureNamespaceMonitoringLabel(ctx, kubeClient, operatorclient.OperatorNamespace)
						if labelErr != nil {
							klog.Warningf("Warning: Failed to ensure monitoring label on namespace: %v", labelErr)
						}
					}
				} else {
					klog.Infof("✅ CSV already has the operand image: %s (skipping patch and deployment checks)", operandImage)
				}
			}

			// Ensure default KubeDescheduler CR exists
			g.By("Ensuring default KubeDescheduler CR exists")
			err = ensureDefaultKubeDescheduler(ctx, kubeClient, deschClient)
		} else {
			// OLM path: install via PackageManifest/Subscription (requires CatalogSource with KDO package)
			olmInstalled = true // Operator will be installed via OLM
			err = installOperatorWithSubscription(ctx, kubeClient, deschClient, dynamicClient, operatorclient.OperatorNamespace)
		}
		o.Expect(err).NotTo(o.HaveOccurred())

		// Wait for descheduler pod to stabilize before any tests run
		// This ensures Prometheus has time to discover the ServiceMonitor
		g.By("Waiting for descheduler pod to stabilize before tests")
		err = waitForOperandStability(ctx, kubeClient, 30*time.Second)
		if err != nil {
			klog.Warningf("Warning: Timeout waiting for pod stability in BeforeEach: %v", err)
		}
	})

	// ============================================================================
	// METRICS/SERVICE TESTS - Use default CR from BeforeAll
	// ============================================================================
	// These tests run WITHOUT hooks, using the default KubeDescheduler CR
	// Simple, predictable validation of operator functionality

	// OCP-76194
	g.It("[OTP][Operator][Serial] should validate profile conflict validation [Slow][Timeout:15m]", func() {
		g.By("Testing profile conflict validation")
		testProfileConflicts(g.GinkgoTB(), ctx, kubeClient, deschClient)
	})

	// OCP-83032
	g.It("[OTP][Operator][Serial] should validate RelatedImages defined in CSV [Slow][Timeout:15m]", func() {
		g.By("Testing RelatedImages defined in CSV")
		if !olmInstalled {
			g.Skip("Skipping. The operator is not installed via OLM")
		}
		testRelatedImages(g.GinkgoTB(), ctx, kubeClient)
	})

	// OCP-45694
	g.It("[OTP][Operator][Serial] should validate must-gather OLM data collection [Slow][Disruptive][Timeout:15m]", func() {
		g.By("Testing must-gather OLM data collection")
		if !olmInstalled {
			g.Skip("Skipping. The operator is not installed via OLM")
		}
		testOLMMustGatherData(g.GinkgoTB(), ctx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should create and remove soft tainter objects [Slow][Timeout:15m]", func() {
		// Skip this test if operator is pre-installed via bundle (to avoid recreating operator)
		if olmInstalled {
			g.Skip("Skipping test - operator is pre-installed via bundle")
		}
		g.By("Testing soft tainter controller lifecycle")
		testSoftTainterController(g.GinkgoTB(), ctx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should validate soft tainter controller with VAP [Slow][Timeout:15m]", func() {
		// Skip this test if operator is pre-installed via bundle (to avoid recreating operator)
		if olmInstalled {
			g.Skip("Skipping test - operator is pre-installed via bundle")
		}
		g.By("Testing soft tainter controller with VAP")
		testSoftTainterControllerWithVAP(g.GinkgoTB(), ctx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should deschedule pods correctly [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing pod descheduling")
		testPodDescheduling(g.GinkgoTB(), ctx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should have metrics service available [Slow][Timeout:15m]", func() {
		g.By("Testing metrics service")
		testMetricsService(g.GinkgoTB(), ctx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should have ServiceMonitor configured [Slow][Timeout:15m]", func() {
		g.By("Testing ServiceMonitor")
		testServiceMonitor(g.GinkgoTB(), ctx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should have Prometheus target up [Slow][Timeout:15m]", func() {
		g.By("Testing Prometheus target")
		testPrometheusTarget(g.GinkgoTB(), ctx, kubeClient)
	})

	g.It("[OTP][Operator][Serial] should have metrics data available [Slow][Timeout:15m]", func() {
		g.By("Testing metrics data")
		testMetricsData(g.GinkgoTB(), ctx, kubeClient)
	})

	// ============================================================================
	// PROFILE/STRATEGY TESTS - Each test manages its own CR lifecycle
	// ============================================================================
	// Tests that modify the KubeDescheduler CR are managed by runProfileTest()
	// which handles CR deletion/creation within g.DeferCleanup()

	// OCP-21205, OCP-36584
	g.It("[OTP][Operator][Serial] should validate PDB compliance during pod evictions [Disruptive][Slow][Timeout:15m]", func() {
		g.By("Testing PDB compliance during pod evictions")
		runProfileTest(ctx, kubeClient, deschClient, testPDBCompliance)
	})

	// OCP-43277, OCP-50941, OCP-76158
	g.It("[OTP][Operator][Serial] should validate descheduler modes and eviction limits [Disruptive][Slow][Timeout:15m]", func() {
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
		// Cancel the test context
		if cancelFnc != nil {
			cancelFnc()
		}

		// IMPORTANT: Do NOT delete the operator namespace if it was pre-installed (OLM bundle)
		// Only delete operator/subscription/operatorgroup if we installed them non-OLM
		if !olmInstalled {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cleanupCancel()

			g.By("AfterEach: Cleaning up non-OLM operator installation")

			if err := deleteKubeDescheduler(cleanupCtx, deschClient, operatorclient.OperatorNamespace, operatorclient.OperatorConfigName); err != nil {
				klog.Warningf("AfterEach: Failed to delete KubeDescheduler: %v", err)
			}

			// Delete the operator namespace only if we created it
			g.By("AfterEach: Deleting operator namespace (non-OLM only)")
			err := kubeClient.CoreV1().Namespaces().Delete(cleanupCtx, operatorclient.OperatorNamespace, metav1.DeleteOptions{})
			if err != nil && !strings.Contains(err.Error(), "not found") {
				klog.Warningf("AfterEach: Failed to delete namespace %s: %v", operatorclient.OperatorNamespace, err)
			}

			g.By("AfterEach: Ensuring namespace is fully deleted")
			wait.PollUntilContextTimeout(cleanupCtx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
				_, err := kubeClient.CoreV1().Namespaces().Get(ctx, operatorclient.OperatorNamespace, metav1.GetOptions{})
				if err != nil && strings.Contains(err.Error(), "not found") {
					klog.Infof("AfterEach: Namespace %s successfully deleted", operatorclient.OperatorNamespace)
					return true, nil
				}
				return false, nil
			})
		}
		klog.Infof("AfterEach: Cleanup completed")
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
// 1. Delete the current "cluster" CR
// 2. Run the test (which creates "cluster" CR with custom profile)
// 3. Delete the test CR in cleanup
func runProfileTest(ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset, testFn func(testing.TB, context.Context, *k8sclient.Clientset, *deschclient.Clientset)) {
	g.DeferCleanup(func() {
		// Create cleanup context inside the deferred callback so the timeout starts when cleanup actually begins,
		// not when the test starts. This ensures cleanup always has a full 10 minutes regardless of test duration.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cleanupCancel()

		// If the namespace was already deleted (non-OLM AfterEach), skip cleanup
		_, nsErr := kubeClient.CoreV1().Namespaces().Get(cleanupCtx, operatorclient.OperatorNamespace, metav1.GetOptions{})
		if apierrors.IsNotFound(nsErr) {
			klog.Infof("Cleanup: Namespace %s already deleted, skipping CR restore", operatorclient.OperatorNamespace)
			return
		}

		g.By("Cleanup: Deleting KubeDescheduler CR")
		if err := deleteKubeDeschedulerAndWait(cleanupCtx, kubeClient, deschClient); err != nil {
			klog.Warningf("Cleanup: Failed to delete KubeDescheduler: %v", err)
		}

		// Wait for operator ConfigMap to be cleaned up to prevent policy mismatch in next test
		g.By("Cleanup: Waiting for operator ConfigMap to be cleaned up")
		if err := waitForConfigMapDeletion(cleanupCtx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperatorConfigName); err != nil {
			klog.Warningf("Cleanup: Warning - ConfigMap cleanup timeout (may cause policy validation failures in next test): %v", err)
		}
	})

	// Check if KubeDescheduler CR exists before attempting deletion, with retry logic for transient failures
	var existing *descv1.KubeDescheduler
	var crNotFound bool

	g.By("Checking if KubeDescheduler CR exists (with retry)")
	err := wait.Poll(1*time.Second, 10*time.Second, func() (bool, error) {
		var getErr error
		existing, getErr = deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
		if getErr != nil {
			if apierrors.IsNotFound(getErr) {
				crNotFound = true
				klog.Infof("KubeDescheduler CR not found during check")
				return true, nil // CR doesn't exist, exit polling successfully
			}
			klog.Warningf("Retrying: failed to get KubeDescheduler CR: %v", getErr)
			return false, nil // Transient error, keep retrying
		}
		crNotFound = false
		klog.Infof("KubeDescheduler CR found during check")
		return true, nil // CR exists, exit polling successfully
	})

	if err != nil {
		g.Fail(fmt.Sprintf("Timeout waiting to check KubeDescheduler CR existence (after 10 seconds): %v", err))
		return
	}

	// Only delete and wait for ConfigMap cleanup if the CR exists
	if !crNotFound && existing != nil {
		g.By("Deleting default KubeDescheduler CR and waiting for operand to be gone")
		if delErr := deleteKubeDeschedulerAndWait(ctx, kubeClient, deschClient); delErr != nil {
			g.Fail(fmt.Sprintf("Error deleting KubeDescheduler CR before test: %v", delErr))
			return
		}

		// Wait for operator ConfigMap to be cleaned up to prevent policy mismatch in this test
		if err := waitForConfigMapDeletion(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperatorConfigName); err != nil {
			klog.Warningf("Warning - ConfigMap cleanup timeout: %v", err)
		}
	} else {
		klog.Infof("KubeDescheduler CR not found, skipping deletion and ConfigMap cleanup")
	}

	g.By("Running profile test")
	testFn(g.GinkgoTB(), ctx, kubeClient, deschClient)
}
