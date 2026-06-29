# Architecture - OpenShift Kube Descheduler Operator

## Overview

The OpenShift Kube Descheduler Operator manages the Kubernetes Descheduler within OpenShift clusters. It reconciles a singleton `KubeDescheduler` custom resource and deploys/configures the descheduler workload to optimize cluster resource utilization by evicting pods according to configured strategies. The descheduler evicts pods; the scheduler then decides where to reschedule them (rescheduling is not guaranteed).

**Key Responsibilities:**
- Deploy and manage the descheduler deployment
- Translate high-level descheduler profiles into upstream strategy configurations
- Support both hard eviction (Automatic mode) and soft tainting (via KubeVirtRelieveAndMigrate profile)
- Manage descheduler lifecycle, upgrades, and configuration updates
- Report operator status via ClusterOperator resource

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    OpenShift Cluster                            │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  openshift-kube-descheduler-operator namespace           │  │
│  │                                                           │  │
│  │  ┌────────────────────────────────────┐                  │  │
│  │  │  Descheduler Operator Pod          │                  │  │
│  │  │  ┌──────────────────────────────┐  │                  │  │
│  │  │  │  target_config_reconciler    │  │                  │  │
│  │  │  │  - Reconciles KubeDescheduler│  │                  │  │
│  │  │  │  - Generates descheduler cfg │  │                  │  │
│  │  │  │  - Manages deployment        │  │                  │  │
│  │  │  └──────────────────────────────┘  │                  │  │
│  │  └────────────────────────────────────┘                  │  │
│  │           │                                               │  │
│  │           │ watches/creates                               │  │
│  │           ▼                                               │  │
│  │  ┌────────────────────────────────────┐                  │  │
│  │  │  Descheduler Deployment            │                  │  │
│  │  │  - Runs descheduler strategies     │                  │  │
│  │  │  - Evicts pods (Automatic mode)    │                  │  │
│  │  │  - Or soft-taints nodes            │                  │  │
│  │  └────────────────────────────────────┘                  │  │
│  │           │                                               │  │
│  │           │ (optional)                                    │  │
│  │           ▼                                               │  │
│  │  ┌────────────────────────────────────┐                  │  │
│  │  │  Soft Tainter Deployment           │                  │  │
│  │  │  - Taints underutilized nodes      │                  │  │
│  │  │  - Scheduler considers taints      │                  │  │
│  │  └────────────────────────────────────┘                  │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Cluster-wide Resources                                   │  │
│  │  - KubeDescheduler CR (singleton: "cluster")             │  │
│  │  - ClusterOperator/kube-descheduler (status reporting)   │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

## Components

### 1. Operator Controller (`target_config_reconciler.go`)

**Purpose**: Main reconciliation loop that watches the `KubeDescheduler` CR and ensures the descheduler deployment matches desired state.

**Key Functions** (verified in source code):
- Watches `KubeDescheduler` CR for changes
- Translates profiles into upstream descheduler policy configuration
- Creates/updates descheduler deployment, service account, RBAC
- Optionally deploys soft-tainter controller
- Updates ClusterOperator status

**Reconciliation Flow**:
1. Read `KubeDescheduler` CR (singleton named "cluster")
2. Validate configuration (profiles, mode, eviction limits)
3. Generate descheduler policy ConfigMap from profiles
4. Apply descheduler deployment with generated config
5. Apply soft-tainter deployment if KubeVirtRelieveAndMigrate profile enabled
6. Update ClusterOperator status (Available, Progressing, Degraded)

### 2. Descheduler Workload

**Purpose**: Runs the upstream Kubernetes descheduler with OpenShift-specific configuration.

**Key Characteristics**:
- Runs as a Deployment (1 replica) in `openshift-kube-descheduler-operator` namespace
- Configured via ConfigMap (generated from profiles)
- Runs periodically based on `deschedulingIntervalSeconds` (default: varies by profile)
- Two modes:
  - **Automatic**: Actually evicts pods
  - **Predictive** (default): Simulates eviction, logs what would happen

**Upstream Dependency**:
- Based on [kubernetes-sigs/descheduler](https://github.com/kubernetes-sigs/descheduler)
- Version tracked in `go.mod`

### 3. Soft Tainter Controller (`pkg/softtainter/`)

**Purpose**: Alternative to pod eviction - applies soft taints to nodes so scheduler prefers other nodes for new pods.

**When Deployed** (verified in reconciler code):
- Only when `KubeVirtRelieveAndMigrate` profile is enabled
- Runs as separate deployment

**How It Works**:
1. Monitors node utilization
2. Taints underutilized nodes with soft taints (`PreferNoSchedule`)
3. Scheduler sees taints and prefers non-tainted nodes
4. Existing pods remain running (no eviction)
5. Workloads gradually rebalance via natural pod lifecycle

**Benefits**:
- No pod disruption
- Gentler rebalancing
- Useful for stateful workloads

## Configuration Flow

### Profile → Strategy Translation

**Profile mappings** (from `pkg/apis/descheduler/v1/types_descheduler.go` search: "type DeschedulerProfile"):

| Profile | Upstream Strategies |
|---------|---------------------|
| **AffinityAndTaints** | RemovePodsViolatingInterPodAntiAffinity, RemovePodsViolatingNodeAffinity, RemovePodsViolatingNodeTaints |
| **TopologyAndDuplicates** | RemovePodsViolatingTopologySpreadConstraint, RemoveDuplicates |
| **SoftTopologyAndDuplicates** | RemovePodsViolatingTopologySpreadConstraint, RemoveDuplicates *(with ScheduleAnyway soft constraints)* |
| **LifecycleAndUtilization** | PodLifeTime, LowNodeUtilization |
| **EvictPodsWithLocalStorage** | *(enables evicting pods with emptyDir)* |
| **EvictPodsWithPVC** | *(enables evicting pods with PVCs)* |
| **LongLifecycle** | PodLifeTime (with specific thresholds) |
| **CompactAndScale** | HighNodeUtilization (compacts to fewer nodes) |
| **KubeVirtRelieveAndMigrate** | KubevirtMigrationAware (KubeVirt live migration support) |

**Deprecated Profiles:**
- `DevPreviewLongLifecycle` → auto-migrated to `LongLifecycle` (removed in 4.19+)
- `DevKubeVirtRelieveAndMigrate` → auto-migrated to `KubeVirtRelieveAndMigrate` (removed in 4.22+)

**Note**: The **soft-tainter** component is enabled when **KubeVirtRelieveAndMigrate** profile is active, not **SoftTopologyAndDuplicates**. The latter uses soft topology spread constraints (`ScheduleAnyway`), while the former deploys the soft-tainter deployment that applies `PreferNoSchedule` taints.

### Configuration Precedence

1. **Profiles** (user-specified in `KubeDescheduler` CR)
2. **ProfileCustomizations** (override profile defaults)
3. **Mode** (Automatic vs Predictive)
4. **EvictionLimits** (total and per-node caps)

## Data Flow

```
User modifies KubeDescheduler CR
          ↓
Operator detects change (informer)
          ↓
Operator validates config
          ↓
Operator generates descheduler policy ConfigMap
          ↓
Operator applies Deployment with new config
          ↓
Descheduler pod restarts (if config changed)
          ↓
Descheduler runs strategies every N seconds
          ↓
Descheduler evaluates pods and nodes
          ↓
[Automatic mode] → Evicts pods
[Predictive mode] → Logs what would be evicted
[Soft tainting] → Soft Tainter taints nodes
```

## Dependencies

### Internal Dependencies (OpenShift)

- **library-go**: Controller factory, resourceapply helpers, operator framework
- **openshift/api**: CRD definitions, operatorv1.OperatorSpec
- **openshift/client-go**: Generated clientsets for OpenShift APIs

### External Dependencies

- **kubernetes-sigs/descheduler**: Upstream descheduler API types and policy structures
- **kubernetes client-go**: Kubernetes API clients and informers
- **controller-runtime**: Only for CRD generation (`controller-gen`)

### API Dependencies

The operator interacts with:
- **KubeDescheduler** CR (`operator.openshift.io/v1`)
- **Deployments** (apps/v1)
- **ConfigMaps** (v1)
- **ServiceAccounts**, **ClusterRoles**, **ClusterRoleBindings** (RBAC)
- **ServiceMonitor** (monitoring.coreos.com/v1) - for Prometheus metrics
- **ClusterOperator** (config.openshift.io/v1) - status reporting

## Key Design Decisions

### Decision 1: Profiles Over Raw Strategy Configuration

**Rationale**: 
- Upstream descheduler strategy configuration is complex and error-prone
- Profiles provide curated, tested combinations of strategies
- Easier for users to understand and maintain

**Tradeoff**: 
- Less flexibility than raw strategy configuration
- Must add new profiles for new use cases

**Alternative Considered**: Allow raw strategy passthrough via CR  
**Rejected Because**: Too risky - users could misconfigure and destabilize clusters

### Decision 2: Singleton KubeDescheduler CR

**Rationale**:
- Only one descheduler per cluster makes sense (one reconciliation loop)
- Prevents conflicting descheduler instances with different policies
- Follows pattern of other cluster-scoped operators

**Implementation**: CRD validation enforces `.metadata.name == "cluster"`

### Decision 3: Soft Tainting as Separate Deployment

**Rationale**:
- Fundamentally different mechanism than eviction
- Requires separate controller (soft-tainter)
- Cannot mix soft tainting with hard eviction in same run

**Implementation**: KubeVirtRelieveAndMigrate profile triggers soft-tainter deployment

### Decision 4: Predictive Mode as Default

**Rationale**:
- Safer default - no actual pod disruption
- Allows users to test descheduler policies before enabling eviction
- Logs show what would happen in Automatic mode

**Tradeoff**: Users must explicitly enable Automatic mode to get actual descheduling

**Alternative Considered**: Default to Automatic  
**Rejected Because**: Too risky for production clusters

### Decision 5: Operator Manages Descheduler Lifecycle

**Rationale**:
- Cluster admin shouldn't manually deploy descheduler
- Operator ensures correct RBAC, configuration, upgrades
- Integrates with OpenShift monitoring and status reporting

**Tradeoff**: Cannot run descheduler independently of operator

## Upgrade and Compatibility

### Upgrade Strategy

- **In-place upgrades**: Operator updates descheduler deployment image
- **Config migration**: Operator handles deprecated profile migration
  - `DevPreviewLongLifecycle` → `LongLifecycle` (auto-migrated)
  - `DevKubeVirtRelieveAndMigrate` → `KubeVirtRelieveAndMigrate` (auto-migrated)

### API Compatibility

- **CRD versioning**: Currently v1 (stable)
- **Deprecated fields**: Marked with `+kubebuilder:deprecatedversion:warning`
  - `devEnableSoftTainter` (deprecated - use SoftTopologyAndDuplicates or KubeVirtRelieveAndMigrate profile)

### OpenShift Version Compatibility

See [README.md](./README.md#releases) for the full version compatibility matrix.

## Security Considerations

### RBAC

**RBAC requirements** (from `deploy/02_clusterrole.yaml`):

The descheduler requires broad cluster permissions:
- **List/Get**: All pods, nodes, replicasets, deployments, jobs, etc.
- **Delete**: Pods (for eviction)
- **Create**: Pod evictions

**Mitigation**: 
- Runs as dedicated service account with minimal required permissions
- Cannot escalate privileges or modify other resources

### Pod Eviction Safeguards

1. **Eviction Limits**: `evictionLimits.total` and `evictionLimits.node` cap evictions
2. **PodDisruptionBudgets**: Descheduler respects PDBs (won't violate min available)
3. **Predictive Mode**: Default mode simulates without actual eviction
4. **Namespace Exclusions**: System namespaces excluded by default

### Soft Tainting Security

- Soft-tainter uses ValidatingAdmissionPolicy to prevent unauthorized taint removal
- Only soft-tainter service account can manage soft taints

## Performance Characteristics

### Resource Usage

**Typical resource usage:**

- **Operator**: Low (watches CR, applies manifests)
  - CPU: ~10m
  - Memory: ~50Mi

- **Descheduler**: Varies by cluster size
  - CPU: ~100m - 500m (during runs)
  - Memory: ~100Mi - 500Mi
  - Runs periodically, not continuously

### Scalability

**Tested configurations:**

- **Cluster Size**: Tested up to 250 nodes, 10K pods
- **Descheduling Interval**: Adjust `deschedulingIntervalSeconds` based on cluster size
  - Small clusters (< 50 nodes): 60s
  - Medium clusters (50-150 nodes): 300s (5 min)
  - Large clusters (150+ nodes): 600s (10 min)

## Observability

### Metrics

**Exposed via ServiceMonitor (Prometheus) - verified on live cluster:**

- `descheduler_build_info` - Descheduler version
- `descheduler_pods_evicted_total` - Total pods evicted by strategy

### Logging

- **Operator logs**: Reconciliation events, errors
- **Descheduler logs**: Strategy evaluation, pod eviction decisions
- **Soft-tainter logs**: Node tainting decisions

### Status Reporting

**ClusterOperator `kube-descheduler` reports:**

- **Available**: Descheduler deployment running
- **Progressing**: Rollout in progress
- **Degraded**: Deployment failed or CrashLoopBackOff

## Testing Strategy

### Test Architecture

The operator uses a multi-layered testing approach:

**Unit Tests**
- Test controller sync logic in isolation
- Validate profile-to-strategy translation
- Verify configuration validation rules
- Mock external dependencies

**E2E Tests** 
- Deployed via OpenShift Tests Extension (OTE) framework
- Verify end-to-end descheduling workflows
- Test both Automatic and Predictive modes
- Validate metrics collection

**CI Integration**
- OpenShift CI (Prow) runs tests on PRs
- Topology testing (SNO, Two-Node, MicroShift)

For detailed testing instructions, test writing guidelines, and debugging tips, see **[TESTING.md](./TESTING.md)**. For contribution testing requirements, see [CONTRIBUTING.md](./CONTRIBUTING.md#testing-guidelines).

## Known Limitations

**Known limitations:**

- Cannot run multiple descheduler instances per cluster (singleton CR enforced)
- Profile configurations are opaque (users can't easily see generated strategy config)
- Soft tainting requires ValidatingAdmissionPolicy (Kubernetes 1.26+)
- Descheduler cannot evict pods that are not backed by a controller (bare pods)

## Additional Documentation

- **[README.md](./README.md)** - User documentation and descheduler profile descriptions
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** - Contribution guidelines and development guide
- **[TESTING.md](./TESTING.md)** - Comprehensive testing guide
- **[AGENTS.md](./AGENTS.md)** - AI agent quick reference with verified commands

## References

- **Upstream Descheduler**: https://github.com/kubernetes-sigs/descheduler
- **OpenShift Docs**: https://docs.openshift.com/container-platform/latest/nodes/scheduling/nodes-descheduler.html
- **Library-go**: https://github.com/openshift/library-go
- **Source Code**: https://github.com/openshift/cluster-kube-descheduler-operator
