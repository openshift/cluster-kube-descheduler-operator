/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package kubevirtplugins provides the registration entry point for all
// KubeVirt-aware descheduler plugins in this repository.
//
// Consumers (e.g. openshift/descheduler) import this package and call
// Register once, during plugin setup, to add every plugin in this repo
// to the descheduler's plugin registry.  Adding a new plugin to this
// repository requires only a new pluginregistry.Register call here —
// the carry commit in the downstream descheduler does not need to change.
package kubevirtplugins

import (
	"github.com/openshift-virtualization/kubevirt-descheduler-plugins/plugins/kubevirtmigrationaware"
	"sigs.k8s.io/descheduler/pkg/framework/pluginregistry"
)

// Register adds all KubeVirt-aware plugins to registry.
// Call this from the descheduler's RegisterDefaultPlugins function.
func Register(registry pluginregistry.Registry) {
	pluginregistry.Register(
		kubevirtmigrationaware.PluginName,
		kubevirtmigrationaware.New,
		&kubevirtmigrationaware.KubevirtMigrationAware{},
		&kubevirtmigrationaware.KubevirtMigrationAwareArgs{},
		kubevirtmigrationaware.ValidateKubevirtMigrationAwareArgs,
		kubevirtmigrationaware.SetDefaults_KubevirtMigrationAwareArgs,
		registry,
	)
}
