module github.com/openshift/cluster-kube-descheduler-operator

go 1.13

require (
	github.com/ghodss/yaml v1.0.0
	github.com/jteeuwen/go-bindata v3.0.8-0.20151023091102-a0ff2567cfb7+incompatible
	github.com/openshift/api v0.0.0-20200327145400-6efe1ee417e1
	github.com/openshift/build-machinery-go v0.0.0-20200211121458-5e3d6e570160
	github.com/openshift/library-go v0.0.0-20200327125526-163b2f0d6264
	github.com/prometheus/client_golang v1.3.0
	github.com/spf13/cobra v0.0.5
	k8s.io/api v0.18.0
	k8s.io/apimachinery v0.18.0
	k8s.io/client-go v0.18.0
	k8s.io/code-generator v0.18.0
	k8s.io/klog v1.0.0
	sigs.k8s.io/descheduler v0.10.1-0.20200507191943-31c7855212e9
)
