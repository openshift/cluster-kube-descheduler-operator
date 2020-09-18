package operator

import (
	"context"
	"time"

	"k8s.io/client-go/kubernetes"

	operatorv1 "github.com/openshift/api/operator/v1"
	operatorconfigclient "github.com/openshift/client-go/operator/clientset/versioned"
	operatorclientinformers "github.com/openshift/client-go/operator/informers/externalversions"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/genericoperatorclient"
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

	operatorClient, _, err := genericoperatorclient.NewClusterScopedOperatorClient(cc.KubeConfig, operatorv1.GroupVersion.WithResource("kubedeschedulers"))
	if err != nil {
		return err
	}

	operatorConfigClient, err := operatorconfigclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}
	operatorConfigInformers := operatorclientinformers.NewSharedInformerFactory(operatorConfigClient, 10*time.Minute)

	targetConfigReconciler := NewTargetConfigReconciler(
		ctx,
		operatorConfigClient.OperatorV1().KubeDeschedulers(),
		operatorConfigInformers.Operator().V1().KubeDeschedulers(),
		operatorClient,
		kubeClient,
		cc.EventRecorder,
	)

	operatorConfigInformers.Start(ctx.Done())
	kubeInformersForNamespaces.Start(ctx.Done())
	targetConfigReconciler.Run(1, ctx.Done())

	return nil
}
