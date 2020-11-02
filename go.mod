module github.com/openshift/cluster-kube-descheduler-operator

go 1.13

require (
	github.com/ghodss/yaml v1.0.0
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/openshift/api v0.0.0-20201102162614-9252afb032e1
	github.com/openshift/build-machinery-go v0.0.0-20200917070002-f171684f77ab
	github.com/openshift/library-go v0.0.0-20201102091359-c4fa0f5b3a08
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.0.0
	k8s.io/api v0.19.2
	k8s.io/apimachinery v0.19.2
	k8s.io/client-go v0.19.2
	k8s.io/code-generator v0.19.2
	k8s.io/klog/v2 v2.3.0
	sigs.k8s.io/controller-tools v0.4.0
	sigs.k8s.io/descheduler v0.19.0
)
