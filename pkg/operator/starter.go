package operator

import (
	"context"
	"os"
	"time"

	"k8s.io/klog/v2"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	migrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset"

	operatorconfigclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	operatorclientinformers "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/informers/externalversions"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
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

	dynamicClient, err := dynamic.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return err
	}

	// only for 4.8, so v1beta1->v1 stored version migration can run
	apiExtensionsClient, err := clientset.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return err
	}
	migrationsClient, err := migrationclient.NewForConfig(cc.ProtoKubeConfig)
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

	targetConfigReconciler := NewTargetConfigReconciler(
		ctx,
		os.Getenv("IMAGE"),
		operatorConfigClient.KubedeschedulersV1(),
		operatorConfigInformers.Kubedeschedulers().V1().KubeDeschedulers(),
		deschedulerClient,
		kubeClient,
		dynamicClient,
		cc.EventRecorder,
		apiExtensionsClient,
		migrationsClient,
	)

	logLevelController := loglevel.NewClusterOperatorLoggingController(deschedulerClient, cc.EventRecorder)

	klog.Infof("Starting informers")
	operatorConfigInformers.Start(ctx.Done())
	kubeInformersForNamespaces.Start(ctx.Done())

	klog.Infof("Starting log level controller")
	go logLevelController.Run(ctx, 1)
	klog.Infof("Starting target config reconciler")
	go targetConfigReconciler.Run(1, ctx.Done())

	<-ctx.Done()
	return nil
}
