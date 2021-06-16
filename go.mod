module github.com/openshift/cluster-kube-descheduler-operator

go 1.16

require (
	github.com/ghodss/yaml v1.0.0
	github.com/imdario/mergo v0.3.7
	github.com/openshift/api v0.0.0-20210730095913-85e1d547cdee
	github.com/openshift/build-machinery-go v0.0.0-20210712174854-1bb7fd1518d3
	github.com/openshift/client-go v0.0.0-20210730113412-1811c1b3fc0e
	github.com/openshift/library-go v0.0.0-20210730114916-d82fae7e3feb
	github.com/prometheus/client_golang v1.11.0
	github.com/spf13/cobra v1.1.3
	k8s.io/api v0.22.0-rc.0
	k8s.io/apimachinery v0.22.0-rc.0
	k8s.io/client-go v0.22.0-rc.0
	k8s.io/code-generator v0.22.0-rc.0
	k8s.io/klog/v2 v2.9.0
	sigs.k8s.io/controller-tools v0.2.8
	sigs.k8s.io/descheduler v0.20.1-0.20210127064140-241f1325c968
)
