package operator

import (
	"context"
	"fmt"
	"os"
	"time"

	"k8s.io/klog/v2"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	configv1informers "github.com/openshift/client-go/config/informers/externalversions"
	routev1client "github.com/openshift/client-go/route/clientset/versioned"
	routev1informers "github.com/openshift/client-go/route/informers/externalversions"
	operatorconfigclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	operatorclientinformers "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/informers/externalversions"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/configobservation/configobservercontroller"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/loglevel"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

const (
	workQueueKey = "key"
)

func RunOperator(ctx context.Context, cc *controllercmd.ControllerContext) error {
	kubeClient, err := kubernetes.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return err
	}

	openshiftConfigClient, err := configv1client.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}

	configInformers := configv1informers.NewSharedInformerFactory(openshiftConfigClient, 10*time.Minute)

	openshiftRouteClient, err := routev1client.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}

	routeInformers := routev1informers.NewSharedInformerFactory(openshiftRouteClient, 10*time.Minute)

	dynamicClient, err := dynamic.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return err
	}

	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(kubeClient,
		"",
		operatorclient.OperatorNamespace,
	)

	operatorConfigClient, err := operatorconfigclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}
	operatorConfigInformers := operatorclientinformers.NewSharedInformerFactory(operatorConfigClient, 10*time.Minute)
	deschedulerClient := &operatorclient.DeschedulerClient{
		Ctx:            ctx,
		SharedInformer: operatorConfigInformers.Kubedeschedulers().V1().KubeDeschedulers().Informer(),
		OperatorClient: operatorConfigClient.KubedeschedulersV1(),
	}

	resourceSyncController, err := resourcesynccontroller.NewResourceSyncController(
		deschedulerClient,
		kubeInformersForNamespaces,
		kubeClient,
		cc.EventRecorder,
	)
	if err != nil {
		return err
	}

	configObserver := configobservercontroller.NewConfigObserver(
		deschedulerClient,
		kubeInformersForNamespaces,
		configInformers,
		resourceSyncController,
		cc.EventRecorder,
	)

	// make sure to remove previous "cluster" descheduler deployment before starting a new one with a proper name
	// TODO: this can be removed in 4.12+
	if err := kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Delete(ctx, operatorclient.OperatorConfigName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed removing old operand deployment: %w", err)
	}

	targetConfigReconciler := NewTargetConfigReconciler(
		ctx,
		os.Getenv("RELATED_IMAGE_OPERAND_IMAGE"),
		os.Getenv("RELATED_IMAGE_SOFTTAINTER_IMAGE"),
		operatorConfigClient.KubedeschedulersV1(),
		operatorConfigInformers.Kubedeschedulers().V1().KubeDeschedulers(),
		deschedulerClient,
		kubeClient,
		dynamicClient,
		configInformers,
		routeInformers,
		cc.EventRecorder,
	)

	logLevelController := loglevel.NewClusterOperatorLoggingController(deschedulerClient, cc.EventRecorder)

	klog.Infof("Starting informers")
	operatorConfigInformers.Start(ctx.Done())
	configInformers.Start(ctx.Done())
	routeInformers.Start(ctx.Done())
	kubeInformersForNamespaces.Start(ctx.Done())

	klog.Infof("Starting log level controller")
	go logLevelController.Run(ctx, 1)
	klog.Infof("Starting target config reconciler")
	go targetConfigReconciler.Run(1, ctx.Done())

	go resourceSyncController.Run(ctx, 1)
	go configObserver.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
