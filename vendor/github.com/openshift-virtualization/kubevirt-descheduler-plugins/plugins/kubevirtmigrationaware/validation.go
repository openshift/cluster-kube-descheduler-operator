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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
)

func ValidateKubevirtMigrationAwareArgs(obj runtime.Object) error {
	args := obj.(*KubevirtMigrationAwareArgs)
	if args.MigrationCooldown.Duration < 0 {
		return fmt.Errorf("migrationCooldown must be non-negative, got %v", args.MigrationCooldown.Duration)
	}
	if args.MaxMigrationCooldown.Duration < 0 {
		return fmt.Errorf("maxMigrationCooldown must be non-negative, got %v", args.MaxMigrationCooldown.Duration)
	}
	if args.MaxMigrationCooldown.Duration > 0 && args.MaxMigrationCooldown.Duration < args.MigrationCooldown.Duration {
		return fmt.Errorf("maxMigrationCooldown (%v) must be >= migrationCooldown (%v)",
			args.MaxMigrationCooldown.Duration, args.MigrationCooldown.Duration)
	}
	if args.MigrationHistoryWindow.Duration < 0 {
		return fmt.Errorf("migrationHistoryWindow must be non-negative, got %v", args.MigrationHistoryWindow.Duration)
	}
	return nil
}
