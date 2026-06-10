# KubevirtMigrationAware

An `EvictorPlugin` that makes the descheduler aware of KubeVirt live-migration
state when deciding whether to evict `virt-launcher` pods.  It prevents
evictions during active migrations and applies a self-tuning cooldown after
migrations complete, reducing per-VM churn without modifying any other part of
the descheduler.

---

## 1. Problem

### 1.1 The descheduler rebalances by evicting pods — but VMs are not pods

The descheduler measures node utilisation, identifies outlier nodes, and evicts
pods from overloaded nodes so that the scheduler can place them somewhere
better.  For regular stateless workloads this is harmless: the pod restarts
quickly on a new node and the cluster converges.

For KubeVirt virtual machines the same eviction triggers a **live migration**.
The `virt-launcher` pod is evicted, KubeVirt moves the VM's memory and CPU
state to a destination node, and a new `virt-launcher` pod appears there.  The
migration itself consumes CPU, memory bandwidth, and network capacity on both
source and destination nodes — resources that are visible to the very metrics
the descheduler uses to decide its next move.

### 1.2 Per-VM churn: the same VM migrated repeatedly

Without any awareness of migration state, the descheduler can evict the same
VM multiple times in quick succession:

1. VM is on node A (overloaded) → descheduler evicts it → migration starts.
2. Migration completes; VM is now on node B.
3. Node B's utilisation rises because the VM just landed there and is warming
   up its CPU caches and memory working set.
4. Node B now looks like an outlier → descheduler evicts the VM again.
5. Repeat.

This **churn loop** produces more migrations than the cluster needs, degrades
VM performance, and can prevent the cluster from ever reaching a stable state.

### 1.3 Non-convergence at cluster scale

A subtler and harder problem arises when the descheduler's outlier detection
uses cluster-relative thresholds.  If all nodes run at sustained moderate load,
comparing nodes against each other always produces outliers — the cluster looks
imbalanced even when it is as balanced as the workload allows.

Under these conditions the descheduler keeps evicting.  Measurements on a
300-VM cluster running a sustained CPU/RAM stress profile showed roughly
**one eviction per VM per day** even after the cluster was already
post-rebalance.  Raising the outlier margin from 10% to 20% cuts the volume
significantly; this plugin further reduces harm by rate-limiting how often any
individual VM can be evicted — but it does not change the fundamental
convergence property of the outlier algorithm.

**Important:** this plugin addresses *per-VM churn* (the same VM migrated
repeatedly).  It does not reduce *total migration volume*: if 50 VMs are in
cooldown, the descheduler will target the other 50.  For total-volume
reduction, raise the outlier threshold in your descheduler profile.

---

## 2. Architecture

### 2.1 The descheduler is VM-blind by default

The descheduler only watches `Pod` objects.  It has no built-in concept of
VirtualMachineInstances (VMIs), migration state, or whether an eviction will
trigger a live migration or a simple pod restart.

To protect VMs we need the descheduler to consult KubeVirt's VMI status before
evicting.  This plugin provides that bridge.

### 2.2 A dedicated VMI informer — no API-server calls in the hot path

The plugin creates its own `DynamicSharedInformerFactory` that watches
`virtualmachineinstances.kubevirt.io/v1` across all namespaces.  VMI objects
are stored in a local in-memory cache (a standard `client-go` informer store).

Every call to `Filter` or `PreEvictionFilter` reads from this cache — no
API-server round-trip in the eviction hot path.  The cache warms up at plugin
startup with a 30-second timeout; if the VMI informer cannot sync (e.g.
KubeVirt is not installed), the plugin fails fast and prevents the descheduler
from starting.

The plugin also registers an `UpdateFunc` event handler on the same informer.
When a VMI's `status.migrationState.endTimestamp` transitions to a new
non-empty value — meaning a migration just completed — the handler records the
event in an in-memory history map keyed by VMI UID.  This history drives the
exponential backoff described in §3.2.

**Seeding migration history from existing VMIMs at startup.**  Once the VMI
informer cache has warmed up, the plugin performs a one-shot, best-effort
`list` of all `virtualmachineinstancemigrations.kubevirt.io/v1` objects across
all namespaces.  For each VMIM whose `status.phase` is `Succeeded` and whose
`status.migrationState.endTimestamp` falls within the configured
`migrationHistoryWindow`, the plugin resolves the corresponding VMI via the
already-synced VMI cache (using `spec.vmiName` + namespace → VMI UID), then
records the completion in the in-memory history exactly as the live informer
handler would.  Entries are inserted oldest-first to preserve the
ascending-order invariant that the binary-search inside `countAndPrune` relies
on.

This seeding step is **intentionally best-effort**: KubeVirt garbage-collects
completed VMIMs after a cluster-defined TTL, so any completions that predate
the GC window are already absent and cannot be recovered.  The history will
continue to build from live informer events regardless and converge over time.
The primary benefit is after a rolling restart of the descheduler pod: VMIMs
that KubeVirt has not yet GC'd — typically recent completions within the last
few hours — are recovered immediately, shortening the window during which a
chronically bouncing VM could appear to have a clean record.

The link between a `virt-launcher` pod and its VMI is the annotation
`kubevirt.io/domain` that KubeVirt sets on every `virt-launcher` pod.  Its
value is the VMI name.  Pods without this annotation are not `virt-launcher`
pods and pass through both extension points unchanged.

### 2.3 Two extension points: hard block vs. soft defer

The descheduler's evictor pipeline exposes two distinct hooks, and the plugin
uses both for different purposes.

**`Filter` — hard block.**  Called during candidate selection.  Returning
`false` removes the pod from the eviction candidate set entirely for this
descheduler cycle.  The plugin uses this to block eviction of any
`virt-launcher` pod whose VMI has a migration actively in progress
(`startTimestamp` present, `endTimestamp` absent in `migrationState`).

KubeVirt's `virt-api` already provides a complementary safety net: a
validating admission webhook that intercepts eviction requests and rejects
them when a migration is already in progress for that VM.  Our `Filter`
acts upstream of that — it prevents the eviction attempt from being issued
at all, avoiding the API round-trip.  In a distributed environment where
concurrent control loops may race, KubeVirt's webhook remains the
authoritative last line of defence; this plugin is defence-in-depth, not a
replacement.

**`PreEvictionFilter` — soft defer.**  Called immediately before each
individual eviction is issued.  Returning `false` skips this pod and lets the
eviction loop try the next candidate on the same node.  The plugin uses this to
apply the cooldown logic described in §3: if the VM migrated recently and the
cooldown has not expired, the eviction is deferred rather than hard-blocked,
giving other pods on the same node a chance to be evicted instead.

The practical difference: `Filter` stops a pod from being a candidate at all;
`PreEvictionFilter` lets the loop skip a specific pod and try others.  Both
must be enabled in the descheduler profile (see §6).

---

## 3. Cooldown Logic

### Two protection tiers with different durability

Before describing the individual layers it is important to understand that the
cooldown logic has two fundamentally different durability tiers:

**Tier A — VMI-persisted (survives descheduler pod restarts).**
Layer 1 reads `startTimestamp` and `endTimestamp` directly from
`status.migrationState` on the VMI object.  KubeVirt writes and owns this
field; it persists on the VMI regardless of what happens to the descheduler
pod.  A rolling update, an OOM kill, or a node eviction of the descheduler pod
does not erase it.  Critically, this tier also captures the *cost* of the
migration: the difference between the two timestamps tells the plugin how long
that VM took to migrate, which directly raises the cooldown for expensive VMs.

**Tier B — in-memory only (lost on descheduler pod restart).**
Layer 2 keeps a sliding-window history of migration-completion events in the
plugin's process memory.  This drives the exponential backoff: a VM that
migrates repeatedly within the window gets a progressively longer cooldown.
The history is populated by informer events at runtime and is not persisted
anywhere.  If the descheduler pod restarts the history is reset, and VMs that
had accumulated backoff appear clean again — see §8 for the operational
implications.

The two tiers complement each other: Tier A provides a baseline guarantee that
is always present and restart-safe; Tier B adds stronger churn resistance for
VMs that are actively churning and its absence after a restart is typically
short-lived because the in-memory history rebuilds as migrations continue.

---

The cooldown is computed in three layers applied in order.  Each layer can only
increase the effective cooldown; none can reduce it below the previous layer's
result.

### 3.1 Layer 1 — base adaptive cooldown

```
effectiveCooldown = max(migrationCooldown, migrationDuration)
```

`migrationCooldown` is the operator-configured minimum (default **15 minutes**).
`migrationDuration` is `endTimestamp − startTimestamp` read from the VMI's
`status.migrationState`.

The `max` means that heavier VMs — ones that take a long time to migrate
because they have large memory footprints or high dirty-page rates — receive
proportionally longer protection automatically, without any manual per-VM
configuration.

**Examples with default `migrationCooldown = 15m`:**

| VM type | Migration duration | Effective cooldown (layer 1) |
|---|---|---|
| Small idle VM | 30 s | 15 m (configured floor dominates) |
| Medium VM | 10 m | 15 m (configured floor dominates) |
| Large memory VM | 25 m | 25 m (duration dominates) |
| Monster VM | 90 m | 90 m (duration dominates) |

If `startTimestamp` is absent or malformed the migration duration cannot be
computed; the plugin falls back to `migrationCooldown` alone.

### 3.2 Layer 2 — exponential backoff from migration history

```
effectiveCooldown = layer1Result × 2^(count − 1)    (for count ≥ 1)
```

`count` is the number of migration completions recorded for this VMI in the
`migrationHistoryWindow` (default **48 hours**).  The history is populated by
the informer event handler (§2.2), which fires each time a VMI's
`endTimestamp` changes — capturing every migration the cluster runs for that
VMI, not just descheduler-caused ones.

The effect is that each successive migration within the window doubles the
cooldown, making it progressively harder to evict a VM that keeps getting
churned:

**Examples with default `migrationCooldown = 15m`, small VM (duration < 15m):**

| Migrations in last 48h | Effective cooldown (layer 2) |
|---:|---|
| 0 or 1 | 15 m |
| 2 | 30 m |
| 3 | 1 h |
| 4 | 2 h |
| 5 | 4 h |
| 6+ | capped by layer 3 |

A VM that migrates once or twice recovers its normal cooldown naturally as old
history entries age out of the 48-hour window.  A VM that migrates 6+ times in
two days is almost certainly in a churn loop; it gets progressively longer
protection until the loop breaks.

> **Note on the race condition:** the informer event handler fires
> asynchronously.  Between the moment a migration completes and the moment the
> descheduler's next cycle calls `PreEvictionFilter`, the event handler may or
> may not have recorded the completion yet.  In the worst case `count` is
> under-reported by 1, meaning the backoff multiplier is 1× lower than
> expected for a single cycle.  This is intentional and acceptable: the correct
> value is applied in the next cycle.

### 3.3 Layer 3 — maximum cooldown cap

```
effectiveCooldown = min(layer2Result, maxMigrationCooldown)
```

`maxMigrationCooldown` (default **6 hours**) bounds the growth from both the
adaptive duration (layer 1) and the exponential backoff (layer 2).

Without this cap, a VM with 8 migrations in 24 hours on a 15-minute base
cooldown would reach `15m × 2^7 = 32 h`, locking the VM for longer than the
history window itself.  The cap ensures the descheduler always has an
opportunity to re-evaluate after at most 6 hours, regardless of how severe the
churn history is.

To disable the cap, set `maxMigrationCooldown: 0`.  This is not recommended
with a long `migrationHistoryWindow` because backoff can grow unbounded.

**Full worked example — large VM in a churn loop:**

Assume: `migrationCooldown: 15m`, `maxMigrationCooldown: 6h`,
`migrationHistoryWindow: 48h`.  The VM has a 30-minute migration duration.

| Event | Time | count in 48h window | Layer 1 | Layer 2 | Layer 3 (cap) |
|---|---|---:|---|---|---|
| 1st migration completes | T+0 | 1 | 30 m | 30 m | 30 m |
| 2nd migration completes | T+31m | 2 | 30 m | 60 m | 60 m |
| 3rd migration completes | T+2h | 3 | 30 m | 2 h | 2 h |
| 4th migration completes | T+5h | 4 | 30 m | 4 h | 4 h |
| 5th migration completes | T+10h | 5 | 30 m | 8 h | **6 h** (capped) |
| 1st entry ages out at T+48h | T+49h | 4 | 30 m | 4 h | 4 h |

The VM steps back down gradually as history entries age out — it does not
suddenly go from fully protected to fully evictable.

---

## 4. Configuration Reference

All fields are optional.  Omitting a field (or setting it to `0`) causes the
default to apply.

| Field | Default | Valid range | Description |
|---|---|---|---|
| `migrationCooldown` | `15m` | `≥ 0` | Minimum cooldown after any migration. `0` disables the configured floor; the adaptive duration (layer 1) still applies. |
| `maxMigrationCooldown` | `6h` | `≥ migrationCooldown` or `0` | Upper bound on the effective cooldown after all layers. `0` disables the cap. |
| `migrationHistoryWindow` | `48h` | `≥ 0` | Sliding window for migration-count history used by exponential backoff. Must exceed `6 × maxMigrationCooldown` (36h with defaults) to reliably apply the cap in steady state. `0` disables the window (no backoff). |

**Validation rules:**
- `migrationCooldown` must be non-negative.
- `maxMigrationCooldown` must be non-negative.
- If both are non-zero, `maxMigrationCooldown ≥ migrationCooldown` (a cap
  below the floor is a misconfiguration).
- `migrationHistoryWindow` must be non-negative.

### Example configurations

**Conservative — minimal interference, short memory:**
```yaml
migrationCooldown: 5m
maxMigrationCooldown: 1h
migrationHistoryWindow: 6h
```
Suitable for clusters where workloads are expected to migrate frequently for
legitimate reasons (e.g. scheduled maintenance windows) and operators do not
want the backoff to accumulate.

**Default — balanced protection:**
```yaml
# All defaults; these values are applied automatically when fields are omitted.
migrationCooldown: 15m
maxMigrationCooldown: 6h
migrationHistoryWindow: 48h
```

**Protective — strong churn resistance:**
```yaml
migrationCooldown: 30m
maxMigrationCooldown: 12h
migrationHistoryWindow: 72h
```
Suitable for clusters with large memory-intensive VMs where each migration is
expensive and operators want the descheduler to back off aggressively after
repeated evictions.

---

## 5. Profile Setup

The plugin must be listed under **both** `filter` and `preEvictionFilter` in
the descheduler profile.  Listing it under only one extension point silently
disables the other; there is no error.

The `DefaultEvictor` is automatically injected by the descheduler and does not
need to appear in the `plugins` section.  It does however need an explicit
`pluginConfig` entry if you want non-default behaviour.  For KubeVirt workloads
`nodeFit: true` is strongly recommended: it makes the descheduler verify that a
suitable destination node exists before issuing an eviction, preventing a VM
from being evicted into a situation where the scheduler has nowhere valid to
place it.

```yaml
apiVersion: "descheduler/v1alpha2"
kind: "DeschedulerPolicy"
profiles:
  - name: KubevirtRelieveAndMigrate
    pluginConfig:
      - name: KubevirtMigrationAware
        args:
          migrationCooldown: 15m
          maxMigrationCooldown: 6h
          migrationHistoryWindow: 48h
      - name: DefaultEvictor
        args:
          nodeFit: true   # only evict when a valid destination node exists
      - name: LowNodeUtilization
        args:
          thresholds:
            MetricResource: 10
          targetThresholds:
            MetricResource: 10
          useDeviationThresholds: true
    plugins:
      filter:
        enabled:
          - KubevirtMigrationAware      # hard-blocks eviction during active migration
      preEvictionFilter:
        enabled:
          - KubevirtMigrationAware      # soft-defers eviction during cooldown
      balance:
        enabled:
          - LowNodeUtilization
```

The RBAC for the descheduler's service account must include `list` and `watch`
on `virtualmachineinstances` and `list` on `virtualmachineinstancemigrations`
in all namespaces:

```yaml
- apiGroups: ["kubevirt.io"]
  resources: ["virtualmachineinstances"]
  verbs: ["list", "watch"]
- apiGroups: ["kubevirt.io"]
  resources: ["virtualmachineinstancemigrations"]
  verbs: ["list"]
```

The `list` on `virtualmachineinstancemigrations` is needed only for the
one-shot history-seeding call at startup (§2.2).  If this permission is
unavailable the plugin falls back gracefully and builds its history from live
VMI informer events instead.

---

## 6. Observability

### Metrics

The plugin registers two Prometheus metrics.

**`descheduler_kubevirt_eviction_blocks_total`** — counter

Incremented each time the plugin prevents an eviction, labelled by `reason`,
`node`, and `namespace`.

| Label | Values | Description |
|---|---|---|
| `reason` | `migration_in_progress` | Blocked by `Filter`: VMI has an active migration. |
| `reason` | `cooldown` | Blocked by `PreEvictionFilter`: VMI is within its cooldown window. |
| `node` | node name | The node the `virt-launcher` pod was scheduled on. |
| `namespace` | namespace name | The namespace of the `virt-launcher` pod and VMI. |

**`descheduler_kubevirt_effective_cooldown_seconds`** — histogram

Recorded each time a cooldown block is applied, capturing the effective
cooldown duration in seconds.  Bucket boundaries correspond to the exponential
backoff steps under default configuration:
`900` (15 m), `1800` (30 m), `3600` (1 h), `7200` (2 h), `14400` (4 h),
`21600` (6 h).

If most observations land in the `900` bucket, the plugin is applying only the
base cooldown — backoff is not engaging significantly.  If observations shift
toward `21600`, many VMs are hitting the cap, indicating sustained churn that
warrants operator attention.

### Useful PromQL queries

**Is the plugin actively blocking evictions on any node?**
```promql
rate(descheduler_kubevirt_eviction_blocks_total[10m]) > 0
```

**Which nodes are seeing the most cooldown blocks over the last hour?**
```promql
topk(10,
  increase(descheduler_kubevirt_eviction_blocks_total{reason="cooldown"}[1h])
)
```

**Are VMs hitting the cap (6 h bucket) — sign of a churn loop?**
```promql
increase(descheduler_kubevirt_effective_cooldown_seconds_bucket{le="21600"}[1h])
/
increase(descheduler_kubevirt_effective_cooldown_seconds_count[1h])
```
A ratio close to 1.0 means most blocked evictions are at or below 6 hours.
A low ratio means many blocks are coming from the base 15-minute cooldown —
normal expected behaviour.

**Suggested alert — sustained blocking on a single node:**
```promql
rate(descheduler_kubevirt_eviction_blocks_total{reason="cooldown"}[30m]) > 0.1
```
This fires if a node is seeing more than one cooldown block every ~10 minutes
over a 30-minute window, which may indicate that all VMs on that node are in
cooldown and the descheduler cannot rebalance it.

---

## 7. Scheduler–Descheduler Decoupling

The descheduler and the scheduler are independent, stateless components with
no shared state.  When the descheduler evicts a `virt-launcher` pod from an
overloaded node it is making a bet: it hopes the scheduler will place the new
pod somewhere better, but it has no control over — and no visibility into —
where that placement will actually land.

The **soft-tainter** (a separate operator component) tries to close this
information gap by applying `PreferNoSchedule` taints to overloaded nodes,
nudging the scheduler away from them.  This works well for most workloads, but
a VM that carries specific scheduling constraints — `nodeSelector`, `nodeAffinity`,
pod affinity rules, or a narrow set of tolerated taints — can bypass
`PreferNoSchedule` entirely and land back on a suboptimal node regardless.

When that happens this plugin's cooldown mechanism provides a backstop: the VM
that just migrated onto the wrong node will not be immediately re-evicted in a
tight loop.  The exponential backoff described in §3.2 makes each successive
eviction of the same VM progressively harder, giving the cluster time to
stabilise or for an operator to intervene.

However, the cooldown only protects the specific VM that landed badly.  The
descheduler still sees an overloaded node and will continue to evict *other*
eligible VMs from it, which may or may not improve the situation depending on
their scheduling constraints.  The fundamental fix for mis-placed VMs is
correct scheduling configuration, not rate-limiting.

---

## 8. Known Limitations

**Migration history (Tier B) is in-memory; it is partially recovered on restart.**
The migration history that drives exponential backoff (§3, Tier B) is stored
in the plugin's process memory.  If the descheduler pod is restarted — rolling
update, OOM kill, node eviction — the history is reset.

At startup the plugin performs a best-effort `list` of existing
`VirtualMachineInstanceMigration` objects and re-seeds the history from any
Succeeded migrations whose `endTimestamp` falls within the configured window
and whose VMIM KubeVirt has not yet garbage-collected (§2.2).  For a typical
rolling restart this recovers most of the recent history; for an extended
outage, completions that predate KubeVirt's GC TTL will be missing and the
history must rebuild from live events.

The VMI-persisted state (§3, Tier A) — the last migration's start and end
timestamps on the VMI object — is unaffected by a descheduler restart and
continues to enforce the base adaptive cooldown.  The window of vulnerability
is therefore bounded: the per-VM base cooldown (layer 1) remains intact; only
the churn-history multiplier (layer 2) may be partially missing immediately
after restart, until the seeding step and live events have rebuilt it.

**All migrations are counted, not only descheduler-caused ones.**
The informer handler fires on every `endTimestamp` transition, including
migrations triggered by node drain, KubeVirt's own resource management, or
manual operator actions.  A VM that was legitimately drained for hardware
maintenance will enter the backoff history alongside descheduler-driven
evictions.  This is conservative: the plugin may protect a VM that does not
strictly need protection, but it will never fail to protect one that does.

**Cooldown protects individual VMs but does not reduce total migration volume.**
If 50 VMs on a cluster are in cooldown, the descheduler selects the other 50
as candidates.  The total number of migrations on the cluster does not decrease;
the benefit is that no single VM is churned repeatedly.  For clusters where the
outlier threshold keeps finding outliers under sustained load, raising the
outlier margin is a more effective lever than this plugin alone.

**Mixed VM sizes can produce structural non-convergence.**
In a cluster with heterogeneous VM sizes (e.g. 144 × 1 Gi VMs and 1 × 64 Gi
VM), the descheduler may never reach a stable state: migrating the large VM
sharply changes utilisation on both source and destination, which can flip
which nodes satisfy the outlier predicate on the next cycle, causing
back-and-forth movement.  This plugin's adaptive cooldown gives the large VM
more protection (its migration takes longer, so its layer-1 cooldown is
larger), but the surrounding small VMs are still subject to ongoing eviction.
True convergence in this scenario requires VM-size-aware eviction selection,
which is outside the scope of this plugin.

---

## 9. Fail-Open Contract

Every code path that cannot retrieve or parse VMI state allows the eviction to
proceed rather than blocking it.  Specifically:

- Pod has no `kubevirt.io/domain` annotation → not a `virt-launcher` pod → pass through.
- VMI not found in informer cache (cache miss, different namespace, VMI deleted) → pass through.
- VMI object is not of type `*unstructured.Unstructured` → pass through.
- `startTimestamp` or `endTimestamp` is absent or not valid RFC 3339 → treat as no migration record → pass through.

The plugin never hard-fails in a way that would prevent evictions of unrelated
workloads.  If KubeVirt is uninstalled after the plugin starts, the informer
cache goes stale but continues to serve the last known state; cache misses for
new VMIs fail open.

The only hard failure is at startup: if the VMI informer cache does not sync
within 30 seconds, the plugin returns an error and the descheduler does not
start.  This is intentional — operating with a permanently empty or stale cache
would silently remove all VM protections.
