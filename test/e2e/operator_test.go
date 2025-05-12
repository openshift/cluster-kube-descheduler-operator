package e2e

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	descv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	deschclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	ssscheme "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned/scheme"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/softtainter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apiextclientv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/util/taints"
	"k8s.io/utils/clock"
	utilpointer "k8s.io/utils/pointer"

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
	softTainterDeploymentName                          = "softtainter"
	softTainterServiceAccountName                      = "openshift-descheduler-softtainter"
	softTainterClusterRoleName                         = "openshift-descheduler-softtainter"
	softTainterClusterRoleBindingName                  = "openshift-descheduler-softtainter"
	softTainterClusterMonitoringViewClusterRoleBinding = "openshift-descheduler-softtainter-monitoring"
	softTainterValidatingAdmissionPolicyName           = "openshift-descheduler-softtainter-vap"
	softTainterValidatingAdmissionPolicyBindingName    = "openshift-descheduler-softtainter-vap-binding"
)

var operatorConfigs = map[string]string{
	baseConf:                      "assets/07_descheduler-operator.cr.yaml",
	kubeVirtRelieveAndMigrateConf: "assets/07_descheduler-operator.cr.devKubeVirtRelieveAndMigrate.yaml",
}
var operatorConfigsAppliers = map[string]func() error{}

const EXPERIMENTAL_DISABLE_PSI_CHECK = "EXPERIMENTAL_DISABLE_PSI_CHECK"

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

	operator_image := "pipeline:cluster-kube-descheduler-operator"
	if os.Getenv("OPERATOR_IMAGE") != "" {
		operator_image = os.Getenv("OPERATOR_IMAGE")
	}

	kubeClient := getKubeClientOrDie()
	apiExtClient := getApiExtensionKubeClient()
	deschClient := getDeschedulerClient()

	eventRecorder := events.NewKubeRecorder(kubeClient.CoreV1().Events("default"), "test-e2e", &corev1.ObjectReference{}, clock.RealClock{})

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
			path: "assets/05_deployment.yaml",
			readerAndApply: func(objBytes []byte) error {
				required := resourceread.ReadDeploymentV1OrDie(objBytes)
				// override the operator image with the one built in the CI

				// E.g. RELEASE_IMAGE_LATEST=registry.build03.ci.openshift.org/ci-op-52fj47p4/stable:${component}
				registry := strings.Split(os.Getenv("RELEASE_IMAGE_LATEST"), "/")[0]

				required.Spec.Template.Spec.Containers[0].Image = registry + "/" + os.Getenv("NAMESPACE") + "/" + operator_image
				// OPERAND_IMAGE env
				for i, env := range required.Spec.Template.Spec.Containers[0].Env {
					if env.Name == "RELATED_IMAGE_OPERAND_IMAGE" {
						required.Spec.Template.Spec.Containers[0].Env[i].Value = "quay.io/jchaloup/descheduler:v5.1.1-8"
					} else if env.Name == "RELATED_IMAGE_SOFTTAINTER_IMAGE" {
						required.Spec.Template.Spec.Containers[0].Env[i].Value = registry + "/" + os.Getenv("NAMESPACE") + "/" + operator_image
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

	for k, path := range operatorConfigs {
		operatorConfigsAppliers[k] = func() error {
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
		klog.Errorf("Unable to create Descheduler operator resources: %v", err)
		os.Exit(1)
	}

	// apply base CR for the operator
	err := operatorConfigsAppliers[baseConf]()
	if err != nil {
		klog.Errorf("Unable to apply a CR for Descheduler operator: %v", err)
		os.Exit(1)
	}

	// wait for descheduler operator pod to be running
	deschOpPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName+"-operator", "")
	if err != nil {
		klog.Errorf("Unable to wait for the Descheduler operator pod to run")
		os.Exit(1)
	}
	klog.Infof("Descheduler operator pod running in %v", deschOpPod.Name)

	// wait for descheduler pod to be running
	deschPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName, operatorclient.OperandName+"-operator")
	if err != nil {
		klog.Errorf("Unable to wait for the Descheduler pod to run")
		os.Exit(1)
	}
	klog.Infof("Descheduler (operand) pod running in %v", deschPod.Name)

	os.Exit(m.Run())
}

func TestSoftTainterDeployment(t *testing.T) {
	kubeClient := getKubeClientOrDie()
	ctx, cancelFnc := context.WithCancel(context.TODO())
	defer cancelFnc()

	// ensure that softtainter additional objects are not there
	if err := checkSoftTainterObjects(ctx, kubeClient, operatorclient.OperatorNamespace, false); err != nil {
		t.Fatalf("Unexpected softTainter object: %v", err)
	}
	klog.Infof("No one of the softtainter additonal objects is there")

	// label all the nodes to mock a KubeVirt deployment
	if err := applyKubeVirtNodeLabel(ctx, kubeClient); err != nil {
		t.Fatalf("Failed applying KubeVirt node label: %v", err)
	}
	defer func(ctx context.Context, kubeClient *k8sclient.Clientset) {
		err := dropKubeVirtNodeLabel(ctx, kubeClient)
		if err != nil {
			t.Fatalf("Failed reverting KubeVirt node label: %v", err)
		}
	}(ctx, kubeClient)

	// patch the operator deployment to mock PSI
	prevDisablePSIcheck, foundDisablePSIcheck := os.LookupEnv(EXPERIMENTAL_DISABLE_PSI_CHECK)
	if err := mockPSIEnv(ctx, kubeClient); err != nil {
		t.Fatalf("Failed mocking PSI path enviromental variable to the operator depoyment: %v", err)
	}
	defer func(ctx context.Context, kubeClient *k8sclient.Clientset, prevDisablePSIcheck string, foundDisablePSIcheck bool) {
		err := unmockPSIEnv(ctx, kubeClient, prevDisablePSIcheck, foundDisablePSIcheck)
		if err != nil {
			t.Fatalf("Failed PSI path enviromental variable: %v", err)
		}
	}(ctx, kubeClient, prevDisablePSIcheck, foundDisablePSIcheck)
	// wait for descheduler operator pod to be running
	deschOpPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName, operatorclient.OperandName+"-operator")
	if err != nil {
		t.Fatalf("Unable to wait for the Descheduler operator pod to run")
	}
	klog.Infof("Descheduler pod running in %v", deschOpPod.Name)

	// apply devKubeVirtRelieveAndMigrate CR for the operator
	if err := operatorConfigsAppliers[kubeVirtRelieveAndMigrateConf](); err != nil {
		t.Fatalf("Unable to apply a CR for Descheduler operator: %v", err)
	}
	klog.Infof("Descheduler operator is now configured with devKubeVirtRelieveAndMigrate profile")
	defer func() {
		if err := operatorConfigsAppliers[baseConf](); err != nil {
			t.Fatalf("Failed restoring base profile: %v", err)
		}
	}()

	// wait for softtainter pod to be running
	stPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.SoftTainterOperandName, "")
	if err != nil {
		t.Fatalf("Unable to wait for the softtainter pod to run")
	}
	klog.Infof("SoftTainter pod running in %v", stPod.Name)

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
	err = operatorConfigsAppliers["base"]()
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

func TestSoftTainterVAP(t *testing.T) {
	kubeClient := getKubeClientOrDie()
	ctx, cancelFnc := context.WithCancel(context.TODO())
	defer cancelFnc()

	// label all the nodes to mock a KubeVirt deployment
	if err := applyKubeVirtNodeLabel(ctx, kubeClient); err != nil {
		t.Fatalf("Failed applying KubeVirt node label: %v", err)
	}
	defer func(ctx context.Context, kubeClient *k8sclient.Clientset) {
		err := dropKubeVirtNodeLabel(ctx, kubeClient)
		if err != nil {
			t.Fatalf("Failed reverting KubeVirt node label: %v", err)
		}
	}(ctx, kubeClient)

	// patch the operator deployment to mock PSI
	prevDisablePSIcheck, foundDisablePSIcheck := os.LookupEnv(EXPERIMENTAL_DISABLE_PSI_CHECK)
	if err := mockPSIEnv(ctx, kubeClient); err != nil {
		t.Fatalf("Failed mocking PSI path enviromental variable to the operator depoyment: %v", err)
	}
	defer func(ctx context.Context, kubeClient *k8sclient.Clientset, prevDisablePSIcheck string, foundDisablePSIcheck bool) {
		err := unmockPSIEnv(ctx, kubeClient, prevDisablePSIcheck, foundDisablePSIcheck)
		if err != nil {
			t.Fatalf("Failed PSI path enviromental variable: %v", err)
		}
	}(ctx, kubeClient, prevDisablePSIcheck, foundDisablePSIcheck)
	// wait for descheduler operator pod to be running
	deschOpPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.OperandName, operatorclient.OperandName+"-operator")
	if err != nil {
		t.Fatalf("Unable to wait for the Descheduler operator pod to run")
	}
	klog.Infof("Descheduler pod running in %v", deschOpPod.Name)

	// apply devKubeVirtRelieveAndMigrate CR for the operator
	if err := operatorConfigsAppliers[kubeVirtRelieveAndMigrateConf](); err != nil {
		t.Fatalf("Unable to apply a CR for Descheduler operator: %v", err)
	}
	klog.Infof("Descheduler operator is now configured with devKubeVirtRelieveAndMigrate profile")
	defer func() {
		if err := operatorConfigsAppliers[baseConf](); err != nil {
			t.Fatalf("Failed restoring base profile: %v", err)
		}
	}()

	// wait for softtainter pod to be running
	stPod, err := waitForPodRunningByNamePrefix(ctx, kubeClient, operatorclient.OperatorNamespace, operatorclient.SoftTainterOperandName, "")
	if err != nil {
		t.Fatalf("Unable to wait for the softtainter pod to run")
	}
	klog.Infof("SoftTainter pod running in %v", stPod.Name)

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

func TestDescheduling(t *testing.T) {
	kubeClient := getKubeClientOrDie()
	ctx := context.Background()
	testNamespace := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "e2e-" + strings.ToLower(t.Name())}}
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
	defer kubeClient.CoreV1().Namespaces().Delete(ctx, testNamespace.Name, metav1.DeleteOptions{})
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

	if err := wait.PollImmediate(1*time.Second, 3*time.Minute, func() (bool, error) {
		klog.Infof("Listing pods...")
		podList, err := kubeClient.CoreV1().Pods(testNamespace.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.Errorf("Unable to get pods: %v", err)
			return false, nil
		}
		excludePodNames := getPodNames(podList.Items)
		sort.Strings(excludePodNames)
		t.Logf("Existing pods: %v", excludePodNames)
		// validate no pods were deleted
		if len(intersectStrings(initialPodNames, excludePodNames)) > 0 {
			t.Logf("Not every pod was evicted")
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatalf("error while waiting for pod: %v", err)
	}
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
	if err := wait.PollImmediate(10*time.Second, 60*time.Second, func() (bool, error) {
		podList, err := clientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labelMap).String(),
		})
		if err != nil {
			return false, err
		}
		if len(podList.Items) != desireRunningPodNum {
			t.Logf("Waiting for %v pods to be running, got %v instead", desireRunningPodNum, len(podList.Items))
			return false, nil
		}
		for _, pod := range podList.Items {
			if pod.Status.Phase != v1.PodRunning {
				t.Logf("Pod %v not running yet, is %v instead", pod.Name, pod.Status.Phase)
				return false, nil
			}
		}
		return true, nil
	}); err != nil {
		t.Fatalf("Error waiting for pods running: %v", err)
	}
}

func waitForPodRunningByNamePrefix(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, nameprefix, excludedprefix string) (*v1.Pod, error) {
	var expectedPod *corev1.Pod
	// Wait until the expected pod is running
	if err := wait.PollImmediate(5*time.Second, 1*time.Minute, func() (bool, error) {
		klog.Infof("Listing pods...")
		podItems, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.Errorf("Unable to list pods: %v", err)
			return false, nil
		}
		for _, pod := range podItems.Items {
			if !strings.HasPrefix(pod.Name, nameprefix) || (excludedprefix != "" && strings.HasPrefix(pod.Name, excludedprefix)) {
				continue
			}
			klog.Infof("Checking pod: %v, phase: %v, deletionTS: %v\n", pod.Name, pod.Status.Phase, pod.GetDeletionTimestamp())
			if pod.Status.Phase == corev1.PodRunning && pod.GetDeletionTimestamp() == nil {
				expectedPod = pod.DeepCopy()
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	return expectedPod, nil
}

func waitForPodGoneByNamePrefix(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, nameprefix, excludedprefix string) error {
	// Wait until a no pods match nameprefix
	if err := wait.PollImmediate(5*time.Second, 1*time.Minute, func() (bool, error) {
		klog.Infof("Listing pods...")
		podItems, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.Errorf("Unable to list pods: %v", err)
			return false, nil
		}
		for _, pod := range podItems.Items {
			if !strings.HasPrefix(pod.Name, nameprefix) || (excludedprefix != "" && strings.HasPrefix(pod.Name, excludedprefix)) {
				continue
			}
			klog.Infof("Found pod: %v, phase: %v, deletionTS: %v\n", pod.Name, pod.Status.Phase, pod.GetDeletionTimestamp())
			return false, nil
		}
		return true, nil
	}); err != nil {
		return err
	}
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
