module github.com/openshift/cluster-kube-descheduler-operator

go 1.13

require (
	github.com/ghodss/yaml v1.0.0
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/imdario/mergo v0.3.7
	github.com/openshift/api v0.0.0-20201214114959-164a2fb63b5f
	github.com/openshift/build-machinery-go v0.0.0-20200917070002-f171684f77ab
	github.com/openshift/library-go v0.0.0-20210106214821-c4d0b9c8d55f
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.1.1
	k8s.io/api v0.20.1
	k8s.io/apimachinery v0.20.1
	k8s.io/client-go v0.20.1
	k8s.io/code-generator v0.20.1
	k8s.io/klog/v2 v2.4.0
	sigs.k8s.io/controller-tools v0.2.8
	sigs.k8s.io/descheduler v0.20.1-0.20210127064140-241f1325c968
)
