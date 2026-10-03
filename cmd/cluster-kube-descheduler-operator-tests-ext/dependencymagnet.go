// This file imports test packages to ensure they are included in the build.
// These imports are necessary to register Ginkgo tests with the OpenShift Tests Extension framework.
package main

import (
	// Import test packages to register Ginkgo tests
	_ "github.com/openshift/cluster-kube-descheduler-operator/test/e2e"
)
