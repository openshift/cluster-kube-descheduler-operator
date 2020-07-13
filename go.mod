module github.com/openshift/cluster-kube-descheduler-operator

go 1.13

require (
	github.com/ghodss/yaml v1.0.0
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/openshift/api v0.0.0-20200518100801-1de8998c0357
	github.com/openshift/build-machinery-go v0.0.0-20200512074546-3744767c4131
	github.com/openshift/library-go v0.0.0-20200518140451-8b2ad0d4eeef
	github.com/prometheus/client_golang v1.3.0
	github.com/spf13/cobra v0.0.5
	k8s.io/api v0.18.4
	k8s.io/apimachinery v0.18.4
	k8s.io/client-go v0.18.4
	k8s.io/code-generator v0.18.4
	k8s.io/klog v1.0.0
	sigs.k8s.io/descheduler v0.18.1-0.20200709083202-05c69ee26a9b
)
