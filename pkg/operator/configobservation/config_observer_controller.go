package configobservation

import (
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/configobservation/descheduler"
	"github.com/openshift/library-go/pkg/operator/configobserver"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/client-go/tools/cache"
)

type ConfigObserver struct {
	*configobserver.ConfigObserver
}

func NewConfigObserver(
	operatorClient v1helpers.OperatorClient,
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces,
	resourceSyncer resourcesynccontroller.ResourceSyncer,
	eventRecorder events.Recorder,
) *ConfigObserver {

	configMapPreRunCacheSynced := []cache.InformerSynced{}
	configMapPreRunCacheSynced = append(configMapPreRunCacheSynced, kubeInformersForNamespaces.InformersFor("openshift-kube-descheduler-operator").Core().V1().ConfigMaps().Informer().HasSynced)

	c := &ConfigObserver{
		ConfigObserver: configobserver.NewConfigObserver(
			operatorClient,
			eventRecorder,
			Listers{
				ResourceSync:       resourceSyncer,
				PreRunCachesSynced: append(configMapPreRunCacheSynced, operatorClient.Informer().HasSynced),
			},
			descheduler.ObserveDescheduler,
		),
	}
	operatorClient.Informer().AddEventHandler(c.EventHandler())
	kubeInformersForNamespaces.InformersFor("cluster-kube-descheduler-operator").Core().V1().ConfigMaps().Informer().AddEventHandler(c.EventHandler())

	return nil
}
