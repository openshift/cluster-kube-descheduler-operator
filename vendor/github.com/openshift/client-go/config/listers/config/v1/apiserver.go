// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/listers"
	"k8s.io/client-go/tools/cache"
)

// APIServerLister helps list APIServers.
// All objects returned here must be treated as read-only.
type APIServerLister interface {
	// List lists all APIServers in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1.APIServer, err error)
	// Get retrieves the APIServer from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1.APIServer, error)
	APIServerListerExpansion
}

// aPIServerLister implements the APIServerLister interface.
type aPIServerLister struct {
	listers.ResourceIndexer[*v1.APIServer]
}

// NewAPIServerLister returns a new APIServerLister.
func NewAPIServerLister(indexer cache.Indexer) APIServerLister {
	return &aPIServerLister{listers.New[*v1.APIServer](indexer, v1.Resource("apiserver"))}
}