package e2e

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/drain"

	descv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	deschclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	olmlib "github.com/openshift/cluster-kube-descheduler-operator/test/e2e/olm"
)

// Helper functions for resource creation and management

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
