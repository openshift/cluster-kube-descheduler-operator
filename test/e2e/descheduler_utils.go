package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/drain"
	utilpointer "k8s.io/utils/pointer"
	"sigs.k8s.io/yaml"

	"github.com/google/go-cmp/cmp"
	operatorv1 "github.com/openshift/api/operator/v1"
	descv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	deschclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/profiles"
	olmlib "github.com/openshift/cluster-kube-descheduler-operator/test/e2e/olm"
	v1alpha2 "sigs.k8s.io/descheduler/pkg/api/v1alpha2"
)

// Helper functions for resource creation and management

func installOperatorWithSubscription(
	ctx context.Context,
	kubeClient *k8sclient.Clientset,
	deschClient *deschclient.Clientset,
	dynamicClient dynamic.Interface,
	deschedulerNamespace string,
) error {
	klog.Infof("Creating the operator namespace")
	namespaceObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: deschedulerNamespace,
		},
	}
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, namespaceObj, metav1.CreateOptions{})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}

	klog.Infof("Setting up OperatorGroup")
	og := &operatorsv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "descheduler-og",
			Namespace: namespaceObj.Name,
		},
		Spec: operatorsv1.OperatorGroupSpec{
			TargetNamespaces: []string{namespaceObj.Name},
		},
	}

	klog.Infof("Fetching subscription details from packagemanifest")
	sub, err := packagemanifestKDO(ctx, dynamicClient, "cluster-kube-descheduler-operator", namespaceObj.Name, []string{"redhat-operators"})
	if err != nil {
		return err
	}

	klog.Infof("Creating OperatorGroup")
	err = createOperatorGroup(ctx, dynamicClient, og)
	if err != nil {
		return err
	}

	klog.Infof("Creating Subscription")
	err = createSubscription(ctx, dynamicClient, sub)
	if err != nil {
		return err
	}

	klog.Infof("Waiting for descheduler operator deployment")
	err = waitForDeploymentReady(ctx, kubeClient, namespaceObj.Name, "descheduler-operator")
	o.Expect(err).NotTo(o.HaveOccurred())

	klog.Infof("Waiting for CSV to succeed")
	// Poll for CSV to be available and succeed
	var csvName string
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		name, err := getCSVName(ctx, dynamicClient, namespaceObj.Name, "")
		if err != nil {
			klog.V(2).Infof("CSV not yet available: %v", err)
			return false, nil
		}
		csvName = name
		return true, nil
	})
	if err != nil {
		return err
	}
	err = waitForCSVSucceeded(ctx, dynamicClient, namespaceObj.Name, csvName)
	if err != nil {
		return err
	}

	klog.Infof("Descheduler operator successfully installed via OLM, CSV: %s", csvName)

	// Create KubeDescheduler CR to deploy the operand
	// Matches OTP kubedescheduler_podlifetime.yaml configuration
	// Start in Predictive mode (dry-run) - tests can patch to Automatic if needed
	klog.Infof("Creating KubeDescheduler CR")
	kdCR := newDefaultKubeDescheduler()
	_, err = deschClient.KubedeschedulersV1().KubeDeschedulers(namespaceObj.Name).Create(ctx, kdCR, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	klog.Infof("Waiting for descheduler operand deployment")
	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := kubeClient.AppsV1().Deployments(namespaceObj.Name).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		return err == nil, nil
	})
	if err != nil {
		return err
	}

	err = waitForDeploymentReady(ctx, kubeClient, namespaceObj.Name, operatorclient.OperandName)
	if err != nil {
		return err
	}

	klog.Infof("Descheduler operand successfully deployed and running")
	return nil
}

// createOperatorGroup creates an OperatorGroup for the descheduler operator
func createOperatorGroup(ctx context.Context, dynamicClient dynamic.Interface, og *operatorsv1.OperatorGroup) error {
	klog.Infof("Creating OperatorGroup %s in namespace %s", og.Name, og.Namespace)

	// Convert typed OperatorGroup to unstructured for use with library-go
	unstructuredOG, err := runtime.DefaultUnstructuredConverter.ToUnstructured(og)
	if err != nil {
		return fmt.Errorf("failed to convert OperatorGroup to unstructured: %w", err)
	}

	// Set TypeMeta fields required by Kubernetes API
	u := &unstructured.Unstructured{Object: unstructuredOG}
	u.SetAPIVersion("operators.coreos.com/v1")
	u.SetKind("OperatorGroup")

	err = olmlib.CreateOperatorGroup(ctx, dynamicClient, u)
	if err != nil {
		return fmt.Errorf("failed to create OperatorGroup %s: %w", og.Name, err)
	}

	klog.Infof("Successfully created OperatorGroup %s", og.Name)
	return nil
}

// deleteOperatorGroup deletes the OperatorGroup
func deleteOperatorGroup(ctx context.Context, dynamicClient dynamic.Interface, og *operatorsv1.OperatorGroup) error {
	klog.Infof("Deleting OperatorGroup %s in namespace %s", og.Name, og.Namespace)

	// Convert typed OperatorGroup to unstructured for use with library-go
	unstructuredOG, err := runtime.DefaultUnstructuredConverter.ToUnstructured(og)
	if err != nil {
		return fmt.Errorf("failed to convert OperatorGroup to unstructured: %w", err)
	}

	// Set TypeMeta fields required by Kubernetes API
	u := &unstructured.Unstructured{Object: unstructuredOG}
	u.SetAPIVersion("operators.coreos.com/v1")
	u.SetKind("OperatorGroup")

	err = olmlib.DeleteOperatorGroup(ctx, dynamicClient, u)
	if err != nil {
		return fmt.Errorf("failed to delete OperatorGroup %s: %w", og.Name, err)
	}

	klog.Infof("Successfully deleted OperatorGroup %s", og.Name)
	return nil
}

// createSubscription creates a Subscription for the descheduler operator
func createSubscription(ctx context.Context, dynamicClient dynamic.Interface, sub *operatorsv1alpha1.Subscription) error {
	klog.Infof("Creating Subscription %s in namespace %s", sub.Name, sub.Namespace)

	// Convert typed Subscription to unstructured for use with library-go
	unstructuredSub, err := runtime.DefaultUnstructuredConverter.ToUnstructured(sub)
	if err != nil {
		return fmt.Errorf("failed to convert Subscription to unstructured: %w", err)
	}

	// Set TypeMeta fields required by Kubernetes API
	u := &unstructured.Unstructured{Object: unstructuredSub}
	u.SetAPIVersion("operators.coreos.com/v1alpha1")
	u.SetKind("Subscription")

	err = olmlib.CreateSubscription(ctx, dynamicClient, u)
	if err != nil {
		return fmt.Errorf("failed to create Subscription %s: %w", sub.Name, err)
	}

	klog.Infof("Successfully created Subscription %s", sub.Name)
	return nil
}

// deleteSubscription deletes the Subscription
func deleteSubscription(ctx context.Context, dynamicClient dynamic.Interface, sub *operatorsv1alpha1.Subscription) error {
	klog.Infof("Deleting Subscription %s in namespace %s", sub.Name, sub.Namespace)

	// Convert typed Subscription to unstructured for use with library-go
	unstructuredSub, err := runtime.DefaultUnstructuredConverter.ToUnstructured(sub)
	if err != nil {
		return fmt.Errorf("failed to convert Subscription to unstructured: %w", err)
	}

	// Set TypeMeta fields required by Kubernetes API
	u := &unstructured.Unstructured{Object: unstructuredSub}
	u.SetAPIVersion("operators.coreos.com/v1alpha1")
	u.SetKind("Subscription")

	err = olmlib.DeleteSubscription(ctx, dynamicClient, u)
	if err != nil {
		return fmt.Errorf("failed to delete Subscription %s: %w", sub.Name, err)
	}

	klog.Infof("Successfully deleted Subscription %s", sub.Name)
	return nil
}

// createKubeDeschedulerWithProfiles attempts to create a KubeDescheduler CR with specified profiles
// Returns error if creation fails (which is expected for conflicting profiles)
func createKubeDeschedulerWithProfiles(ctx context.Context, deschClient *deschclient.Clientset, name string, profiles []string) error {
	// Convert string profiles to DeschedulerProfile type
	deschProfiles := make([]descv1.DeschedulerProfile, len(profiles))
	for i, p := range profiles {
		deschProfiles[i] = descv1.DeschedulerProfile(p)
	}

	kdCR := &descv1.KubeDescheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: operatorclient.OperatorNamespace,
		},
		Spec: descv1.KubeDeschedulerSpec{
			Profiles: deschProfiles,
			Mode:     descv1.Predictive,
		},
	}

	_, err := deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Create(ctx, kdCR, metav1.CreateOptions{})
	return err
}

// deleteKubeDescheduler deletes a KubeDescheduler CR
func deleteKubeDescheduler(ctx context.Context, deschClient *deschclient.Clientset, namespace, name string) error {
	err := deschClient.KubedeschedulersV1().KubeDeschedulers(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		klog.Warningf("Failed to delete KubeDescheduler %s/%s: %v", namespace, name, err)
	}
	return err
}

// buildKubeDescheduler creates a KubeDescheduler CR with minimal base configuration
// and allows customization via a modifier function. Profiles and ProfileCustomizations are left
// empty and must be set explicitly via the modifier function.
func buildKubeDescheduler(modify func(*descv1.KubeDescheduler)) *descv1.KubeDescheduler {
	kd := &descv1.KubeDescheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorclient.OperatorConfigName,
			Namespace: operatorclient.OperatorNamespace,
		},
		Spec: descv1.KubeDeschedulerSpec{
			OperatorSpec: operatorv1.OperatorSpec{
				ManagementState: operatorv1.Managed,
			},
			DeschedulingIntervalSeconds: utilpointer.Int32(30),
			Mode:                        descv1.Predictive,
			EvictionLimits: &descv1.EvictionLimits{
				Total: utilpointer.Int32(4),
			},
		},
	}

	// Apply custom modifications if provided
	if modify != nil {
		modify(kd)
	}

	return kd
}

// createAndValidateKubeDeschedulerCR creates a KubeDescheduler CR, validates the generated policy,
// and waits for operand stability. This helper combines the common pattern used across profile tests.
func createAndValidateKubeDeschedulerCR(ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset, kubeDescheduler *descv1.KubeDescheduler, description string) error {
	g.By(fmt.Sprintf("Creating new KubeDescheduler CR with %s", description))
	err := createKubeDeschedulerAndWait(ctx, kubeClient, deschClient, kubeDescheduler)
	if err != nil {
		return err
	}

	g.By("Validating operator-generated descheduling policy matches expected policy")
	err = validateDeschedulingPolicy(ctx, kubeClient, kubeDescheduler)
	if err != nil {
		return err
	}

	g.By("Waiting for descheduler operand to run stably for 30 seconds")
	err = waitForOperandStability(ctx, kubeClient, 30*time.Second)
	if err != nil {
		return err
	}
	return nil
}

// newDefaultKubeDescheduler creates a KubeDescheduler CR with default configuration
// This matches the default CR created in installOperatorWithSubscription
func newDefaultKubeDescheduler() *descv1.KubeDescheduler {
	return buildKubeDescheduler(func(kd *descv1.KubeDescheduler) {
		kd.Spec.Profiles = []descv1.DeschedulerProfile{descv1.LifecycleAndUtilization}
		kd.Spec.ProfileCustomizations = &descv1.ProfileCustomizations{
			PodLifetime: &metav1.Duration{Duration: 10 * time.Second},
		}
	})
}

// createKubeDeschedulerAndWait creates a KubeDescheduler CR and waits for the operand deployment to be ready
func createKubeDeschedulerAndWait(ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset, kubeDescheduler *descv1.KubeDescheduler) error {
	// Create the KubeDescheduler CR
	klog.Infof("Creating KubeDescheduler CR %s/%s", operatorclient.OperatorNamespace, operatorclient.OperatorConfigName)
	_, err := deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Create(ctx, kubeDescheduler, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create KubeDescheduler CR: %w", err)
	}

	// Wait for descheduler deployment to be ready
	klog.Infof("Waiting for descheduler deployment to be ready")
	err = waitForDeploymentReady(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName)
	if err != nil {
		return fmt.Errorf("timeout waiting for descheduler deployment to be ready: %w", err)
	}
	return nil
}

// kubeDeschedulerSpecIsDefault reports whether spec matches the default test CR.
// Observed config is written by the operator and is ignored. Empty log levels match
// the API default of Normal.
func kubeDeschedulerSpecIsDefault(spec descv1.KubeDeschedulerSpec) bool {
	desired := newDefaultKubeDescheduler().Spec
	normalize := func(s *descv1.KubeDeschedulerSpec) {
		s.ObservedConfig = runtime.RawExtension{}
		s.UnsupportedConfigOverrides = runtime.RawExtension{}
		if s.LogLevel == "" {
			s.LogLevel = operatorv1.Normal
		}
		if s.OperatorLogLevel == "" {
			s.OperatorLogLevel = operatorv1.Normal
		}
	}
	normalize(&spec)
	normalize(&desired)
	return apiequality.Semantic.DeepEqual(spec, desired)
}

// ensureDefaultKubeDescheduler leaves a default CR in place. A non-default CR is
// deleted, the operand ConfigMap is waited out, and the default CR is created.
// A missing CR is created as the default without a delete.
func ensureDefaultKubeDescheduler(ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) error {
	current, err := deschClient.KubedeschedulersV1().KubeDeschedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		klog.Infof("KubeDescheduler CR not found, creating default")
		return createKubeDeschedulerAndWait(ctx, kubeClient, deschClient, newDefaultKubeDescheduler())
	}
	if err != nil {
		return fmt.Errorf("failed to get KubeDescheduler CR: %w", err)
	}
	if kubeDeschedulerSpecIsDefault(current.Spec) {
		klog.Infof("KubeDescheduler CR already has the default spec, leaving it in place")
		return nil
	}

	klog.Infof("KubeDescheduler CR spec is not default, deleting it before creating the default")
	if err := deleteKubeDeschedulerAndWait(ctx, kubeClient, deschClient); err != nil {
		return err
	}
	if err := waitForConfigMapDeletion(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperatorConfigName); err != nil {
		return fmt.Errorf("timed out waiting for ConfigMap deletion before creating default CR: %w", err)
	}
	return createKubeDeschedulerAndWait(ctx, kubeClient, deschClient, newDefaultKubeDescheduler())
}

// deleteKubeDeschedulerAndWait deletes a KubeDescheduler CR and waits for the operand deployment to be gone
func deleteKubeDeschedulerAndWait(ctx context.Context, kubeClient *k8sclient.Clientset, deschClient *deschclient.Clientset) error {
	// Delete the KubeDescheduler CR
	err := deleteKubeDescheduler(ctx, deschClient, operatorclient.OperatorNamespace, operatorclient.OperatorConfigName)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return err
	}

	// Wait for descheduler deployment to be deleted
	klog.Infof("Waiting for descheduler deployment to be deleted")
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				klog.V(4).Info("Descheduler deployment deleted")
				return true, nil
			}
			klog.V(2).Infof("Error getting deployment: %v", err)
			return false, nil
		}
		klog.V(4).Info("Descheduler deployment still exists")
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("timeout waiting for descheduler deployment to be deleted: %w", err)
	}

	klog.Infof("Descheduler deployment successfully deleted")
	return nil
}

// patchKubeDeschedulerMode patches the KubeDescheduler CR mode
func patchKubeDeschedulerMode(ctx context.Context, deschClient *deschclient.Clientset, namespace, name, mode string) error {
	// Get current CR
	kdCR, err := deschClient.KubedeschedulersV1().KubeDeschedulers(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get KubeDescheduler CR: %w", err)
	}

	// Update mode
	kdCR.Spec.Mode = descv1.Mode(mode)

	// Update the CR
	_, err = deschClient.KubedeschedulersV1().KubeDeschedulers(namespace).Update(ctx, kdCR, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch KubeDescheduler mode to %s: %w", mode, err)
	}

	klog.Infof("Successfully patched KubeDescheduler mode to %s", mode)
	return nil
}

// patchKubeDeschedulerNamespaceFiltering patches the KubeDescheduler CR with namespace filtering and optionally sets profiles
func patchKubeDeschedulerNamespaceFiltering(ctx context.Context, deschClient *deschclient.Clientset, namespace, name string, included, excluded []string, profiles []descv1.DeschedulerProfile) error {
	// Get current CR
	kdCR, err := deschClient.KubedeschedulersV1().KubeDeschedulers(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get KubeDescheduler CR: %w", err)
	}

	// Set profiles if provided
	if len(profiles) > 0 {
		kdCR.Spec.Profiles = profiles
	}

	// Initialize profileCustomizations if nil
	if kdCR.Spec.ProfileCustomizations == nil {
		kdCR.Spec.ProfileCustomizations = &descv1.ProfileCustomizations{}
	}

	// Set included/excluded namespaces
	// Namespaces is a struct, not a pointer, so we directly update its fields
	if len(included) > 0 {
		kdCR.Spec.ProfileCustomizations.Namespaces.Included = included
		kdCR.Spec.ProfileCustomizations.Namespaces.Excluded = nil
	}
	if len(excluded) > 0 {
		kdCR.Spec.ProfileCustomizations.Namespaces.Excluded = excluded
		kdCR.Spec.ProfileCustomizations.Namespaces.Included = nil
	}

	// Update the CR
	_, err = deschClient.KubedeschedulersV1().KubeDeschedulers(namespace).Update(ctx, kdCR, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch KubeDescheduler namespace filtering: %w", err)
	}

	klog.Infof("Successfully patched KubeDescheduler (profiles: %v, included: %v, excluded: %v)", profiles, included, excluded)
	return nil
}

// waitForDeploymentReady waits for a deployment to have the expected number of ready replicas
func waitForDeploymentReady(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, name string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		deployment, err := kubeClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			klog.Warningf("Failed to get deployment %s/%s: %v", namespace, name, err)
			return false, nil
		}

		if deployment.Spec.Replicas == nil {
			return false, fmt.Errorf("deployment %s/%s has nil Spec.Replicas", namespace, name)
		}

		if deployment.Status.ReadyReplicas >= *deployment.Spec.Replicas {
			klog.Infof("Deployment %s/%s is ready with %d replicas", namespace, name, deployment.Status.ReadyReplicas)
			return true, nil
		}

		klog.Infof("Waiting for deployment %s/%s: %d/%d replicas ready",
			namespace, name, deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
		return false, nil
	})
}

// checkPodLogs checks if pod logs contain the expected pattern
func checkPodLogs(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, podName, expectedPattern string) error {
	// Keep track of current pod name in case it gets recreated during polling
	currentPodName := podName

	return wait.PollUntilContextTimeout(ctx, 15*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		req := kubeClient.CoreV1().Pods(namespace).GetLogs(currentPodName, &corev1.PodLogOptions{})
		logs, err := req.Stream(ctx)
		if err != nil {
			// If pod not found, try to get the current descheduler pod (it may have been recreated)
			if strings.Contains(err.Error(), "not found") {
				klog.Infof("Pod %s not found, attempting to get current descheduler pod", currentPodName)
				newPodName, lookupErr := getPodByLabel(ctx, kubeClient, namespace, deschedulerLabel)
				if lookupErr != nil {
					klog.Warningf("Failed to lookup current pod: %v", lookupErr)
					return false, nil
				}
				if newPodName != currentPodName {
					klog.Infof("Found new descheduler pod: %s (old: %s)", newPodName, currentPodName)
					currentPodName = newPodName
					// Retry with new pod name
					req = kubeClient.CoreV1().Pods(namespace).GetLogs(currentPodName, &corev1.PodLogOptions{})
					logs, err = req.Stream(ctx)
					if err != nil {
						klog.Warningf("Failed to get logs for pod %s/%s: %v", namespace, currentPodName, err)
						return false, nil
					}
				} else {
					klog.Warningf("Failed to get logs for pod %s/%s: %v", namespace, currentPodName, err)
					return false, nil
				}
			} else {
				klog.Warningf("Failed to get logs for pod %s/%s: %v", namespace, currentPodName, err)
				return false, nil
			}
		}
		defer logs.Close()

		buf := new(strings.Builder)
		_, err = io.Copy(buf, logs)
		if err != nil {
			klog.Warningf("Failed to read logs: %v", err)
			return false, nil
		}

		logContent := buf.String()
		matched, _ := regexp.MatchString(expectedPattern, logContent)
		if matched {
			klog.Infof("Found expected pattern in logs: %s", expectedPattern)
			return true, nil
		}

		klog.Infof("Pattern not found yet in logs: %s", expectedPattern)
		return false, nil
	})
}

// getPodByLabel gets a pod by label selector
func getPodByLabel(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, labelSelector string) (string, error) {
	pods, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", err
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found with label selector %s", labelSelector)
	}

	// Return the first running pod
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Name, nil
		}
	}

	return "", fmt.Errorf("no running pods found with label selector %s", labelSelector)
}

// waitForOperandStability waits for the descheduler operand pod to be running stably without restarts or errors
// for the specified duration by checking pod age
func waitForOperandStability(ctx context.Context, kubeClient *k8sclient.Clientset, duration time.Duration) error {
	klog.Infof("Waiting for descheduler operand to run stably for %v", duration)

	// increasing the duration in case the pod gets recreated during the waiting
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, duration+20*time.Second, false, func(ctx context.Context) (bool, error) {
		// Get the current pod by label (pod name may change during the check)
		podName, err := getPodByLabel(ctx, kubeClient, operatorclient.OperatorNamespace, deschedulerLabel)
		if err != nil {
			klog.V(4).Infof("Failed to get descheduler pod: %v, retrying...", err)
			return false, nil
		}

		// Get pod details
		pod, err := kubeClient.CoreV1().Pods(operatorclient.OperatorNamespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			klog.V(4).Infof("Failed to get pod %s: %v, retrying...", podName, err)
			return false, nil
		}

		// Check if pod is running
		if pod.Status.Phase != corev1.PodRunning {
			klog.V(4).Infof("Pod %s is not running (phase: %s), waiting...", podName, pod.Status.Phase)
			return false, nil
		}

		// Check for container issues
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.State.Waiting != nil {
				klog.V(4).Infof("Container %s is waiting: %s - %s",
					containerStatus.Name,
					containerStatus.State.Waiting.Reason,
					containerStatus.State.Waiting.Message)
				return false, nil
			}
			if containerStatus.State.Terminated != nil {
				klog.V(4).Infof("Container %s is terminated: %s - %s",
					containerStatus.Name,
					containerStatus.State.Terminated.Reason,
					containerStatus.State.Terminated.Message)
				return false, nil
			}
		}

		// Check pod age - if the pod has been running for longer than the required duration, we're done
		podAge := time.Since(pod.CreationTimestamp.Time)
		klog.Infof("Operand pod has been running for %v", podAge)

		if podAge >= duration {
			klog.Infof("Descheduler operand pod %s has been running stably for %v (age: %v)", podName, duration, podAge)
			return true, nil
		}

		klog.V(4).Infof("Pod %s is stable but not old enough yet - age=%v, required=%v",
			podName, podAge, duration)
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("timeout waiting for descheduler operand to run stably for %v: %w", duration, err)
	}

	return nil
}

// getCSVName gets the CSV name for the operator using label selector
func getCSVName(ctx context.Context, dynamicClient dynamic.Interface, namespace, labelSelector string) (string, error) {
	// Updated API: GetCSVName is now GetTheLatestCSVName
	csvName, err := olmlib.GetTheLatestCSVName(ctx, dynamicClient, namespace, labelSelector)
	if err != nil {
		return "", fmt.Errorf("failed to list CSVs: %w", err)
	}

	klog.Infof("Found CSV: %s", csvName)
	return csvName, nil
}

// RelatedImage represents an image referenced in a CSV
type RelatedImage struct {
	Name  string
	Image string
}

// getCSVRelatedImages gets the relatedImages from a CSV
func getCSVRelatedImages(ctx context.Context, dynamicClient dynamic.Interface, namespace, csvName string) ([]RelatedImage, error) {
	// Get the CSV as an unstructured object
	csvUnstructured, err := dynamicClient.Resource(olmlib.CSVGVR()).Namespace(namespace).Get(ctx, csvName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get CSV %s: %w", csvName, err)
	}

	// Updated API: GetCSVRelatedImages now takes an unstructured CSV object
	libImages, err := olmlib.GetCSVRelatedImages(csvUnstructured)
	if err != nil {
		return nil, fmt.Errorf("failed to get CSV related images: %w", err)
	}

	// Convert from library-go RelatedImage to local RelatedImage type
	images := make([]RelatedImage, len(libImages))
	for i, img := range libImages {
		images[i] = RelatedImage{
			Name:  img.Name,
			Image: img.Image,
		}
	}

	klog.Infof("Found %d related images in CSV %s", len(images), csvName)
	return images, nil
}

// waitForCSVSucceeded waits for CSV to reach Succeeded phase
func waitForCSVSucceeded(ctx context.Context, dynamicClient dynamic.Interface, namespace, csvName string) error {
	klog.Infof("Waiting for CSV %s/%s to succeed", namespace, csvName)

	// The library-go no longer provides WaitForCSVSucceeded, so we implement it here
	err := wait.PollUntilContextTimeout(ctx, 10*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		csv, err := dynamicClient.Resource(olmlib.CSVGVR()).Namespace(namespace).Get(ctx, csvName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				klog.Infof("CSV %s not found yet, waiting...", csvName)
				return false, nil
			}
			return false, err
		}

		// Get the phase from status
		phase, found, err := unstructured.NestedString(csv.Object, "status", "phase")
		if err != nil || !found {
			klog.Infof("CSV %s phase not yet available, waiting...", csvName)
			return false, nil
		}

		klog.Infof("CSV %s current phase: %s", csvName, phase)

		if phase == "Succeeded" {
			return true, nil
		}

		if phase == "Failed" {
			return false, fmt.Errorf("CSV %s failed", csvName)
		}

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("failed waiting for CSV to succeed: %w", err)
	}

	klog.Infof("CSV %s succeeded", csvName)
	return nil
}

// packagemanifestKDO fetches packagemanifest values dynamically for kube-descheduler-operator
func packagemanifestKDO(ctx context.Context, dynamicClient dynamic.Interface, packageName, namespace string, catalogNames []string) (*operatorsv1alpha1.Subscription, error) {
	klog.Infof("Fetching packagemanifest values for %s", packageName)

	// Updated API: BuildSubscriptionFromPackageManifest now returns *unstructured.Unstructured
	unstructuredSub, err := olmlib.BuildSubscriptionFromPackageManifest(ctx, dynamicClient, packageName, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get packagemanifest %s: %w", packageName, err)
	}

	// Convert unstructured Subscription back to typed object
	sub := &operatorsv1alpha1.Subscription{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredSub.Object, sub)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Subscription from unstructured: %w", err)
	}

	// The unstructured Subscription uses "package" for the package name field,
	// but the typed Subscription struct maps Package to JSON tag "name".
	// FromUnstructured looks for "name" in spec and finds nothing, leaving
	// Spec.Package empty. Set it explicitly from the known packageName.
	if sub.Spec.Package == "" {
		sub.Spec.Package = packageName
	}

	klog.Infof("Found package manifest: channel=%s, source=%s, startingCSV=%s",
		sub.Spec.Channel, sub.Spec.CatalogSource, sub.Spec.StartingCSV)

	return sub, nil
}

// cordonNode cordons a node using CordonHelper from kubectl/drain
func cordonNode(ctx context.Context, kubeClient *k8sclient.Clientset, node *corev1.Node) error {
	cordonHelper := drain.NewCordonHelper(node)
	if cordonHelper.UpdateIfRequired(true) {
		err, patchErr := cordonHelper.PatchOrReplaceWithContext(ctx, kubeClient, false)
		if err != nil {
			return fmt.Errorf("failed to cordon node %s: %w", node.Name, err)
		}
		if patchErr != nil {
			klog.Warningf("Failed to create patch for cordoning node %s, but Update succeeded: %v", node.Name, patchErr)
		}
		klog.Infof("Cordoned node %s", node.Name)
	} else {
		klog.Infof("Node %s is already cordoned", node.Name)
	}
	return nil
}

// uncordonNode uncordons a node using CordonHelper from kubectl/drain
func uncordonNode(ctx context.Context, kubeClient *k8sclient.Clientset, node *corev1.Node) error {
	cordonHelper := drain.NewCordonHelper(node)
	if cordonHelper.UpdateIfRequired(false) {
		err, patchErr := cordonHelper.PatchOrReplaceWithContext(ctx, kubeClient, false)
		if err != nil {
			return fmt.Errorf("failed to uncordon node %s: %w", node.Name, err)
		}
		if patchErr != nil {
			klog.Warningf("Failed to create patch for uncordoning node %s, but Update succeeded: %v", node.Name, patchErr)
		}
		klog.Infof("Uncordoned node %s", node.Name)
	} else {
		klog.Infof("Node %s is already uncordoned", node.Name)
	}
	return nil
}

// addNodeLabel adds a label to a node
func addNodeLabel(ctx context.Context, kubeClient *k8sclient.Clientset, node *corev1.Node, key, value string) error {
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels[key] = value

	updatedNode, err := kubeClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to add label to node %s: %w", node.Name, err)
	}
	// Update the node object with the server's response to keep it in sync
	*node = *updatedNode
	klog.Infof("Added label %s=%s to node %s", key, value, node.Name)
	return nil
}

// removeNodeLabel removes a label from a node
func removeNodeLabel(ctx context.Context, kubeClient *k8sclient.Clientset, node *corev1.Node, key string) error {
	delete(node.Labels, key)

	updatedNode, err := kubeClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to remove label from node %s: %w", node.Name, err)
	}
	// Update the node object with the server's response to keep it in sync
	*node = *updatedNode
	klog.Infof("Removed label %s from node %s", key, node.Name)
	return nil
}

// RawExtensionToUnstructured converts a RawExtension to unstructured map
// Handles both RawExtension.Object (if populated) and RawExtension.Raw (JSON bytes)
func RawExtensionToUnstructured(ext runtime.RawExtension) (map[string]interface{}, error) {
	var result map[string]interface{}

	if ext.Object != nil {
		// Object is populated, convert it
		var err error
		result, err = runtime.DefaultUnstructuredConverter.ToUnstructured(ext.Object)
		if err != nil {
			return nil, fmt.Errorf("failed to convert RawExtension.Object to unstructured: %w", err)
		}
	} else if len(ext.Raw) > 0 {
		// Object is nil, unmarshal from Raw JSON bytes
		if err := json.Unmarshal(ext.Raw, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal RawExtension.Raw: %w", err)
		}
	} else {
		// No data available
		return nil, fmt.Errorf("RawExtension has neither Object nor Raw")
	}

	return result, nil
}

// getDeschedulerPolicyFromConfigMap reads the descheduler ConfigMap and returns the DeschedulerPolicy object
// validateDeschedulingPolicy validates that the operator-generated policy matches the expected policy
// from profiles.BuildDeschedulingPolicy
func validateDeschedulingPolicy(ctx context.Context, kubeClient *k8sclient.Clientset, kubeDescheduler *descv1.KubeDescheduler) error {
	klog.Infof("Validating operator-generated descheduling policy")

	// Build protected namespaces list once (matching operator logic in target_config_reconciler.go)
	protectedNamespaces := []string{"kube-system", "hypershift", "openshift"}
	allNamespaces, err := kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w", err)
	}
	for _, ns := range allNamespaces.Items {
		if strings.HasPrefix(ns.Name, "openshift-") {
			protectedNamespaces = append(protectedNamespaces, ns.Name)
		}
	}

	// Build expected policy once using profiles.BuildDeschedulingPolicy
	expectedPolicy, err := profiles.BuildDeschedulingPolicy(kubeDescheduler, protectedNamespaces, "")
	if err != nil {
		return fmt.Errorf("failed to build expected policy: %w", err)
	}

	// Normalize expected policy once
	normalizedExpected, err := normalizeDeschedulerPolicy(expectedPolicy)
	if err != nil {
		return fmt.Errorf("failed to normalize expected policy: %w", err)
	}

	// This prevents policy mismatch when tests run in sequence where previous test's ConfigMap
	// cleanup may still be in progress. We actively wait for the new ConfigMap to be created.
	klog.Infof("Waiting for operator to create/update ConfigMap (grace period)...")
	graceCtx, graceCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer graceCancel()
	err = wait.PollUntilContextTimeout(graceCtx, 1*time.Second, 2*time.Minute, true, func(pollCtx context.Context) (bool, error) {
		_, err := kubeClient.CoreV1().ConfigMaps(operatorclient.OperatorNamespace).Get(pollCtx, operatorclient.OperatorConfigName, metav1.GetOptions{})
		if err == nil {
			klog.Infof("ConfigMap exists - operator reconciliation in progress")
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		klog.Warningf("Timeout waiting for ConfigMap creation (will retry in policy validation): %v", err)
	}

	// Poll until actual policy matches expected
	policyCtx, policyCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer policyCancel()

	startPolicyCheck := time.Now()
	attemptCount := 0
	err = wait.PollUntilContextTimeout(policyCtx, 5*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		elapsed := time.Since(startPolicyCheck)
		attemptCount++

		// Log every 30 seconds to show progress
		if attemptCount%6 == 1 {
			klog.Infof("Policy validation attempt %d (elapsed: %v)", attemptCount, elapsed)
		}

		// Get actual policy from ConfigMap
		actualPolicy, err := getDeschedulerPolicyFromConfigMap(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperatorConfigName)
		if err != nil {
			klog.V(2).Infof("Failed to get DeschedulerPolicy from ConfigMap (attempt %d, elapsed: %v): %v", attemptCount, elapsed, err)
			return false, nil
		}

		// Normalize actual policy to ensure consistent comparison with RawExtension formatting
		normalizedActual, err := normalizeDeschedulerPolicy(actualPolicy)
		if err != nil {
			klog.V(2).Infof("Failed to normalize actual policy (attempt %d, elapsed: %v): %v", attemptCount, elapsed, err)
			return false, nil
		}

		// Compare full normalized policies (including profile names, plugins, and arguments)
		if diff := cmp.Diff(normalizedExpected, normalizedActual); diff != "" {
			if attemptCount%6 == 1 { // Log every 30 seconds
				klog.V(4).Infof("Policy mismatch (attempt %d, elapsed: %v), retrying...", attemptCount, elapsed)
			}
			klog.V(4).Infof("Policy mismatch (-expected +actual):\n%s", diff)
			return false, nil
		}

		klog.V(4).Infof("Policy validation SUCCEEDED after %d attempts, %v total elapsed", attemptCount, elapsed)
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("timeout waiting for descheduling policy to match expected: %w", err)
	}

	klog.Infof("Descheduling policy validated successfully")
	return nil
}

// normalizeDeschedulerPolicy normalizes a DeschedulerPolicy by marshaling and unmarshaling through YAML
// This ensures consistent RawExtension representation (Raw vs Object) for comparison
func normalizeDeschedulerPolicy(policy *v1alpha2.DeschedulerPolicy) (*v1alpha2.DeschedulerPolicy, error) {
	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal policy to YAML: %w", err)
	}

	// Unmarshal back to DeschedulerPolicy
	normalized := &v1alpha2.DeschedulerPolicy{}
	err = yaml.Unmarshal(yamlBytes, normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal normalized policy: %w", err)
	}

	// System-protected namespaces that are created/deleted during test execution
	// These should be stripped from comparison as they may differ between test runs
	systemProtectedNamespaces := map[string]bool{
		"openshift-kube-descheduler-operator": true,
		"openshift-kube-apiserver":            true,
		"openshift-kube-controller-manager":   true,
		"openshift-kube-scheduler":            true,
		"openshift-etcd":                      true,
		"openshift-monitoring":                true,
		"openshift":                           true,
	}

	// Normalize RawExtension fields to ensure consistent JSON encoding
	// RawExtension bytes might have different formatting (whitespace, ordering) even if semantically identical
	for profileIdx := range normalized.Profiles {
		profile := &normalized.Profiles[profileIdx]
		for pluginConfigIdx := range profile.PluginConfigs {
			pluginConfig := &profile.PluginConfigs[pluginConfigIdx]
			// Normalize the Args RawExtension by parsing and re-marshaling as JSON
			if len(pluginConfig.Args.Raw) > 0 {
				var args map[string]interface{}
				if err := json.Unmarshal(pluginConfig.Args.Raw, &args); err == nil {
					// Strip only system-protected namespaces from comparison:
					// The operator caches its protectedNamespaces list at startup,
					// so the excluded namespace set in its policy may differ from
					// what the test observes at validation time. User-defined
					// namespaces should still be compared.
					if namespacesRaw, ok := args["namespaces"]; ok {
						if namespacesList, ok := namespacesRaw.([]interface{}); ok {
							var filteredNamespaces []string
							for _, ns := range namespacesList {
								if nsStr, ok := ns.(string); ok && !systemProtectedNamespaces[nsStr] {
									filteredNamespaces = append(filteredNamespaces, nsStr)
								}
							}
							if len(filteredNamespaces) == 0 {
								delete(args, "namespaces")
							} else {
								args["namespaces"] = filteredNamespaces
							}
						}
					}
					normalizedBytes, err := json.Marshal(args)
					if err == nil {
						pluginConfig.Args.Raw = normalizedBytes
					}
				}
			}
		}
	}

	return normalized, nil
}

func getDeschedulerPolicyFromConfigMap(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, configMapName string) (*v1alpha2.DeschedulerPolicy, error) {
	klog.V(4).Infof("Getting DeschedulerPolicy from ConfigMap %s/%s", namespace, configMapName)

	// Get the ConfigMap
	cm, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, configMapName, err)
	}

	// Get the policy.yaml content
	policyYAML, ok := cm.Data["policy.yaml"]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %s/%s does not contain 'policy.yaml' key", namespace, configMapName)
	}

	klog.V(4).Infof("ConfigMap policy.yaml content:\n%s", policyYAML)

	// Unmarshal YAML into DeschedulerPolicy object
	policy := &v1alpha2.DeschedulerPolicy{}
	err = yaml.Unmarshal([]byte(policyYAML), policy)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal policy.yaml into DeschedulerPolicy: %w", err)
	}

	klog.V(4).Infof("Successfully parsed DeschedulerPolicy from ConfigMap with %d profiles", len(policy.Profiles))
	return policy, nil
}

// ensureNamespaceMonitoringLabel ensures namespace has cluster-monitoring label for Prometheus scraping
func ensureNamespaceMonitoringLabel(ctx context.Context, kubeClient *k8sclient.Clientset, namespace string) error {
	const labelKey = "openshift.io/cluster-monitoring"
	const labelValue = "true"

	ns, err := kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get namespace %s: %w", namespace, err)
	}

	// Check if label already exists
	if ns.Labels != nil && ns.Labels[labelKey] == labelValue {
		klog.Infof("Namespace %s already has monitoring label %s=%s", namespace, labelKey, labelValue)
		return nil
	}

	// Add label
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels[labelKey] = labelValue

	_, err = kubeClient.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to add monitoring label to namespace %s: %w", namespace, err)
	}

	klog.Infof("Added monitoring label to namespace %s", namespace)

	// Verify label was applied
	nsVerify, err := kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to verify label on namespace %s: %w", namespace, err)
	}

	if nsVerify.Labels[labelKey] != labelValue {
		return fmt.Errorf("label verification failed: expected %s=%s, got %s", labelKey, labelValue, nsVerify.Labels[labelKey])
	}

	klog.Infof("✓ Verified: namespace %s has monitoring label %s=%s", namespace, labelKey, labelValue)
	return nil
}

// waitForConfigMapDeletion waits for operator ConfigMap to be deleted
func waitForConfigMapDeletion(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, name string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil && strings.Contains(err.Error(), "not found") {
			klog.Infof("ConfigMap cleaned up - preventing stale policy in next test")
			return true, nil
		}
		klog.V(4).Infof("ConfigMap still exists, waiting for deletion")
		return false, nil
	})
}

// patchCSVWithImages patches the CSV with the provided operand image
func patchCSVWithImages(ctx context.Context, dynamicClient dynamic.Interface, kubeClient *k8sclient.Clientset, namespace, operandImage string) error {
	if operandImage == "" {
		return fmt.Errorf("operandImage parameter is empty")
	}

	// Get CSV
	csvList, err := dynamicClient.Resource(schema.GroupVersionResource{
		Group:    operatorsv1alpha1.GroupName,
		Version:  operatorsv1alpha1.GroupVersion,
		Resource: "clusterserviceversions",
	}).Namespace(namespace).List(ctx, metav1.ListOptions{})

	if err != nil {
		return fmt.Errorf("failed to list CSVs: %w", err)
	}

	if len(csvList.Items) == 0 {
		return fmt.Errorf("no CSV found in namespace %s", namespace)
	}

	csv := &csvList.Items[0]
	csvName := csv.GetName()
	klog.Infof("Found CSV: %s", csvName)

	// Find indices for both locations that need patching
	var relatedImageIndex int = -1
	var envVarIndex int = -1

	// Find index of descheduler-operand in relatedImages
	relatedImages, _, _ := unstructured.NestedSlice(csv.Object, "spec", "relatedImages")
	for i, img := range relatedImages {
		imgMap := img.(map[string]interface{})
		if imgMap["name"] == "descheduler-operand" {
			relatedImageIndex = i
			break
		}
	}
	if relatedImageIndex == -1 {
		return fmt.Errorf("descheduler-operand not found in CSV relatedImages")
	}

	// Find index of RELATED_IMAGE_OPERAND_IMAGE in deployment env
	deployments, _, _ := unstructured.NestedSlice(csv.Object, "spec", "install", "spec", "deployments")
	if len(deployments) > 0 {
		deployment := deployments[0].(map[string]interface{})
		spec, _, _ := unstructured.NestedMap(deployment, "spec", "template", "spec")
		containers, _, _ := unstructured.NestedSlice(spec, "containers")
		if len(containers) > 0 {
			container := containers[0].(map[string]interface{})
			envVars, _, _ := unstructured.NestedSlice(container, "env")
			for i, env := range envVars {
				envMap := env.(map[string]interface{})
				if envMap["name"] == "RELATED_IMAGE_OPERAND_IMAGE" {
					envVarIndex = i
					break
				}
			}
		}
	}
	if envVarIndex == -1 {
		return fmt.Errorf("RELATED_IMAGE_OPERAND_IMAGE not found in deployment env")
	}

	// Build a SINGLE JSON Patch with BOTH operations
	patch := []map[string]interface{}{
		{
			"op":    "replace",
			"path":  fmt.Sprintf("/spec/relatedImages/%d/image", relatedImageIndex),
			"value": operandImage,
		},
		{
			"op":    "replace",
			"path":  fmt.Sprintf("/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/%d/value", envVarIndex),
			"value": operandImage,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal combined patch: %w", err)
	}

	// Apply BOTH patches in a single operation
	_, err = dynamicClient.Resource(schema.GroupVersionResource{
		Group:    operatorsv1alpha1.GroupName,
		Version:  operatorsv1alpha1.GroupVersion,
		Resource: "clusterserviceversions",
	}).Namespace(namespace).Patch(ctx, csvName, types.JSONPatchType, patchBytes, metav1.PatchOptions{})

	if err != nil {
		return fmt.Errorf("failed to apply combined patch: %w", err)
	}
	klog.Infof("✅ CSV patched with operand image")

	// Wait for the patches to be applied
	klog.Infof("⏳ Waiting for CSV patches to be applied...")
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		csvList, err := dynamicClient.Resource(schema.GroupVersionResource{
			Group:    operatorsv1alpha1.GroupName,
			Version:  operatorsv1alpha1.GroupVersion,
			Resource: "clusterserviceversions",
		}).Namespace(namespace).List(ctx, metav1.ListOptions{})

		if err != nil {
			klog.Warningf("Failed to list CSVs during wait: %v", err)
			return false, nil
		}

		if len(csvList.Items) == 0 {
			klog.Warningf("CSV not found during wait")
			return false, nil
		}

		csv := &csvList.Items[0]

		// Check relatedImages
		relatedImages, _, _ := unstructured.NestedSlice(csv.Object, "spec", "relatedImages")
		relatedImagePatched := false
		for _, img := range relatedImages {
			imgMap := img.(map[string]interface{})
			if imgMap["name"] == "descheduler-operand" {
				actualImage, _ := imgMap["image"].(string)
				if actualImage == operandImage {
					relatedImagePatched = true
					klog.Infof("✅ relatedImages patch applied: %s", actualImage)
				} else {
					klog.Infof("⏳ relatedImages not yet patched. Current: %s", actualImage)
				}
			}
		}

		// Check deployment env variable
		envPatched := false
		deployments, _, _ := unstructured.NestedSlice(csv.Object, "spec", "install", "spec", "deployments")
		if len(deployments) > 0 {
			deployment := deployments[0].(map[string]interface{})
			spec, _, _ := unstructured.NestedMap(deployment, "spec", "template", "spec")
			containers, _, _ := unstructured.NestedSlice(spec, "containers")
			if len(containers) > 0 {
				container := containers[0].(map[string]interface{})
				envVars, _, _ := unstructured.NestedSlice(container, "env")
				for _, env := range envVars {
					envMap := env.(map[string]interface{})
					if envMap["name"] == "RELATED_IMAGE_OPERAND_IMAGE" {
						actualEnvValue, _ := envMap["value"].(string)
						if actualEnvValue == operandImage {
							envPatched = true
							klog.Infof("✅ env patch applied: %s", actualEnvValue)
						} else {
							klog.Infof("⏳ env not yet patched. Current: %s", actualEnvValue)
						}
					}
				}
			}
		}

		if relatedImagePatched && envPatched {
			klog.Infof("✅ All CSV patches successfully applied!")
			return true, nil
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("CSV patch did not apply after 30 seconds: %w", err)
	}

	// Verify patch was applied
	klog.Infof("Verifying operand image in CSV")
	err = verifyCSVHasImage(ctx, dynamicClient, namespace, operandImage)
	if err != nil {
		return err
	}

	return nil
}

// verifyCSVHasImage checks if the CSV contains the expected operand image in both relatedImages and deployment env
func verifyCSVHasImage(ctx context.Context, dynamicClient dynamic.Interface, namespace, operandImage string) error {
	// Get CSV
	csvList, err := dynamicClient.Resource(schema.GroupVersionResource{
		Group:    operatorsv1alpha1.GroupName,
		Version:  operatorsv1alpha1.GroupVersion,
		Resource: "clusterserviceversions",
	}).Namespace(namespace).List(ctx, metav1.ListOptions{})

	if err != nil {
		return fmt.Errorf("failed to list CSVs: %w", err)
	}

	if len(csvList.Items) == 0 {
		return fmt.Errorf("no CSV found in namespace %s", namespace)
	}

	csvAfter := csvList.Items[0]

	// Check relatedImages
	relatedImagesAfter, _, _ := unstructured.NestedSlice(csvAfter.Object, "spec", "relatedImages")
	foundInRelatedImages := false
	for _, img := range relatedImagesAfter {
		imgMap := img.(map[string]interface{})
		name, _ := imgMap["name"].(string)
		actualImage, _ := imgMap["image"].(string)

		if name == "descheduler-operand" {
			if actualImage == operandImage {
				foundInRelatedImages = true
				klog.Infof("✅ Operand image found in relatedImages: %s", operandImage)
			} else {
				return fmt.Errorf("VERIFICATION FAILED: operand image in relatedImages does not match\nExpected: %s\nActual: %s",
					operandImage, actualImage)
			}
			break
		}
	}

	// Check deployment env variable
	foundInEnv := false
	deployments, _, _ := unstructured.NestedSlice(csvAfter.Object, "spec", "install", "spec", "deployments")
	if len(deployments) > 0 {
		deployment := deployments[0].(map[string]interface{})
		spec, _, _ := unstructured.NestedMap(deployment, "spec", "template", "spec")
		containers, _, _ := unstructured.NestedSlice(spec, "containers")
		if len(containers) > 0 {
			container := containers[0].(map[string]interface{})
			envVars, _, _ := unstructured.NestedSlice(container, "env")
			for _, env := range envVars {
				envMap := env.(map[string]interface{})
				if envMap["name"] == "RELATED_IMAGE_OPERAND_IMAGE" {
					actualEnvValue, _ := envMap["value"].(string)
					if actualEnvValue == operandImage {
						foundInEnv = true
						klog.Infof("✅ Operand image found in deployment env: %s", operandImage)
					} else {
						return fmt.Errorf("VERIFICATION FAILED: operand image in deployment env does not match\nExpected: %s\nActual: %s",
							operandImage, actualEnvValue)
					}
					break
				}
			}
		}
	}

	if !foundInRelatedImages || !foundInEnv {
		return fmt.Errorf("VERIFICATION FAILED: operand image not found in one or both locations")
	}

	klog.Infof("✅ Verification passed: operand image correctly applied in both locations")
	return nil
}
