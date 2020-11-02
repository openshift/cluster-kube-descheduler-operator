package configobservation

import (
	configlistersv1beta1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/listers/descheduler/v1beta1"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	corelistersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type Listers struct {
	ConfigmapLister    corelistersv1.ConfigMapLister
	DeschedulerLister  configlistersv1beta1.KubeDeschedulerLister
	ResourceSync       resourcesynccontroller.ResourceSyncer
	PreRunCachesSynced []cache.InformerSynced
}

func (l Listers) ResourceSyncer() resourcesynccontroller.ResourceSyncer {
	return l.ResourceSync
}

func (l Listers) PreRunHasSynced() []cache.InformerSynced {
	return l.PreRunCachesSynced
}
