module github.com/openshift/cluster-kube-descheduler-operator

go 1.13

require (
	github.com/ghodss/yaml v1.0.0
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/openshift/api v0.0.0-20200728130943-c9b966e1d6a4
	github.com/openshift/build-machinery-go v0.0.0-20200713135615-1f43d26dccc7
	github.com/openshift/library-go v0.0.0-20200728084749-958c008826cb
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.0.0
	k8s.io/api v0.19.0-rc.2
	k8s.io/apimachinery v0.19.0-rc.2
	k8s.io/client-go v0.19.0-rc.2
	k8s.io/code-generator v0.19.0-rc.2
	k8s.io/klog/v2 v2.2.0
	sigs.k8s.io/descheduler v0.18.1-0.20200728174747-d0fbebb77c79
)
