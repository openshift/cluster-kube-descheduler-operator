package configobservercontroller

import (
	operatorclientinformers "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/informers/externalversions/descheduler/v1beta1"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/configobservation"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/configobservation/descheduler"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/configobserver"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/client-go/tools/cache"
)

type ConfigObserver struct {
	factory.Controller
}

func NewConfigObserver(
	operatorClient v1helpers.OperatorClient,
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces,
	configInformer operatorclientinformers.KubeDeschedulerInformer,
	resourceSyncer resourcesynccontroller.ResourceSyncer,
	eventRecorder events.Recorder,
) *ConfigObserver {
	interestingNamespaces := []string{
		operatorclient.OperatorNamespace,
	}

	informers := []factory.Informer{
		operatorClient.Informer(),
		configInformer.Informer(),
	}
	for _, ns := range interestingNamespaces {
		informers = append(informers, kubeInformersForNamespaces.InformersFor(ns).Core().V1().ConfigMaps().Informer())
	}

	configMapPreRunCacheSynced := []cache.InformerSynced{}
	for _, ns := range interestingNamespaces {
		configMapPreRunCacheSynced = append(configMapPreRunCacheSynced, kubeInformersForNamespaces.InformersFor(ns).Core().V1().ConfigMaps().Informer().HasSynced)
	}

	c := &ConfigObserver{
		Controller: configobserver.NewConfigObserver(
			operatorClient,
			eventRecorder,
			configobservation.Listers{
				DeschedulerLister: configInformer.Lister(),
				ConfigmapLister:   kubeInformersForNamespaces.ConfigMapLister(),
				ResourceSync:      resourceSyncer,
				PreRunCachesSynced: append(configMapPreRunCacheSynced,
					operatorClient.Informer().HasSynced,
					configInformer.Informer().HasSynced,
				),
			},
			informers,
			descheduler.ObserveDeschedulerConfig,
		),
	}

	return c
}
