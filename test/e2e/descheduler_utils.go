package e2e

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	descv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	deschclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
)

// Struct types for descheduler test resources

type operatorgroup struct {
	name      string
	namespace string
}

type subscription struct {
	name        string
	namespace   string
	channelName string
	sourceName  string
	startingCSV string
}

type kubedescheduler struct {
	namespace        string
	mode             string
	profiles         []string
	imageInfo        string
	logLevel         string
	operatorLogLevel string
}

// Helper functions for resource creation and management

// createOperatorGroup creates an OperatorGroup for the descheduler operator
func (og *operatorgroup) createOperatorGroup(ctx context.Context, dynamicClient dynamic.Interface) error {
	klog.Infof("Creating OperatorGroup %s in namespace %s", og.name, og.namespace)

	ogGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1",
		Resource: "operatorgroups",
	}

	operatorGroup := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1",
			"kind":       "OperatorGroup",
			"metadata": map[string]interface{}{
				"name":      og.name,
				"namespace": og.namespace,
			},
			"spec": map[string]interface{}{
				"targetNamespaces": []string{og.namespace},
			},
		},
	}

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 20*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := dynamicClient.Resource(ogGVR).Namespace(og.namespace).Create(ctx, operatorGroup, metav1.CreateOptions{})
		if err != nil {
			klog.Warningf("Failed to create OperatorGroup, retrying: %v", err)
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("failed to create OperatorGroup %s: %w", og.name, err)
	}

	klog.Infof("Successfully created OperatorGroup %s", og.name)
	return nil
}

// deleteOperatorGroup deletes the OperatorGroup
func (og *operatorgroup) deleteOperatorGroup(ctx context.Context, dynamicClient dynamic.Interface) error {
	klog.Infof("Deleting OperatorGroup %s in namespace %s", og.name, og.namespace)

	ogGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1",
		Resource: "operatorgroups",
	}

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 20*time.Second, true, func(ctx context.Context) (bool, error) {
		err := dynamicClient.Resource(ogGVR).Namespace(og.namespace).Delete(ctx, og.name, metav1.DeleteOptions{})
		if err != nil {
			klog.Warningf("Failed to delete OperatorGroup, retrying: %v", err)
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("failed to delete OperatorGroup %s: %w", og.name, err)
	}

	klog.Infof("Successfully deleted OperatorGroup %s", og.name)
	return nil
}

// createSubscription creates a Subscription for the descheduler operator
func (sub *subscription) createSubscription(ctx context.Context, dynamicClient dynamic.Interface) error {
	klog.Infof("Creating Subscription %s in namespace %s", sub.name, sub.namespace)

	subGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}

	subscription := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1alpha1",
			"kind":       "Subscription",
			"metadata": map[string]interface{}{
				"name":      sub.name,
				"namespace": sub.namespace,
			},
			"spec": map[string]interface{}{
				"channel":             sub.channelName,
				"installPlanApproval": "Automatic",
				"name":                sub.name,
				"source":              sub.sourceName,
				"sourceNamespace":     "openshift-marketplace",
			},
		},
	}

	// Add startingCSV if provided using k8s unstructured helper
	if sub.startingCSV != "" {
		if err := unstructured.SetNestedField(subscription.Object, sub.startingCSV, "spec", "startingCSV"); err != nil {
			return fmt.Errorf("failed to set startingCSV: %w", err)
		}
	}

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 20*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := dynamicClient.Resource(subGVR).Namespace(sub.namespace).Create(ctx, subscription, metav1.CreateOptions{})
		if err != nil {
			klog.Warningf("Failed to create Subscription, retrying: %v", err)
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("failed to create Subscription %s: %w", sub.name, err)
	}

	klog.Infof("Successfully created Subscription %s", sub.name)
	return nil
}

// deleteSubscription deletes the Subscription
func (sub *subscription) deleteSubscription(ctx context.Context, dynamicClient dynamic.Interface) error {
	klog.Infof("Deleting Subscription %s in namespace %s", sub.name, sub.namespace)

	subGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 20*time.Second, true, func(ctx context.Context) (bool, error) {
		err := dynamicClient.Resource(subGVR).Namespace(sub.namespace).Delete(ctx, sub.name, metav1.DeleteOptions{})
		if err != nil {
			klog.Warningf("Failed to delete Subscription, retrying: %v", err)
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("failed to delete Subscription %s: %w", sub.name, err)
	}

	klog.Infof("Successfully deleted Subscription %s", sub.name)
	return nil
}

// skipMissingCatalogsources checks if required catalog sources are available
func (sub *subscription) skipMissingCatalogsources(ctx context.Context, dynamicClient dynamic.Interface) error {
	klog.Infof("Checking for required catalog source: %s", sub.sourceName)

	csGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "catalogsources",
	}

	// Try to get the catalog source
	_, err := dynamicClient.Resource(csGVR).Namespace("openshift-marketplace").Get(ctx, sub.sourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("catalog source %s not found in openshift-marketplace: %w", sub.sourceName, err)
	}

	klog.Infof("Catalog source %s is available", sub.sourceName)
	return nil
}

// createKubeDescheduler creates a KubeDescheduler CR
func (dsch *kubedescheduler) createKubeDescheduler(ctx context.Context, deschClient *deschclient.Clientset) error {
	klog.Infof("Creating KubeDescheduler in namespace %s with profiles %v", dsch.namespace, dsch.profiles)
	// For now, the KubeDescheduler CR should already exist from operator installation
	// This function can be enhanced to create/update CR with specific profiles if needed
	return nil
}

// createKubeDeschedulerWithProfiles attempts to create a KubeDescheduler CR with specified profiles
// Returns error if creation fails (which is expected for conflicting profiles)
func createKubeDeschedulerWithProfiles(ctx context.Context, deschClient *deschclient.Clientset, namespace, name string, profiles []string) error {
	// Convert string profiles to DeschedulerProfile type
	deschProfiles := make([]descv1.DeschedulerProfile, len(profiles))
	for i, p := range profiles {
		deschProfiles[i] = descv1.DeschedulerProfile(p)
	}

	kdCR := &descv1.KubeDescheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: descv1.KubeDeschedulerSpec{
			Profiles: deschProfiles,
			Mode:     descv1.Predictive,
		},
	}

	_, err := deschClient.KubedeschedulersV1().KubeDeschedulers(namespace).Create(ctx, kdCR, metav1.CreateOptions{})
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

// patchKubeDeschedulerNamespaceFiltering patches the KubeDescheduler CR with namespace filtering
func patchKubeDeschedulerNamespaceFiltering(ctx context.Context, deschClient *deschclient.Clientset, namespace, name string, included, excluded []string) error {
	// Get current CR
	kdCR, err := deschClient.KubedeschedulersV1().KubeDeschedulers(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get KubeDescheduler CR: %w", err)
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

	klog.Infof("Successfully patched KubeDescheduler namespace filtering (included: %v, excluded: %v)", included, excluded)
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

// getCSVName gets the CSV name for the operator using label selector
func getCSVName(ctx context.Context, dynamicClient dynamic.Interface, namespace, labelSelector string) (string, error) {
	csvGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}

	csvList, err := dynamicClient.Resource(csvGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list CSVs: %w", err)
	}

	if len(csvList.Items) == 0 {
		return "", fmt.Errorf("no CSV found with label selector: %s", labelSelector)
	}

	csvName := csvList.Items[0].GetName()
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
	csvGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}

	csvUnstructured, err := dynamicClient.Resource(csvGVR).Namespace(namespace).Get(ctx, csvName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get CSV %s: %w", csvName, err)
	}

	// Extract relatedImages from spec using k8s unstructured helper
	relatedImages, found, err := unstructured.NestedSlice(csvUnstructured.Object, "spec", "relatedImages")
	if err != nil {
		return nil, fmt.Errorf("failed to get relatedImages: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("relatedImages not found in CSV spec")
	}

	// Convert to RelatedImage structs using k8s unstructured helpers
	var images []RelatedImage
	for _, item := range relatedImages {
		imgMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		// Use k8s built-in helpers instead of manual map access
		name, _, _ := unstructured.NestedString(imgMap, "name")
		image, _, _ := unstructured.NestedString(imgMap, "image")
		images = append(images, RelatedImage{
			Name:  name,
			Image: image,
		})
	}

	klog.Infof("Found %d related images in CSV %s", len(images), csvName)
	return images, nil
}

// waitForCSVSucceeded waits for CSV to reach Succeeded phase
func waitForCSVSucceeded(ctx context.Context, dynamicClient dynamic.Interface, namespace, csvName string) error {
	klog.Infof("Waiting for CSV %s/%s to succeed", namespace, csvName)

	csvGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}

	return wait.PollUntilContextTimeout(ctx, 10*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		csv, err := dynamicClient.Resource(csvGVR).Namespace(namespace).Get(ctx, csvName, metav1.GetOptions{})
		if err != nil {
			klog.Warningf("Failed to get CSV %s: %v", csvName, err)
			return false, nil
		}

		// Get the phase from status
		phase, found, err := unstructured.NestedString(csv.Object, "status", "phase")
		if err != nil || !found {
			klog.Warningf("CSV %s has no phase yet", csvName)
			return false, nil
		}

		klog.Infof("CSV %s phase: %s", csvName, phase)

		if phase == "Succeeded" {
			klog.Infof("CSV %s succeeded", csvName)
			return true, nil
		}

		if phase == "Failed" {
			return false, fmt.Errorf("CSV %s failed", csvName)
		}

		return false, nil
	})
}

// packagemanifestKDO fetches packagemanifest values dynamically for kube-descheduler-operator
func packagemanifestKDO(ctx context.Context, dynamicClient dynamic.Interface, packageName, namespace string, catalogNames []string) (*subscription, error) {
	klog.Infof("Fetching packagemanifest values for %s", packageName)

	pmGVR := schema.GroupVersionResource{
		Group:    "packages.operators.coreos.com",
		Version:  "v1",
		Resource: "packagemanifests",
	}

	// Get the package manifest
	pm, err := dynamicClient.Resource(pmGVR).Namespace(namespace).Get(ctx, packageName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get packagemanifest %s: %w", packageName, err)
	}

	// Extract catalog source name
	catalogSource, found, err := unstructured.NestedString(pm.Object, "status", "catalogSource")
	if err != nil || !found {
		klog.Warningf("Could not find catalogSource in packagemanifest, using default")
		catalogSource = "redhat-operators"
	}

	// Extract default channel
	defaultChannel, found, err := unstructured.NestedString(pm.Object, "status", "defaultChannel")
	if err != nil || !found {
		klog.Warningf("Could not find defaultChannel in packagemanifest, using 'stable'")
		defaultChannel = "stable"
	}

	// Extract channels to find the latest CSV
	channels, found, err := unstructured.NestedSlice(pm.Object, "status", "channels")
	if err != nil || !found {
		return nil, fmt.Errorf("could not find channels in packagemanifest")
	}

	var startingCSV string
	for _, ch := range channels {
		channel, ok := ch.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(channel, "name")
		if name == defaultChannel {
			startingCSV, _, _ = unstructured.NestedString(channel, "currentCSV")
			break
		}
	}

	if startingCSV == "" && len(channels) > 0 {
		// Fallback to first channel's CSV
		if channel, ok := channels[0].(map[string]interface{}); ok {
			startingCSV, _, _ = unstructured.NestedString(channel, "currentCSV")
		}
	}

	klog.Infof("Found package manifest: channel=%s, source=%s, startingCSV=%s", defaultChannel, catalogSource, startingCSV)

	return &subscription{
		name:        packageName,
		namespace:   namespace,
		channelName: defaultChannel,
		sourceName:  catalogSource,
		startingCSV: startingCSV,
	}, nil
}

// cordonNode cordons a node
func cordonNode(ctx context.Context, kubeClient *k8sclient.Clientset, nodeName string) error {
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if !node.Spec.Unschedulable {
		node.Spec.Unschedulable = true
		_, err = kubeClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to cordon node %s: %w", nodeName, err)
		}
		klog.Infof("Cordoned node %s", nodeName)
	}
	return nil
}

// uncordonNode uncordons a node
func uncordonNode(ctx context.Context, kubeClient *k8sclient.Clientset, nodeName string) error {
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if node.Spec.Unschedulable {
		node.Spec.Unschedulable = false
		_, err = kubeClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to uncordon node %s: %w", nodeName, err)
		}
		klog.Infof("Uncordoned node %s", nodeName)
	}
	return nil
}

// getWorkerNodes gets all worker nodes
func getWorkerNodes(ctx context.Context, kubeClient *k8sclient.Clientset) ([]corev1.Node, error) {
	nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "node-role.kubernetes.io/worker=",
	})
	if err != nil {
		return nil, err
	}
	return nodes.Items, nil
}

// addNodeLabel adds a label to a node
func addNodeLabel(ctx context.Context, kubeClient *k8sclient.Clientset, nodeName, key, value string) error {
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels[key] = value

	_, err = kubeClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to add label to node %s: %w", nodeName, err)
	}
	klog.Infof("Added label %s=%s to node %s", key, value, nodeName)
	return nil
}

// removeNodeLabel removes a label from a node
func removeNodeLabel(ctx context.Context, kubeClient *k8sclient.Clientset, nodeName, key string) error {
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	delete(node.Labels, key)

	_, err = kubeClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to remove label from node %s: %w", nodeName, err)
	}
	klog.Infof("Removed label %s from node %s", key, nodeName)
	return nil
}

// Helper functions for GVRs
func getOperatorGroupGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1",
		Resource: "operatorgroups",
	}
}

func getSubscriptionGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}
}

func getCSVGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}
}
