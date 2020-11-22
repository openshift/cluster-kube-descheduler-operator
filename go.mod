module github.com/openshift/cluster-kube-descheduler-operator

go 1.13

require (
	github.com/ghodss/yaml v1.0.0
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/openshift/api v0.0.0-20201119144013-9f0856e7c657
	github.com/openshift/build-machinery-go v0.0.0-20200917070002-f171684f77ab
	github.com/openshift/library-go v0.0.0-20201119162840-a8387fdfa05b
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.1.1
	k8s.io/api v0.20.0-beta.2
	k8s.io/apimachinery v0.20.0-beta.2
	k8s.io/client-go v0.20.0-beta.2
	k8s.io/code-generator v0.20.0-beta.2
	k8s.io/klog/v2 v2.4.0
	sigs.k8s.io/controller-tools v0.2.8
	sigs.k8s.io/descheduler v0.19.1-0.20201121111933-c1323719f4c4
)
