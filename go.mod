module github.com/openshift/cluster-kube-descheduler-operator

go 1.13

require (
	github.com/jteeuwen/go-bindata v3.0.8-0.20151023091102-a0ff2567cfb7+incompatible
	github.com/kubernetes-incubator/descheduler v0.8.0 // indirect
	github.com/openshift/api v0.0.0-20200116145750-0e2ff1e215dd
	github.com/openshift/library-go v0.0.0-20200123173517-9d0011759106
	github.com/prometheus/client_golang v1.3.0
	github.com/spf13/cobra v0.0.5
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.17.2
	k8s.io/apimachinery v0.17.2
	k8s.io/client-go v0.17.2
	k8s.io/klog v1.0.0
	sigs.k8s.io/descheduler v0.8.1-0.20200124153632-e3865fcf8e80
)
