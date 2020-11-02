package descheduler

import (
	"github.com/ghodss/yaml"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/configobservation"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/library-go/pkg/operator/configobserver"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
)

func ObserveDeschedulerConfig(genericListers configobserver.Listers, recorder events.Recorder, existingConfig map[string]interface{}) (map[string]interface{}, []error) {
	listers := genericListers.(configobservation.Listers)
	errs := []error{}
	observedConfig := map[string]interface{}{}

	// Get the descheduler policy configmap from the descheduler operator config
	deschedulerConfig, err := listers.DeschedulerLister.KubeDeschedulers(operatorclient.OperatorNamespace).Get("cluster")
	if errors.IsNotFound(err) {
		klog.Warningf("deschedulers/cluster: not found")
		return observedConfig, errs
	}
	if err != nil {
		errs = append(errs, err)
		return existingConfig, errs
	}
	configMapName := deschedulerConfig.Spec.Policy.Name

	// Get the policy configmap
	configMap, err := listers.ConfigmapLister.ConfigMaps(operatorclient.OperatorNamespace).Get(configMapName)
	if errors.IsNotFound(err) {
		klog.Warningf("descheduler policy configmap '%s' not found", configMapName)
		return observedConfig, errs
	}
	if err != nil {
		errs = append(errs, err)
		return existingConfig, errs
	}

	// Parse the policy data from the configmap
	if err := yaml.Unmarshal([]byte(configMap.Data["policy.cfg"]), &observedConfig); err != nil {
		errs = append(errs, err)
		return existingConfig, errs
	}

	return observedConfig, errs
}
