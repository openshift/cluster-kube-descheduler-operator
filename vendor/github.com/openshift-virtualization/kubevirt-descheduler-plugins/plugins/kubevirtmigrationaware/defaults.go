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

package kubevirtmigrationaware

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	defaultMigrationCooldown      = 15 * time.Minute
	defaultMaxMigrationCooldown   = 6 * time.Hour
	defaultMigrationHistoryWindow = 48 * time.Hour
)

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

func SetDefaults_KubevirtMigrationAwareArgs(obj runtime.Object) {
	args := obj.(*KubevirtMigrationAwareArgs)
	if args.MigrationCooldown.Duration == 0 {
		args.MigrationCooldown = metav1.Duration{Duration: defaultMigrationCooldown}
	}
	if args.MaxMigrationCooldown.Duration == 0 {
		args.MaxMigrationCooldown = metav1.Duration{Duration: defaultMaxMigrationCooldown}
	}
	if args.MigrationHistoryWindow.Duration == 0 {
		args.MigrationHistoryWindow = metav1.Duration{Duration: defaultMigrationHistoryWindow}
	}
}
