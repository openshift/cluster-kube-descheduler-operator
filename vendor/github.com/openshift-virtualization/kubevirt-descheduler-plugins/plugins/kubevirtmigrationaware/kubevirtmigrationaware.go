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

// Package kubevirtmigrationaware provides an EvictorPlugin that prevents the
// descheduler from evicting virt-launcher pods while a VM live-migration is in
// progress (Filter) and suppresses re-eviction during a configurable cooldown
// period after the migration completes (PreEvictionFilter).
//
// Both extension points operate on the per-VMI migrationState recorded in the
// VMI status, which is kept in a local informer cache to avoid API-server load
// in the hot eviction path.
package kubevirtmigrationaware

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	frameworktypes "sigs.k8s.io/descheduler/pkg/framework/types"
)

const (
	PluginName = "KubevirtMigrationAware"

	// virt-launcher pods carry this annotation with the VMI name as value.
	vmiAnnotationKey = "kubevirt.io/domain"

	// VMI and VMIM GVRs in the kubevirt.io API group.
	vmiGroup    = "kubevirt.io"
	vmiVersion  = "v1"
	vmiResource  = "virtualmachineinstances"
	vmimResource = "virtualmachineinstancemigrations"

	// Timeout for the initial informer cache sync at plugin startup.
	cacheWarmupTimeout = 30 * time.Second

	reasonMigrationInProgress = "migration_in_progress"
	reasonCooldown            = "cooldown"
)

var (
	vmiGVR = schema.GroupVersionResource{
		Group:    vmiGroup,
		Version:  vmiVersion,
		Resource: vmiResource,
	}

	vmimGVR = schema.GroupVersionResource{
		Group:    vmiGroup,
		Version:  vmiVersion,
		Resource: vmimResource,
	}

	// evictionBlocksTotal counts how many times the plugin prevented an eviction,
	// labelled by reason, node, and namespace.  Use this to identify which nodes
	// or tenant namespaces are experiencing repeated eviction gating.
	evictionBlocksTotal = k8smetrics.NewCounterVec(
		&k8smetrics.CounterOpts{
			Subsystem:      "descheduler",
			Name:           "kubevirt_eviction_blocks_total",
			Help:           "Number of virt-launcher pod evictions blocked by KubevirtMigrationAware, by reason, node, and namespace.",
			StabilityLevel: k8smetrics.ALPHA,
		},
		[]string{"reason", "node", "namespace"},
	)

	// effectiveCooldownSeconds is a histogram of the adaptive cooldown durations
	// applied when deferring evictions.  Bucket boundaries match the exponential
	// backoff steps with default configuration (15m base, 6h cap), so the
	// distribution directly shows whether VMs are hitting the base cooldown or
	// being pushed toward the cap by repeated migrations.
	effectiveCooldownSeconds = k8smetrics.NewHistogram(
		&k8smetrics.HistogramOpts{
			Subsystem:      "descheduler",
			Name:           "kubevirt_effective_cooldown_seconds",
			Help:           "Distribution of effective cooldown durations applied when deferring virt-launcher pod evictions, in seconds.",
			StabilityLevel: k8smetrics.ALPHA,
			Buckets:        []float64{900, 1800, 3600, 7200, 14400, 21600}, // 15m 30m 1h 2h 4h 6h
		},
	)

	registerMetricsOnce sync.Once
)

// migrationHistory tracks per-VMI migration completion timestamps within a
// sliding window so that the plugin can apply exponential backoff to VMs that
// are migrated repeatedly.
type migrationHistory struct {
	mu          sync.Mutex
	completions map[types.UID][]time.Time // sorted ascending; pruned lazily
}

func newMigrationHistory() *migrationHistory {
	return &migrationHistory{completions: make(map[types.UID][]time.Time)}
}

// record appends a migration completion event for the given VMI.
func (h *migrationHistory) record(uid types.UID, t time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.completions[uid] = append(h.completions[uid], t)
}

// countAndPrune returns the number of migrations recorded for uid within
// window, pruning expired entries in the process.
func (h *migrationHistory) countAndPrune(uid types.UID, window time.Duration) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	ts := h.completions[uid]
	if len(ts) == 0 {
		return 0
	}
	cutoff := time.Now().Add(-window)
	i := sort.Search(len(ts), func(i int) bool { return ts[i].After(cutoff) })
	if i == len(ts) {
		delete(h.completions, uid)
		return 0
	}
	if i > 0 {
		h.completions[uid] = ts[i:]
	}
	return len(h.completions[uid])
}

// onVMIUpdate is the informer UpdateFunc handler.  It records a migration
// completion when endTimestamp transitions to a new non-empty value.
func (h *migrationHistory) onVMIUpdate(oldObj, newObj interface{}) {
	oldU, ok := oldObj.(*unstructured.Unstructured)
	if !ok {
		return
	}
	newU, ok := newObj.(*unstructured.Unstructured)
	if !ok {
		return
	}
	oldEnd, _, _ := unstructured.NestedString(oldU.Object, "status", "migrationState", "endTimestamp")
	newEnd, _, _ := unstructured.NestedString(newU.Object, "status", "migrationState", "endTimestamp")
	if newEnd == "" || newEnd == oldEnd {
		return
	}
	t, err := time.Parse(time.RFC3339, newEnd)
	if err != nil {
		t = time.Now()
	}
	h.record(newU.GetUID(), t)
}

// KubevirtMigrationAware is an EvictorPlugin.
//
//   - Filter: hard-blocks eviction of virt-launcher pods whose VMI is
//     currently mid-migration (startTimestamp set, endTimestamp absent).
//
//   - PreEvictionFilter: soft-blocks eviction of virt-launcher pods whose VMI
//     completed a migration within the configured MigrationCooldown window,
//     allowing the eviction loop to skip and try other candidates instead.
type KubevirtMigrationAware struct {
	logger    klog.Logger
	handle    frameworktypes.Handle
	args      *KubevirtMigrationAwareArgs
	vmiLister cache.GenericLister
	history   *migrationHistory
}

var _ frameworktypes.EvictorPlugin = &KubevirtMigrationAware{}

// New builds the plugin from its arguments.
// It creates a dedicated dynamic client and VMI informer so that Filter and
// PreEvictionFilter can read VMI state from a local cache instead of hitting
// the API server on every eviction decision.
func New(ctx context.Context, args runtime.Object, handle frameworktypes.Handle) (frameworktypes.Plugin, error) {
	kmaArgs, ok := args.(*KubevirtMigrationAwareArgs)
	if !ok {
		return nil, fmt.Errorf("want args to be of type KubevirtMigrationAwareArgs, got %T", args)
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build in-cluster REST config: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create an all-namespaces informer factory with no re-sync (0 = disabled).
	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynClient, 0)
	vmiGenericInformer := factory.ForResource(vmiGVR)

	history := newMigrationHistory()
	if _, err = vmiGenericInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			history.onVMIUpdate(oldObj, newObj)
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to register VMI event handler: %w", err)
	}

	factory.Start(ctx.Done())

	syncCtx, cancel := context.WithTimeout(ctx, cacheWarmupTimeout)
	defer cancel()
	if !cache.WaitForCacheSync(syncCtx.Done(), vmiGenericInformer.Informer().HasSynced) {
		return nil, fmt.Errorf("timed out waiting for VMI informer cache to sync (is KubeVirt installed?)")
	}

	// Best-effort: pre-populate history from VMIM objects that already exist in
	// the cluster.  This shortens the ramp-up period after a descheduler restart
	// by recovering as much backoff state as possible.  VMIMs are periodically
	// garbage-collected by KubeVirt, so completions that predate the GC window
	// will be missing — the history will continue to build from live informer
	// events and converge over time regardless.
	//
	// Requires list permission on virtualmachineinstancemigrations at the cluster
	// level (all namespaces).
	logger := klog.FromContext(ctx).WithValues("plugin", PluginName)
	listCtx, listCancel := context.WithTimeout(ctx, cacheWarmupTimeout)
	defer listCancel()
	if vmimList, err := dynClient.Resource(vmimGVR).List(listCtx, metav1.ListOptions{}); err != nil {
		logger.V(2).Info("cannot list VMIM objects; migration history will build from live events only", "err", err)
	} else {
		seedHistoryFromVMIMs(vmimList.Items, vmiGenericInformer.Lister(), history, kmaArgs.MigrationHistoryWindow.Duration, logger)
	}

	return newPlugin(ctx, kmaArgs, handle, vmiGenericInformer.Lister(), history)
}

// seedHistoryFromVMIMs pre-populates migration history from a snapshot of
// existing VMIM objects.  Only Succeeded migrations whose endTimestamp falls
// within the history window are recorded; everything else is silently skipped.
//
// Entries are inserted oldest-first so that each per-VMI slice in
// migrationHistory stays sorted ascending, preserving the binary-search
// invariant that countAndPrune relies on.
//
// This is called once at startup (see New) and is intentionally best-effort:
// KubeVirt garbage-collects VMIMs after completion, so any completions that
// occurred before the GC window will already be absent from the list.
func seedHistoryFromVMIMs(items []unstructured.Unstructured, lister cache.GenericLister, history *migrationHistory, window time.Duration, logger klog.Logger) {
	cutoff := time.Now().Add(-window)

	type entry struct {
		uid types.UID
		t   time.Time
	}
	var entries []entry

	for i := range items {
		item := &items[i]

		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		if phase != "Succeeded" {
			continue
		}

		endTS, _, _ := unstructured.NestedString(item.Object, "status", "migrationState", "endTimestamp")
		if endTS == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, endTS)
		if err != nil || !t.After(cutoff) {
			continue
		}

		vmiName, _, _ := unstructured.NestedString(item.Object, "spec", "vmiName")
		if vmiName == "" {
			continue
		}

		rObj, err := lister.ByNamespace(item.GetNamespace()).Get(vmiName)
		if err != nil {
			continue // VMI may have been deleted already
		}
		uObj, ok := rObj.(*unstructured.Unstructured)
		if !ok {
			continue
		}

		entries = append(entries, entry{uid: uObj.GetUID(), t: t})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].t.Before(entries[j].t) })
	for _, e := range entries {
		history.record(e.uid, e.t)
	}

	logger.V(2).Info("migration history seeded from VMIM objects (best-effort)",
		"considered", len(items), "seeded", len(entries))
}

// newPlugin is the internal constructor used by both New (production) and tests.
// Tests call this directly with a pre-populated fake lister, bypassing the
// dynamic client and in-cluster config entirely.
func newPlugin(ctx context.Context, args *KubevirtMigrationAwareArgs, handle frameworktypes.Handle, lister cache.GenericLister, history *migrationHistory) (frameworktypes.Plugin, error) {
	logger := klog.FromContext(ctx).WithValues("plugin", PluginName)

	registerMetricsOnce.Do(func() {
		legacyregistry.MustRegister(evictionBlocksTotal, effectiveCooldownSeconds)
	})

	logger.V(2).Info("VMI lister ready",
		"cooldown", args.MigrationCooldown.Duration,
		"maxCooldown", args.MaxMigrationCooldown.Duration,
		"historyWindow", args.MigrationHistoryWindow.Duration)

	return &KubevirtMigrationAware{
		logger:    logger,
		handle:    handle,
		args:      args,
		vmiLister: lister,
		history:   history,
	}, nil
}

// Name returns the plugin name.
func (k *KubevirtMigrationAware) Name() string {
	return PluginName
}

// Filter returns false (block eviction) for virt-launcher pods whose
// corresponding VMI is currently mid-migration.  Non-virt-launcher pods and
// any VMI lookup failure are passed through (fail open).
func (k *KubevirtMigrationAware) Filter(pod *v1.Pod) bool {
	uObj, ok := k.getVMI(pod)
	if !ok {
		return true
	}

	if migrationInProgress(uObj) {
		k.logger.V(3).Info("VMI migration in progress, blocking eviction",
			"pod", klog.KObj(pod), "vmi", pod.Annotations[vmiAnnotationKey], "node", pod.Spec.NodeName)
		evictionBlocksTotal.WithLabelValues(reasonMigrationInProgress, pod.Spec.NodeName, pod.Namespace).Inc()
		return false
	}

	return true
}

// PreEvictionFilter returns false (defer eviction) for virt-launcher pods
// whose corresponding VMI completed a migration within the effective cooldown
// window.
//
// The effective cooldown is computed in three steps:
//  1. Base: max(MigrationCooldown, migration duration) — heavier VMs get longer
//     protection automatically.
//  2. Exponential backoff: the base is doubled for each additional migration
//     recorded in the 6-hour history window, so repeatedly migrated VMs are
//     progressively protected against churn.
//  3. Cap: if MaxMigrationCooldown is non-zero, the result is capped there.
func (k *KubevirtMigrationAware) PreEvictionFilter(pod *v1.Pod) bool {
	uObj, ok := k.getVMI(pod)
	if !ok {
		return true
	}

	endTime, ok := migrationEndTime(uObj)
	if !ok {
		return true
	}

	// Step 1: base = max(configured, migration duration).
	effectiveCooldown := k.args.MigrationCooldown.Duration
	if startTime, hasStart := migrationStartTime(uObj); hasStart {
		if d := endTime.Sub(*startTime); d > effectiveCooldown {
			effectiveCooldown = d
		}
	}

	// Step 2: double the cooldown for each migration beyond the first recorded
	// in the history window.  Uses an overflow-safe doubling loop.
	count := k.history.countAndPrune(uObj.GetUID(), k.args.MigrationHistoryWindow.Duration)
	for i := 1; i < count; i++ {
		next := effectiveCooldown * 2
		if next/2 != effectiveCooldown { // int64 overflow guard
			break
		}
		effectiveCooldown = next
		if max := k.args.MaxMigrationCooldown.Duration; max > 0 && effectiveCooldown >= max {
			break // cap will be applied in step 3; no point doubling further
		}
	}

	// Step 3: apply the optional upper bound.
	if maxCooldown := k.args.MaxMigrationCooldown.Duration; maxCooldown > 0 && effectiveCooldown > maxCooldown {
		effectiveCooldown = maxCooldown
	}

	elapsed := time.Since(*endTime)
	if elapsed < effectiveCooldown {
		remaining := effectiveCooldown - elapsed
		k.logger.V(3).Info("VMI in migration cooldown, deferring eviction",
			"pod", klog.KObj(pod), "vmi", pod.Annotations[vmiAnnotationKey], "node", pod.Spec.NodeName,
			"migrationCount", count,
			"elapsed", elapsed.Round(time.Second),
			"effectiveCooldown", effectiveCooldown.Round(time.Second),
			"remaining", remaining.Round(time.Second))
		evictionBlocksTotal.WithLabelValues(reasonCooldown, pod.Spec.NodeName, pod.Namespace).Inc()
		effectiveCooldownSeconds.Observe(effectiveCooldown.Seconds())
		return false
	}

	return true
}

// getVMI looks up the VMI for a virt-launcher pod from the informer cache.
// Returns (nil, false) for non-virt-launcher pods and on any lookup error
// (fail open: the pod is not blocked from eviction).
func (k *KubevirtMigrationAware) getVMI(pod *v1.Pod) (*unstructured.Unstructured, bool) {
	vmiName, ok := pod.Annotations[vmiAnnotationKey]
	if !ok {
		return nil, false
	}

	rObj, err := k.vmiLister.ByNamespace(pod.Namespace).Get(vmiName)
	if err != nil {
		k.logger.V(4).Info("VMI not found in cache, allowing eviction",
			"pod", klog.KObj(pod), "vmi", vmiName, "err", err)
		return nil, false
	}

	uObj, ok := rObj.(*unstructured.Unstructured)
	if !ok {
		k.logger.V(4).Info("Unexpected VMI object type, allowing eviction",
			"pod", klog.KObj(pod), "vmi", vmiName, "type", fmt.Sprintf("%T", rObj))
		return nil, false
	}

	return uObj, true
}

// migrationInProgress returns true when the VMI has a migration that has
// started but not yet finished (startTimestamp present, endTimestamp absent).
func migrationInProgress(uObj *unstructured.Unstructured) bool {
	startTS, found, _ := unstructured.NestedString(uObj.Object, "status", "migrationState", "startTimestamp")
	if !found || startTS == "" {
		return false
	}
	endTS, found, _ := unstructured.NestedString(uObj.Object, "status", "migrationState", "endTimestamp")
	return !found || endTS == ""
}

// migrationStartTime returns the time at which the last migration started.
// Returns (nil, false) when no startTimestamp is recorded or it is malformed.
func migrationStartTime(uObj *unstructured.Unstructured) (*time.Time, bool) {
	startTS, found, _ := unstructured.NestedString(uObj.Object, "status", "migrationState", "startTimestamp")
	if !found || startTS == "" {
		return nil, false
	}
	t, err := time.Parse(time.RFC3339, startTS)
	if err != nil {
		return nil, false
	}
	return &t, true
}

// migrationEndTime returns the time at which the last migration completed.
// Returns (nil, false) when there is no completed migration record.
func migrationEndTime(uObj *unstructured.Unstructured) (*time.Time, bool) {
	endTS, found, _ := unstructured.NestedString(uObj.Object, "status", "migrationState", "endTimestamp")
	if !found || endTS == "" {
		return nil, false
	}
	t, err := time.Parse(time.RFC3339, endTS)
	if err != nil {
		return nil, false
	}
	return &t, true
}
