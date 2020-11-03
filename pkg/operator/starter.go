package operator

import (
	"context"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/configobservation/configobservercontroller"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/resourcesynccontroller"
	"k8s.io/klog/v2"
	"time"

	"k8s.io/client-go/kubernetes"

	operatorconfigclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	operatorclientinformers "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/informers/externalversions"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
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
		SharedInformer: operatorConfigInformers.Kubedeschedulers().V1beta1().KubeDeschedulers().Informer(),
		OperatorClient: operatorConfigClient.KubedeschedulersV1beta1(),
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
		operatorConfigInformers.Kubedeschedulers().V1beta1().KubeDeschedulers(),
		resourceSyncController,
		cc.EventRecorder,
	)

	targetConfigReconciler := NewTargetConfigReconciler(
		ctx,
		operatorConfigClient.KubedeschedulersV1beta1(),
		operatorConfigInformers.Kubedeschedulers().V1beta1().KubeDeschedulers(),
		deschedulerClient,
		kubeClient,
		cc.EventRecorder,
	)

	klog.Infof("Starting informers")
	operatorConfigInformers.Start(ctx.Done())
	kubeInformersForNamespaces.Start(ctx.Done())
	klog.Infof("Starting target config reconciler")
	go targetConfigReconciler.Run(1, ctx.Done())
	klog.Infof("Starting config observer")
	go configObserver.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
