# Architecture

The cluster-kube-descheduler-operator deploys and manages the descheduler operand, which evicts pods based on configurable strategies. The operator is optional and managed by OLM.

The operator is built on top of [`github.com/openshift/library-go/pkg/controller/controllercmd.NewControllerCommandConfig`](https://github.com/openshift/library-go/blob/c6cd1a243d2d6780ea8515468a2649fe97bfdeae/pkg/controller/controllercmd/cmd.go#L47), which provides a standardized API for the operator command with known flags and reconciliation loop.

## Profiles

A profile is an abstraction over specific descheduler plugins. Instead of focusing on which plugin to run, a profile focuses on the user's intention for pod eviction. Multiple profiles can be enabled simultaneously, with cumulative effects on the cluster.

Profiles are defined in `pkg/apis/descheduler/v1/types_descheduler.go` as a `DeschedulerProfile` enum type. The operator translates the enabled profiles into a descheduler policy configuration that specifies which plugins to run and their parameters.

Profiles can be customized through the `ProfileCustomizations` field. New customization options can be added if properly justified by user requirements. Additionally, `EvictPodsWithLocalStorage` and `EvictPodsWithPVC` are defined as profiles but act as customizations that tune the behavior of other profiles by controlling eviction eligibility.

**Important:** The descheduler is configured to protect certain namespaces from pod eviction: `openshift`, `kube-system`, `hypershift`, and all namespaces with the `openshift-` prefix.

### Available Profiles

- **AffinityAndTaints** - Balance pods based on affinity and node taint violations
- **TopologyAndDuplicates** - Spread pods evenly among nodes based on topology spread constraints and duplicate replicas
- **SoftTopologyAndDuplicates** - Similar to TopologyAndDuplicates, but includes soft ("ScheduleAnyway") topology spread constraints
- **LifecycleAndUtilization** - Balance pods based on node resource usage, pod age, and pod restarts
- **LongLifecycle** - Handle cluster lifecycle over a long term
- **CompactAndScale** - Evict pods to enable the same workload to run on a smaller set of nodes
- **KubeVirtRelieveAndMigrate** - Evict pods from high-cost nodes to relieve overall expenses while considering workload migration

**Important:** The `KubeVirtRelieveAndMigrate` profile is currently used exclusively for [CNV (Container-native virtualization)](https://www.redhat.com/en/topics/containers/what-is-container-native-virtualization) use cases and is not intended for ordinary workloads.
