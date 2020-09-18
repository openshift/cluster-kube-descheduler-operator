module github.com/openshift/cluster-kube-descheduler-operator

go 1.13

require (
	github.com/ghodss/yaml v1.0.0
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/openshift/api v0.0.0-20200827090112-c05698d102cf
	github.com/openshift/build-machinery-go v0.0.0-20200819073603-48aa266c95f7
	github.com/openshift/library-go v0.0.0-20200728084749-958c008826cb
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.0.0
	k8s.io/api v0.19.0
	k8s.io/apimachinery v0.19.0
	k8s.io/client-go v0.19.0
	k8s.io/code-generator v0.19.0
	k8s.io/klog/v2 v2.3.0
	sigs.k8s.io/descheduler v0.19.0
)

replace (
	github.com/openshift/api => github.com/damemi/api v0.0.0-20200918135733-891e920f0424
	github.com/openshift/client-go => github.com/damemi/client-go v0.0.0-20200918144855-c1594bef24b2
)
