module github.com/openshift/cluster-kube-descheduler-operator

go 1.13

require (
	github.com/ghodss/yaml v1.0.0
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/openshift/api master
	github.com/openshift/build-machinery-go master
	github.com/openshift/library-go master
	github.com/prometheus/client_golang v1.3.0
	github.com/spf13/cobra v0.0.5
	k8s.io/api v0.19.0-rc.2
	k8s.io/apimachinery v0.19.0-rc.2
	k8s.io/client-go v0.19.0-rc.2
	k8s.io/code-generator v0.19.0-rc.2
	k8s.io/klog v1.0.0
	sigs.k8s.io/descheduler master
)
