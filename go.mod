module github.com/openshift/cluster-kube-descheduler-operator

go 1.16

require (
	github.com/ghodss/yaml v1.0.0
	github.com/imdario/mergo v0.3.7
	github.com/openshift/api v0.0.0-20210331193751-3acddb19d360
	github.com/openshift/build-machinery-go v0.0.0-20210209125900-0da259a2c359
	github.com/openshift/library-go v0.0.0-20210331235027-66936e2fcc52
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.1.1
	k8s.io/api v0.21.0-rc.0
	k8s.io/apimachinery v0.21.0-rc.0
	k8s.io/client-go v0.21.0-rc.0
	k8s.io/code-generator v0.21.0-rc.0
	k8s.io/klog/v2 v2.8.0
	sigs.k8s.io/controller-tools v0.2.8
	sigs.k8s.io/descheduler v0.20.1-0.20210127064140-241f1325c968
)
