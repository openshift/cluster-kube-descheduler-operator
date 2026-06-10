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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KubevirtMigrationAwareArgs holds arguments used to configure the
// KubevirtMigrationAware plugin.
type KubevirtMigrationAwareArgs struct {
	metav1.TypeMeta `json:",inline"`

	// MigrationCooldown is the minimum duration that must elapse after a VM
	// live-migration completes before the descheduler may evict the virt-launcher
	// pod again.  The effective per-VM cooldown is max(MigrationCooldown,
	// migration duration), so heavier VMs automatically receive longer protection.
	// Defaults to 15m.
	MigrationCooldown metav1.Duration `json:"migrationCooldown,omitempty"`

	// MaxMigrationCooldown caps the adaptive per-VM cooldown computed as
	// max(MigrationCooldown, migration duration) after exponential backoff is
	// applied.  Use this to prevent pathological cases (very slow migrations or
	// heavy churn) from locking a VM indefinitely.  Defaults to 6h.
	MaxMigrationCooldown metav1.Duration `json:"maxMigrationCooldown,omitempty"`

	// MigrationHistoryWindow is the sliding window over which past migration
	// completions are counted for exponential-backoff purposes.  Longer windows
	// make the plugin sensitive to day-scale churn; shorter windows let VMs
	// recover their clean record faster.
	//
	// The window must exceed 6 × MaxMigrationCooldown (36h with defaults) so
	// that in steady state the count within the window stays above 6 and the
	// MaxMigrationCooldown cap is reliably applied to chronically bouncing VMs.
	// Defaults to 48h.
	MigrationHistoryWindow metav1.Duration `json:"migrationHistoryWindow,omitempty"`
}
