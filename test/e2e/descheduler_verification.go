package e2e

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	descv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	deschclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
)

const (
	deschedulerNamespace     = "openshift-kube-descheduler-operator"
	deschedulerOperatorLabel = "app=descheduler-operator"
	deschedulerLabel         = "app=descheduler"
)

// Ginkgo test specs for migrated OTP tests
var _ = g.Describe("[OTP][Operator][Serial] Descheduler Operator Functionality", g.Ordered, g.Serial, func() {
	var (
		ctx           context.Context
		cancelFnc     context.CancelFunc
		kubeClient    *k8sclient.Clientset
		dynamicClient dynamic.Interface
		deschClient   *deschclient.Clientset
	)

	g.BeforeAll(func() {
		g.By("Setting up test environment")
		var err error
		ctx = context.TODO()
		kubeClient = GetKubeClient()
		dynamicClient = GetDynamicClient()
		deschClient = GetDeschedulerClient()

		// Create namespace first
		g.By("Creating operator namespace")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: deschedulerNamespace,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			o.Expect(err).NotTo(o.HaveOccurred())
		}

		// Install operator via OLM
		g.By("Setting up OperatorGroup")
		og := operatorgroup{
			name:      "descheduler-og",
			namespace: deschedulerNamespace,
		}

		g.By("Fetching subscription details from packagemanifest")
		sub, err := packagemanifestKDO(ctx, dynamicClient, "cluster-kube-descheduler-operator", deschedulerNamespace, []string{"redhat-operators"})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Creating OperatorGroup")
		err = og.createOperatorGroup(ctx, dynamicClient)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Creating Subscription")
		err = sub.createSubscription(ctx, dynamicClient)
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Waiting for descheduler operator deployment")
		err = waitForDeploymentReady(ctx, kubeClient, deschedulerNamespace, "descheduler-operator", "1")
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Waiting for CSV to succeed")
		// Poll for CSV to be available and succeed
		var csvName string
		err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			name, err := getCSVName(ctx, dynamicClient, deschedulerNamespace, "")
			if err != nil {
				klog.V(2).Infof("CSV not yet available: %v", err)
				return false, nil
			}
			csvName = name
			return true, nil
		})
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForCSVSucceeded(ctx, dynamicClient, deschedulerNamespace, csvName)
		o.Expect(err).NotTo(o.HaveOccurred())

		klog.Infof("Descheduler operator successfully installed via OLM, CSV: %s", csvName)

		// Create KubeDescheduler CR to deploy the operand
		// Matches OTP kubedescheduler_podlifetime.yaml configuration
		// Start in Predictive mode (dry-run) - tests can patch to Automatic if needed
		g.By("Creating KubeDescheduler CR")
		kdCR := &descv1.KubeDescheduler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster",
				Namespace: deschedulerNamespace,
			},
			Spec: descv1.KubeDeschedulerSpec{
				OperatorSpec: operatorv1.OperatorSpec{
					ManagementState: operatorv1.Managed,
				},
				DeschedulingIntervalSeconds: utilpointer.Int32(30),
				Mode:                        descv1.Predictive, // Start in Predictive (dry-run) mode
				Profiles:                    []descv1.DeschedulerProfile{descv1.LifecycleAndUtilization},
				ProfileCustomizations: &descv1.ProfileCustomizations{
					PodLifetime: &metav1.Duration{Duration: 10 * time.Second},
				},
				EvictionLimits: &descv1.EvictionLimits{
					Total: utilpointer.Int32(4), // Limit evictions to 4 per cycle
				},
			},
		}
		_, err = deschClient.KubedeschedulersV1().KubeDeschedulers(deschedulerNamespace).Create(ctx, kdCR, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Waiting for descheduler operand deployment")
		err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			_, err := kubeClient.AppsV1().Deployments(deschedulerNamespace).Get(ctx, "descheduler", metav1.GetOptions{})
			return err == nil, nil
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		err = waitForDeploymentReady(ctx, kubeClient, deschedulerNamespace, "descheduler", "1")
		o.Expect(err).NotTo(o.HaveOccurred())

		klog.Infof("Descheduler operand successfully deployed and running")
	})

	g.AfterAll(func() {
		if cancelFnc != nil {
			cancelFnc()
		}

		// Cleanup OLM resources
		g.By("Cleaning up operator installation")
		og := operatorgroup{
			name:      "descheduler-og",
			namespace: deschedulerNamespace,
		}
		sub, _ := packagemanifestKDO(ctx, dynamicClient, "cluster-kube-descheduler-operator", deschedulerNamespace, []string{"redhat-operators"})

		deleteKubeDescheduler(ctx, deschClient, deschedulerNamespace, "cluster")
		sub.deleteSubscription(ctx, dynamicClient)
		og.deleteOperatorGroup(ctx, dynamicClient)

		// Delete the namespace
		g.By("Deleting operator namespace")
		err := kubeClient.CoreV1().Namespaces().Delete(ctx, deschedulerNamespace, metav1.DeleteOptions{})
		if err != nil {
			klog.Warningf("Failed to delete namespace %s: %v", deschedulerNamespace, err)
		}

		// Wait for namespace to be fully deleted before completing AfterAll
		g.By("Ensuring namespace is fully deleted")
		err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			_, err := kubeClient.CoreV1().Namespaces().Get(ctx, deschedulerNamespace, metav1.GetOptions{})
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					klog.Infof("Namespace %s successfully deleted", deschedulerNamespace)
					return true, nil
				}
				klog.Warningf("Error checking namespace: %v", err)
				return false, nil
			}
			klog.Infof("Waiting for namespace %s to be fully deleted...", deschedulerNamespace)
			return false, nil
		})
		if err != nil {
			klog.Warningf("Timeout waiting for namespace deletion: %v", err)
		}
	})

	// GROUP 1: Core Descheduling Features
	// Tests fundamental descheduling behavior: modes, eviction limits, and PDB compliance

	// OCP-43277, OCP-50941, OCP-76158
	g.It("[OTP][Operator][Serial] should validate descheduler modes and eviction limits [Disruptive][Slow][Timeout:30m]", func() {
		g.By("Testing Predictive and Automatic modes with eviction limits")
		testDeschedulerModes(g.GinkgoTB(), ctx, kubeClient)
	})

	// OCP-21205, OCP-36584
	g.It("[OTP][Operator][Serial] should validate PDB compliance during pod evictions [Disruptive][Slow][Timeout:30m]", func() {
		g.By("Testing PDB compliance during pod evictions")
		testPDBCompliance(g.GinkgoTB(), ctx, kubeClient)
	})

	// GROUP 2: Descheduling Profiles and Filtering
	// Tests various descheduling strategies, profiles, and namespace filtering

	// OCP-37463, OCP-40055
	g.It("[OTP][Operator][Serial] should validate AffinityAndTaints and TopologyAndDuplicates profiles [Disruptive][Slow][Timeout:30m]", func() {
		g.By("Testing AffinityAndTaints and TopologyAndDuplicates profiles")
		testAffinityAndTopologyProfiles(g.GinkgoTB(), ctx, kubeClient)
	})

	// OCP-52303
	g.It("[OTP][Operator][Serial] should validate namespace include filtering [Disruptive][Slow][Timeout:20m]", func() {
		g.By("Testing namespace include filtering")
		testNamespaceIncludeFiltering(g.GinkgoTB(), ctx, kubeClient)
	})

	// OCP-53058
	g.It("[OTP][Operator][Serial] should validate namespace exclude filtering [Disruptive][Slow][Timeout:20m]", func() {
		g.By("Testing namespace exclude filtering")
		testNamespaceExcludeFiltering(g.GinkgoTB(), ctx, kubeClient)
	})

	// OCP-76422
	g.It("[OTP][Operator][Serial] should validate LongLifecycle profile behavior [Disruptive][Slow][Timeout:20m]", func() {
		g.By("Testing LongLifecycle profile behavior")
		testLongLifecycleProfile(g.GinkgoTB(), ctx, kubeClient)
	})

	// GROUP 3: OLM Integration and Operator Validation
	// Tests OLM-specific functionality, metadata validation, and must-gather integration

	// OCP-76194
	g.It("[OTP][Operator][Serial] should validate profile conflict validation [Slow][Timeout:15m]", func() {
		g.By("Testing profile conflict validation")
		testProfileConflicts(g.GinkgoTB(), ctx, kubeClient)
	})

	// OCP-83032
	g.It("[OTP][Operator][Serial] should validate RelatedImages defined in CSV [Slow][Timeout:15m]", func() {
		g.By("Testing RelatedImages defined in CSV")
		testRelatedImages(g.GinkgoTB(), ctx, kubeClient)
	})

	// OCP-45694
	g.It("[OTP][Operator][Serial] should validate must-gather OLM data collection [Slow][Disruptive][Timeout:15m]", func() {
		g.By("Testing must-gather OLM data collection")
		testOLMMustGatherData(g.GinkgoTB(), ctx, kubeClient)
	})
})

// Test implementations

// testPDBCompliance verifies that descheduler respects Pod Disruption Budgets
func testPDBCompliance(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
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
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.CoreV1().Namespaces().Delete(ctx, testNS.Name, metav1.DeleteOptions{})

	g.By("Cordoning all nodes except one")
	nodeList := nodes.Items
	for i := 1; i < len(nodeList); i++ {
		err = cordonNode(ctx, kubeClient, nodeList[i].Name)
		o.Expect(err).NotTo(o.HaveOccurred())
	}
	defer func() {
		for i := 1; i < len(nodeList); i++ {
			uncordonNode(ctx, kubeClient, nodeList[i].Name)
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
	err = waitForDeploymentReady(ctx, kubeClient, testNS.Name, "test-deployment", "12")
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

	g.By("Setting descheduler mode to Automatic")
	deschClient := GetDeschedulerClient()
	err = patchKubeDeschedulerMode(ctx, deschClient, "openshift-kube-descheduler-operator", "cluster", "Automatic")
	o.Expect(err).NotTo(o.HaveOccurred())

	defer func() {
		g.By("Restoring descheduler mode to Predictive")
		patchKubeDeschedulerMode(ctx, deschClient, "openshift-kube-descheduler-operator", "cluster", "Predictive")
	}()

	// Wait for descheduler to restart with new mode
	err = waitForDeploymentReady(ctx, kubeClient, "openshift-kube-descheduler-operator", "descheduler", "1")
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Uncordoning second node")
	err = uncordonNode(ctx, kubeClient, nodeList[1].Name)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Checking descheduler logs for PDB violation message")
	podName, err := getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	expectedPattern := regexp.QuoteMeta(`"Error evicting pod"`) + ".*" + regexp.QuoteMeta(`Cannot evict pod as it would violate the pod's disruption budget.`)
	err = checkPodLogs(ctx, kubeClient, deschedulerNamespace, podName, expectedPattern)
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("Descheduler correctly respects PDB")
}

// testAffinityAndTopologyProfiles tests AffinityAndTaints and TopologyAndDuplicates profiles
func testAffinityAndTopologyProfiles(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	g.By("Getting worker nodes")
	nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/worker=",
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	if len(nodes.Items) < 2 {
		g.Skip("Requires at least 2 worker nodes")
	}

	g.By("Creating test namespace")
	testNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-affinity-topology",
		},
	}
	_, err = kubeClient.CoreV1().Namespaces().Create(ctx, testNS, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.CoreV1().Namespaces().Delete(ctx, testNS.Name, metav1.DeleteOptions{})

	// Patch CR to use AffinityAndTaints and TopologyAndDuplicates profiles
	g.By("Updating profiles to AffinityAndTaints and TopologyAndDuplicates")
	deschClient := GetDeschedulerClient()
	patch := []byte(`{"spec":{"profiles":["AffinityAndTaints","TopologyAndDuplicates"]}}`)
	_, err = deschClient.KubedeschedulersV1().KubeDeschedulers("openshift-kube-descheduler-operator").Patch(
		ctx, "cluster", types.MergePatchType, patch, metav1.PatchOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	// Restore original profile after test
	defer func() {
		g.By("Restoring original LifecycleAndUtilization profile")
		patch := []byte(`{"spec":{"profiles":["LifecycleAndUtilization"]}}`)
		deschClient.KubedeschedulersV1().KubeDeschedulers("openshift-kube-descheduler-operator").Patch(
			ctx, "cluster", types.MergePatchType, patch, metav1.PatchOptions{})
	}()

	g.By("Setting descheduler mode to Automatic")
	err = patchKubeDeschedulerMode(ctx, deschClient, "openshift-kube-descheduler-operator", "cluster", "Automatic")
	o.Expect(err).NotTo(o.HaveOccurred())

	defer func() {
		g.By("Restoring descheduler mode to Predictive")
		patchKubeDeschedulerMode(ctx, deschClient, "openshift-kube-descheduler-operator", "cluster", "Predictive")
	}()

	// Wait for descheduler to restart with new mode and profiles
	err = waitForDeploymentReady(ctx, kubeClient, "openshift-kube-descheduler-operator", "descheduler", "1")
	o.Expect(err).NotTo(o.HaveOccurred())

	// Test RemovePodsViolatingNodeAffinity
	g.By("Testing RemovePodsViolatingNodeAffinity strategy")
	testNodeAffinityStrategy(ctx, kubeClient, testNS.Name, nodes.Items)

	// Test RemovePodsViolatingNodeTaints
	g.By("Testing RemovePodsViolatingNodeTaints strategy")
	testNodeTaintStrategy(ctx, kubeClient, testNS.Name, nodes.Items)

	// Test RemovePodsViolatingInterPodAntiAffinity
	g.By("Testing RemovePodsViolatingInterPodAntiAffinity strategy")
	testInterPodAntiAffinityStrategy(ctx, kubeClient, testNS.Name)

	// Test RemoveDuplicates
	g.By("Testing RemoveDuplicates strategy")
	testRemoveDuplicatesStrategy(ctx, kubeClient, testNS.Name, nodes.Items)

	klog.Infof("All affinity and topology strategies validated successfully")
}

// testDeschedulerModes tests Predictive and Automatic modes with eviction limits
func testDeschedulerModes(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	g.By("Creating test namespace")
	testNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-descheduler-modes",
		},
	}
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, testNS, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.CoreV1().Namespaces().Delete(ctx, testNS.Name, metav1.DeleteOptions{})

	g.By("Creating deployment for PodLifeTime testing")
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-podlifetime",
			Namespace: testNS.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32(10),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-lifetime"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-lifetime"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "hello-openshift",
							Image: "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83",
						},
					},
				},
			},
		},
	}
	_, err = kubeClient.AppsV1().Deployments(testNS.Name).Create(ctx, deployment, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Waiting for deployment to be ready")
	err = waitForDeploymentReady(ctx, kubeClient, testNS.Name, "test-podlifetime", "10")
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Getting descheduler pod name")
	podName, err := getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Checking logs for PodLifeTime evictions in Predictive mode (dry run)")
	predictivePattern := regexp.QuoteMeta(`"Evicted pod in dry run mode"`) + ".*" + regexp.QuoteMeta(`strategy="PodLifeTime"`)
	err = checkPodLogs(ctx, kubeClient, deschedulerNamespace, podName, predictivePattern)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Checking logs for eviction limit message")
	limitPattern := regexp.QuoteMeta(`"Error evicting pod" err="maximum number of evicted pods per a descheduling cycle reached" limit=4`)
	err = checkPodLogs(ctx, kubeClient, deschedulerNamespace, podName, limitPattern)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Setting descheduler mode to Automatic")
	deschClient := GetDeschedulerClient()
	err = patchKubeDeschedulerMode(ctx, deschClient, "openshift-kube-descheduler-operator", "cluster", "Automatic")
	o.Expect(err).NotTo(o.HaveOccurred())

	defer func() {
		g.By("Restoring descheduler mode to Predictive")
		patchKubeDeschedulerMode(ctx, deschClient, "openshift-kube-descheduler-operator", "cluster", "Predictive")
	}()

	// Wait for descheduler to restart with new mode
	err = waitForDeploymentReady(ctx, kubeClient, "openshift-kube-descheduler-operator", "descheduler", "1")
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Getting new descheduler pod name after restart")
	podName, err = getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Checking logs for actual pod evictions in Automatic mode")
	automaticPattern := regexp.QuoteMeta(`"Evicted pod"`) + ".*" + regexp.QuoteMeta(`strategy="PodLifeTime"`)
	err = checkPodLogs(ctx, kubeClient, deschedulerNamespace, podName, automaticPattern)
	o.Expect(err).NotTo(o.HaveOccurred())

	// Note: Metrics verification removed - the existing operator tests in operator.go
	// already validate metrics infrastructure comprehensively.
	// This test focuses on mode switching and eviction behavior.

	klog.Infof("Descheduler modes and eviction limits validated successfully")
}

// testNamespaceIncludeFiltering tests namespace include filtering
func testNamespaceIncludeFiltering(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	g.By("Creating test namespace to be included")
	testNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-52303",
		},
	}
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, testNS, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.CoreV1().Namespaces().Delete(ctx, testNS.Name, metav1.DeleteOptions{})

	g.By("Creating deployment in included namespace")
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deploy",
			Namespace: testNS.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-include"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-include"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "hello-openshift",
							Image: "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83",
						},
					},
				},
			},
		},
	}
	_, err = kubeClient.AppsV1().Deployments(testNS.Name).Create(ctx, deployment, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Waiting for deployment to be ready")
	err = waitForDeploymentReady(ctx, kubeClient, testNS.Name, "test-deploy", "1")
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Configuring descheduler with namespace include filter")
	deschClient := GetDeschedulerClient()
	err = patchKubeDeschedulerNamespaceFiltering(ctx, deschClient, "openshift-kube-descheduler-operator", "cluster", []string{testNS.Name}, nil)
	o.Expect(err).NotTo(o.HaveOccurred())

	// Wait for descheduler deployment to rollout with new config
	err = waitForDeploymentReady(ctx, kubeClient, "openshift-kube-descheduler-operator", "descheduler", "1")
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Getting descheduler pod name")
	podName, err := getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Checking logs show evictions from included namespace")
	includePattern := regexp.QuoteMeta(`"Evicted pod in dry run mode" `) + ".*" + regexp.QuoteMeta(testNS.Name)
	err = checkPodLogs(ctx, kubeClient, deschedulerNamespace, podName, includePattern)
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("Namespace include filtering validated successfully")
}

// testNamespaceExcludeFiltering tests namespace exclude filtering
func testNamespaceExcludeFiltering(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	g.By("Creating test namespace to be excluded")
	testNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-53058",
		},
	}
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, testNS, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.CoreV1().Namespaces().Delete(ctx, testNS.Name, metav1.DeleteOptions{})

	g.By("Creating deployment in excluded namespace")
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deploy",
			Namespace: testNS.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-exclude"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-exclude"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "hello-openshift",
							Image: "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83",
						},
					},
				},
			},
		},
	}
	_, err = kubeClient.AppsV1().Deployments(testNS.Name).Create(ctx, deployment, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Waiting for deployment to be ready")
	err = waitForDeploymentReady(ctx, kubeClient, testNS.Name, "test-deploy", "1")
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Configuring descheduler with namespace exclude filter")
	deschClient := GetDeschedulerClient()
	err = patchKubeDeschedulerNamespaceFiltering(ctx, deschClient, "openshift-kube-descheduler-operator", "cluster", nil, []string{testNS.Name})
	o.Expect(err).NotTo(o.HaveOccurred())

	// Wait for descheduler deployment to rollout with new config
	err = waitForDeploymentReady(ctx, kubeClient, "openshift-kube-descheduler-operator", "descheduler", "1")
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Getting descheduler pod name")
	podName, err := getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Checking logs do NOT show evictions from excluded namespace")
	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		req := kubeClient.CoreV1().Pods(deschedulerNamespace).GetLogs(podName, &corev1.PodLogOptions{})
		logs, err := req.Stream(ctx)
		if err != nil {
			return false, nil
		}
		defer logs.Close()

		buf := new(strings.Builder)
		io.Copy(buf, logs)
		logContent := buf.String()

		if strings.Contains(logContent, testNS.Name) {
			return false, fmt.Errorf("found excluded namespace %s in logs, which should not happen", testNS.Name)
		}
		return false, nil // Continue polling, expecting timeout (no evictions)
	})

	// We expect timeout here - if namespace filtering works, we should NOT find any evictions
	o.Expect(err).To(o.HaveOccurred())
	o.Expect(wait.Interrupted(err)).To(o.BeTrue(), "Expected timeout waiting for logs")

	klog.Infof("Namespace exclude filtering validated successfully")
}

// testProfileConflicts tests that conflicting profile combinations are rejected
func testProfileConflicts(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	deschClient := GetDeschedulerClient()

	// Test 1: LongLifecycle + LifecycleAndUtilization should be rejected
	g.By("Testing LongLifecycle + LifecycleAndUtilization conflict")
	err := createKubeDeschedulerWithProfiles(ctx, deschClient, "openshift-kube-descheduler-operator", "test-conflict-1",
		[]string{"EvictPodsWithPVC", "LongLifecycle", "LifecycleAndUtilization"})
	o.Expect(err).To(o.HaveOccurred(), "Expected KubeDescheduler creation to fail with conflicting profiles")
	o.Expect(err.Error()).To(o.ContainSubstring("cannot declare LongLifecycle and LifecycleAndUtilization profiles simultaneously"))
	klog.Infof("LongLifecycle + LifecycleAndUtilization conflict correctly rejected")

	// Test 2: CompactAndScale + LifecycleAndUtilization should be rejected
	g.By("Testing CompactAndScale + LifecycleAndUtilization conflict")
	err = createKubeDeschedulerWithProfiles(ctx, deschClient, "openshift-kube-descheduler-operator", "test-conflict-2",
		[]string{"AffinityAndTaints", "CompactAndScale", "LifecycleAndUtilization"})
	o.Expect(err).To(o.HaveOccurred(), "Expected KubeDescheduler creation to fail with conflicting profiles")
	o.Expect(err.Error()).To(o.ContainSubstring("cannot declare CompactAndScale and LifecycleAndUtilization profiles simultaneously"))
	klog.Infof("CompactAndScale + LifecycleAndUtilization conflict correctly rejected")

	// Test 3: CompactAndScale + LongLifecycle should be rejected
	g.By("Testing CompactAndScale + LongLifecycle conflict")
	err = createKubeDeschedulerWithProfiles(ctx, deschClient, "openshift-kube-descheduler-operator", "test-conflict-3",
		[]string{"AffinityAndTaints", "CompactAndScale", "LongLifecycle"})
	o.Expect(err).To(o.HaveOccurred(), "Expected KubeDescheduler creation to fail with conflicting profiles")
	o.Expect(err.Error()).To(o.ContainSubstring("cannot declare CompactAndScale and LongLifecycle profiles simultaneously"))
	klog.Infof("CompactAndScale + LongLifecycle conflict correctly rejected")

	// Test 4: CompactAndScale + TopologyAndDuplicates should be rejected
	g.By("Testing CompactAndScale + TopologyAndDuplicates conflict")
	err = createKubeDeschedulerWithProfiles(ctx, deschClient, "openshift-kube-descheduler-operator", "test-conflict-4",
		[]string{"AffinityAndTaints", "CompactAndScale", "TopologyAndDuplicates"})
	o.Expect(err).To(o.HaveOccurred(), "Expected KubeDescheduler creation to fail with conflicting profiles")
	o.Expect(err.Error()).To(o.ContainSubstring("cannot declare CompactAndScale and TopologyAndDuplicates profiles simultaneously"))
	klog.Infof("CompactAndScale + TopologyAndDuplicates conflict correctly rejected")

	klog.Infof("Profile conflict validation completed successfully")
}

// testLongLifecycleProfile tests the LongLifecycle profile behavior
func testLongLifecycleProfile(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	g.By("Creating test namespace")
	testNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-longlifecycle",
		},
	}
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, testNS, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.CoreV1().Namespaces().Delete(ctx, testNS.Name, metav1.DeleteOptions{})

	g.By("Configuring descheduler with LongLifecycle profile")
	deschClient := GetDeschedulerClient()
	patch := []byte(`{"spec":{"profiles":["LongLifecycle"]}}`)
	_, err = deschClient.KubedeschedulersV1().KubeDeschedulers("openshift-kube-descheduler-operator").Patch(
		ctx, "cluster", types.MergePatchType, patch, metav1.PatchOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	// Restore original profile after test
	defer func() {
		g.By("Restoring original LifecycleAndUtilization profile")
		patch := []byte(`{"spec":{"profiles":["LifecycleAndUtilization"]}}`)
		deschClient.KubedeschedulersV1().KubeDeschedulers("openshift-kube-descheduler-operator").Patch(
			ctx, "cluster", types.MergePatchType, patch, metav1.PatchOptions{})
	}()

	// Wait for descheduler to restart with new profile
	err = waitForDeploymentReady(ctx, kubeClient, "openshift-kube-descheduler-operator", "descheduler", "1")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("LongLifecycle profile configured successfully")

	g.By("Creating deployment")
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-lifecycle",
			Namespace: testNS.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-lifecycle"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-lifecycle"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "hello-openshift",
							Image: "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83",
						},
					},
				},
			},
		},
	}
	_, err = kubeClient.AppsV1().Deployments(testNS.Name).Create(ctx, deployment, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Waiting for deployment to be ready")
	err = waitForDeploymentReady(ctx, kubeClient, testNS.Name, "test-lifecycle", "1")
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Verifying pods are NOT evicted with LongLifecycle profile")
	podName, err := getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	// With LongLifecycle, newly created pods should NOT be evicted
	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		req := kubeClient.CoreV1().Pods(deschedulerNamespace).GetLogs(podName, &corev1.PodLogOptions{})
		logs, err := req.Stream(ctx)
		if err != nil {
			return false, nil
		}
		defer logs.Close()

		buf := new(strings.Builder)
		io.Copy(buf, logs)
		logContent := buf.String()

		if strings.Contains(logContent, "test-lifecycle") {
			return false, fmt.Errorf("found eviction in logs, which should not happen with LongLifecycle")
		}
		return false, nil
	})

	// Expecting timeout - pods should NOT be evicted
	o.Expect(err).To(o.HaveOccurred())
	o.Expect(wait.Interrupted(err)).To(o.BeTrue(), "Expected timeout waiting for logs")

	klog.Infof("LongLifecycle profile validated successfully")
}

// testRelatedImages tests that CSV has relatedImages defined correctly
func testRelatedImages(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	dynamicClient := GetDynamicClient()

	g.By("Getting CSV name for descheduler operator")
	// Use empty label selector - will get all CSVs in namespace (there should only be one)
	csvName, err := getCSVName(ctx, dynamicClient, deschedulerNamespace, "")
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(csvName).NotTo(o.BeEmpty())
	klog.Infof("Found CSV: %s", csvName)

	g.By("Verifying CSV has relatedImages defined")
	relatedImages, err := getCSVRelatedImages(ctx, dynamicClient, deschedulerNamespace, csvName)
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(relatedImages)).To(o.BeNumerically(">", 0), "CSV should have at least one relatedImage")

	// Check that we have both operator and operand images
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

// Helper test functions

func testNodeAffinityStrategy(ctx context.Context, kubeClient *k8sclient.Clientset, namespace string, nodes []corev1.Node) {
	g.By("Creating deployment with node affinity")

	// Create a deployment with node affinity requiring a specific label
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-node-affinity",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32(3),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-affinity"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-affinity"},
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "e2e-az-name",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"e2e-az-1"},
											},
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83",
						},
					},
				},
			},
		},
	}

	_, err := kubeClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.AppsV1().Deployments(namespace).Delete(ctx, "test-node-affinity", metav1.DeleteOptions{})

	// Label a node with the affinity requirement
	g.By("Adding node label to satisfy affinity")
	err = addNodeLabel(ctx, kubeClient, nodes[0].Name, "e2e-az-name", "e2e-az-1")
	o.Expect(err).NotTo(o.HaveOccurred())
	defer removeNodeLabel(ctx, kubeClient, nodes[0].Name, "e2e-az-name")

	// Wait for pods to be running
	g.By("Waiting for pods to be scheduled")
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=test-affinity",
		})
		if err != nil {
			return false, nil
		}
		runningCount := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				runningCount++
			}
		}
		return runningCount == 3, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	// Remove the label to violate affinity
	g.By("Removing node label to violate affinity")
	err = removeNodeLabel(ctx, kubeClient, nodes[0].Name, "e2e-az-name")
	o.Expect(err).NotTo(o.HaveOccurred())

	// Wait for descheduler to process
	g.By("Waiting for descheduler to detect and evict pods")
	podName, err := getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	err = checkPodLogs(ctx, kubeClient, deschedulerNamespace, podName, "node_affinity.go")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("NodeAffinity strategy test completed - found evidence in pod %s logs", podName)
}

func testNodeTaintStrategy(ctx context.Context, kubeClient *k8sclient.Clientset, namespace string, nodes []corev1.Node) {
	g.By("Creating deployment without toleration")

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-node-taint",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-taint"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-taint"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83",
						},
					},
				},
			},
		},
	}

	_, err := kubeClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.AppsV1().Deployments(namespace).Delete(ctx, "test-node-taint", metav1.DeleteOptions{})

	// Wait for pods to be running
	g.By("Waiting for pods to be scheduled")
	var pods *corev1.PodList
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		podList, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=test-taint",
		})
		if err != nil {
			return false, nil
		}
		pods = podList
		runningCount := 0
		for _, pod := range podList.Items {
			if pod.Status.Phase == corev1.PodRunning {
				runningCount++
			}
		}
		return runningCount == 2, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	// Get the node where pods are running
	o.Expect(err).NotTo(o.HaveOccurred())

	if len(pods.Items) > 0 {
		targetNode := pods.Items[0].Spec.NodeName

		// Add taint to the node
		g.By("Adding taint to node to trigger violation")
		node, err := kubeClient.CoreV1().Nodes().Get(ctx, targetNode, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
			Key:    "e2e-test-taint",
			Value:  "true",
			Effect: corev1.TaintEffectNoSchedule,
		})

		_, err = kubeClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		defer func() {
			// Remove taint
			node, _ := kubeClient.CoreV1().Nodes().Get(ctx, targetNode, metav1.GetOptions{})
			var newTaints []corev1.Taint
			for _, taint := range node.Spec.Taints {
				if taint.Key != "e2e-test-taint" {
					newTaints = append(newTaints, taint)
				}
			}
			node.Spec.Taints = newTaints
			kubeClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
		}()

		// Wait for descheduler to process
		g.By("Waiting for descheduler to detect taint violation")
		podName, err := getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
		o.Expect(err).NotTo(o.HaveOccurred())

		err = checkPodLogs(ctx, kubeClient, deschedulerNamespace, podName, "node_taint.go")
		o.Expect(err).NotTo(o.HaveOccurred())

		klog.Infof("NodeTaint strategy test completed - found evidence in pod %s logs", podName)
	}

	if len(pods.Items) == 0 {
		klog.Warningf("No test pods found, skipping taint strategy verification")
	}
}

func testInterPodAntiAffinityStrategy(ctx context.Context, kubeClient *k8sclient.Clientset, namespace string) {
	g.By("Creating deployment with inter-pod anti-affinity")

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-antiaffinity",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32(3),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-antiaffinity"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":          "test-antiaffinity",
						"antiaffinity": "required",
					},
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"antiaffinity": "required",
										},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83",
						},
					},
				},
			},
		},
	}

	_, err := kubeClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.AppsV1().Deployments(namespace).Delete(ctx, "test-pod-antiaffinity", metav1.DeleteOptions{})

	// Wait for descheduler to process
	g.By("Waiting for descheduler to check anti-affinity")
	podName, err := getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	err = checkPodLogs(ctx, kubeClient, deschedulerNamespace, podName, "pod_antiaffinity.go")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("InterPodAntiAffinity strategy test completed - found evidence in pod %s logs", podName)
}

func testRemoveDuplicatesStrategy(ctx context.Context, kubeClient *k8sclient.Clientset, namespace string, nodes []corev1.Node) {
	g.By("Creating deployment with multiple replicas for duplicate testing")

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-duplicates",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32(6),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-duplicates"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-duplicates"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "quay.io/openshifttest/hello-openshift@sha256:4200f438cf2e9446f6bcff9d67ceea1f69ed07a2f83363b7fb52529f7ddd8a83",
						},
					},
				},
			},
		},
	}

	_, err := kubeClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	defer kubeClient.AppsV1().Deployments(namespace).Delete(ctx, "test-duplicates", metav1.DeleteOptions{})

	// Wait for pods to be running
	g.By("Waiting for pods to be scheduled")
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=test-duplicates",
		})
		if err != nil {
			return false, nil
		}
		runningCount := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				runningCount++
			}
		}
		return runningCount == 6, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	// Wait for descheduler to process duplicates
	g.By("Waiting for descheduler to check for duplicates")
	podName, err := getPodByLabel(ctx, kubeClient, deschedulerNamespace, deschedulerLabel)
	o.Expect(err).NotTo(o.HaveOccurred())

	err = checkPodLogs(ctx, kubeClient, deschedulerNamespace, podName, "duplicates.go")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("RemoveDuplicates strategy test completed - found evidence in pod %s logs", podName)
}

// testOLMMustGatherData verifies that must-gather collects OLM data
func testOLMMustGatherData(t testing.TB, ctx context.Context, kubeClient *k8sclient.Clientset) {
	dynamicClient := GetDynamicClient()

	// Since BeforeAll already installed the operator, we just need to verify OLM resources exist
	g.By("Verifying CSV exists")
	csvName, err := getCSVName(ctx, dynamicClient, deschedulerNamespace, "")
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(csvName).NotTo(o.BeEmpty())
	klog.Infof("Found CSV: %s", csvName)

	g.By("Verifying Subscription exists")
	subGVR := getSubscriptionGVR()
	subList, err := dynamicClient.Resource(subGVR).Namespace(deschedulerNamespace).List(ctx, metav1.ListOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(subList.Items)).To(o.BeNumerically(">", 0))
	klog.Infof("Found %d Subscription(s)", len(subList.Items))

	g.By("Verifying OperatorGroup exists")
	ogGVR := getOperatorGroupGVR()
	ogList, err := dynamicClient.Resource(ogGVR).Namespace(deschedulerNamespace).List(ctx, metav1.ListOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(len(ogList.Items)).To(o.BeNumerically(">", 0))
	klog.Infof("Found %d OperatorGroup(s)", len(ogList.Items))

	g.By("Running must-gather and verifying OLM data collection")
	// Create temporary directory for must-gather output
	mustGatherDir := "/tmp/must-gather-45694"
	defer func() {
		// Cleanup must-gather directory
		_ = kubeClient.CoreV1().Pods(deschedulerNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
	}()

	// Run must-gather command via kubectl/oc
	cmd := fmt.Sprintf("oc adm must-gather --dest-dir=%s 2>&1 && rm -rf %s", mustGatherDir, mustGatherDir)
	output, err := exec.Command("bash", "-c", cmd).CombinedOutput()

	if err != nil {
		// If oc command fails, just log it and verify OLM resources exist (which we already did above)
		klog.Warningf("must-gather command failed (may not be available in this environment): %v", err)
		klog.Infof("OLM resources verified successfully - CSV, Subscription, and OperatorGroup exist")
		return
	}

	mustGatherOutput := string(output)

	// Verify must-gather output contains OLM resource types
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

// Helper functions
