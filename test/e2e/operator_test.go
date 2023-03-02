package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	descv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	deschclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	ssscheme "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned/scheme"
	corev1 "k8s.io/api/core/v1"
	apiextclientv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/cluster-kube-descheduler-operator/test/e2e/bindata"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
)

func TestMain(m *testing.M) {
	if os.Getenv("KUBECONFIG") == "" {
		klog.Errorf("KUBECONFIG environment variable not set")
		os.Exit(1)
	}

	if os.Getenv("RELEASE_IMAGE_LATEST") == "" {
		klog.Errorf("RELEASE_IMAGE_LATEST environment variable not set")
		os.Exit(1)
	}

	if os.Getenv("NAMESPACE") == "" {
		klog.Errorf("NAMESPACE environment variable not set")
		os.Exit(1)
	}

	kubeClient := getKubeClientOrDie()
	apiExtClient := getApiExtensionKubeClient()
	deschClient := getDeschedulerClient()

	eventRecorder := events.NewKubeRecorder(kubeClient.CoreV1().Events("default"), "test-e2e", &corev1.ObjectReference{})

	ctx, cancelFnc := context.WithCancel(context.TODO())
	defer cancelFnc()

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
			path: "assets/04_descheduler-cluster-role-binding.yaml",
			readerAndApply: func(objBytes []byte) error {
				_, _, err := resourceapply.ApplyClusterRoleBinding(ctx, kubeClient.RbacV1(), eventRecorder, resourceread.ReadClusterRoleBindingV1OrDie(objBytes))
				return err
			},
		},
		{
			path: "assets/05_deployment.yaml",
			readerAndApply: func(objBytes []byte) error {
				required := resourceread.ReadDeploymentV1OrDie(objBytes)
				// override the operator image with the one built in the CI

				// E.g. RELEASE_IMAGE_LATEST=registry.build03.ci.openshift.org/ci-op-52fj47p4/stable:${component}
				registry := strings.Split(os.Getenv("RELEASE_IMAGE_LATEST"), "/")[0]

				required.Spec.Template.Spec.Containers[0].Image = registry + "/" + os.Getenv("NAMESPACE") + "/pipeline:cluster-kube-descheduler-operator"
				// OPERAND_IMAGE env
				for i, env := range required.Spec.Template.Spec.Containers[0].Env {
					if env.Name == "IMAGE" {
						required.Spec.Template.Spec.Containers[0].Env[i].Value = "http://registry.ci.openshift.org/ocp/4.13:descheduler"
						break
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
		{
			path: "assets/07_descheduler-operator.cr.yaml",
			readerAndApply: func(objBytes []byte) error {
				requiredObj, err := runtime.Decode(ssscheme.Codecs.UniversalDecoder(descv1.SchemeGroupVersion), objBytes)
				if err != nil {
					klog.Errorf("Unable to decode assets/07_descheduler-operator.cr.yaml: %v", err)
					return err
				}
				requiredDesch := requiredObj.(*descv1.KubeDescheduler)

				_, err = deschClient.KubedeschedulersV1().KubeDeschedulers(requiredDesch.Namespace).Create(ctx, requiredDesch, metav1.CreateOptions{})
				return err
			},
		},
	}

	// create required resources, e.g. namespace, crd, roles
	if err := wait.PollImmediate(1*time.Second, 10*time.Second, func() (bool, error) {
		for _, asset := range assets {
			klog.Infof("Creating %v", asset.path)
			if err := asset.readerAndApply(bindata.MustAsset(asset.path)); err != nil {
				klog.Errorf("Unable to create %v: %v", asset.path, err)
				return false, nil
			}
		}

		return true, nil
	}); err != nil {
		klog.Errorf("Unable to create SSO resources: %v", err)
		os.Exit(1)
	}

	var deschPod *corev1.Pod
	// Wait until the descheduler pod is running
	if err := wait.PollImmediate(5*time.Second, 1*time.Minute, func() (bool, error) {
		klog.Infof("Listing pods...")
		podItems, err := kubeClient.CoreV1().Pods(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.Errorf("Unable to list pods: %v", err)
			return false, nil
		}
		for _, pod := range podItems.Items {
			if !strings.HasPrefix(pod.Name, operatorclient.OperandName+"-") {
				continue
			}
			klog.Infof("Checking pod: %v, phase: %v, deletionTS: %v\n", pod.Name, pod.Status.Phase, pod.GetDeletionTimestamp())
			if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
				deschPod = pod.DeepCopy()
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		klog.Errorf("Unable to wait for the SS pod to run")
		os.Exit(1)
	}

	klog.Infof("Descheduler running in %v", deschPod.Name)
	os.Exit(m.Run())
}

func getKubeClientOrDie() *k8sclient.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}
	client, err := k8sclient.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}

func getApiExtensionKubeClient() *apiextclientv1.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}
	client, err := apiextclientv1.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}

func getDeschedulerClient() *deschclient.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Errorf("Unable to build config: %v", err)
		os.Exit(1)
	}
	client, err := deschclient.NewForConfig(config)
	if err != nil {
		klog.Errorf("Unable to build client: %v", err)
		os.Exit(1)
	}
	return client
}
