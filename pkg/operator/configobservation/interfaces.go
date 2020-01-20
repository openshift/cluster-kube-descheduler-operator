package configobservation

import (
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
)

type Listers struct {
	ResourceSync       resourcesynccontroller.ResourceSyncer
	PreRunCachesSynced []cache.InformerSynced
}

func (l Listers) ResourceSyncer() resourcesynccontroller.ResourceSyncer {
	return l.ResourceSync
}

func (l Listers) PreRunHasSynced() []cache.InformerSynced {
	return l.PreRunCachesSynced
}
