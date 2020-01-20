package controller

import (
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/controller/descheduler"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, descheduler.Add)
}
