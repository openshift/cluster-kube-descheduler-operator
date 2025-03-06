# Kube Descheduler Operator

Run the descheduler in your OpenShift cluster to move pods based on specific strategies.

## Releases

| kdo version | ocp version | k8s version | golang |
| ----------- | ----------- | ----------- | ------ |
| 5.0.0       | 4.15, 4.16  | 1.28        | 1.20   |
| 5.0.1       | 4.15, 4.16  | 1.29        | 1.21   |
| 5.0.2       | 4.15, 4.16  | 1.29        | 1.21   |
| 5.1.0       | 4.17, 4.18  | 1.30        | 1.22   |
| 5.1.1       | 4.17, 4.18  | 1.31        | 1.22   |

## Deploy the operator

### Quick Development

1. Build and push the operator image to a registry:
2. Ensure the `image` spec in `deploy/05_deployment.yaml` refers to the operator image you pushed
3. Run `oc create -f deploy/.`

### OperatorHub install with custom index image

This process refers to building the operator in a way that it can be installed locally via the OperatorHub with a custom index image

1. Build and push the operator image to a registry:
   ```sh
   export QUAY_USER=${your_quay_user_id}
   export IMAGE_TAG=${your_image_tag}
   podman build -t quay.io/${QUAY_USER}/cluster-kube-descheduler-operator:${IMAGE_TAG} -f Dockerfile.rhel7
   podman login quay.io -u ${QUAY_USER}
   podman push quay.io/${QUAY_USER}/cluster-kube-descheduler-operator:${IMAGE_TAG}
   ```

1. Update the `.spec.install.spec.deployments[0].spec.template.spec.containers[0].image` field in the KDO CSV under `./manifests/cluster-kube-descheduler-operator.clusterserviceversion.yaml` to point to the newly built image.

1. build and push the metadata image to a registry (e.g. https://quay.io):
   ```sh
   podman build -t quay.io/${QUAY_USER}/cluster-kube-descheduler-operator-metadata:${IMAGE_TAG} -f Dockerfile.metadata .
   podman push quay.io/${QUAY_USER}/cluster-kube-descheduler-operator-metadata:${IMAGE_TAG}
   ```

1. build and push image index for operator-registry (pull and build https://github.com/operator-framework/operator-registry/ to get the `opm` binary)
   ```sh
   opm index add --bundles quay.io/${QUAY_USER}/cluster-kube-descheduler-operator-metadata:${IMAGE_TAG} --tag quay.io/${QUAY_USER}/cluster-kube-descheduler-operator-index:${IMAGE_TAG}
   podman push quay.io/${QUAY_USER}/cluster-kube-descheduler-operator-index:${IMAGE_TAG}
   ```

   Don't forget to increase the number of open files, .e.g. `ulimit -n 100000` in case the current limit is insufficient.

1. create and apply catalogsource manifest (remember to change <<QUAY_USER>> and <<IMAGE_TAG>> to your own values):
   ```yaml
   apiVersion: operators.coreos.com/v1alpha1
   kind: CatalogSource
   metadata:
     name: cluster-kube-descheduler-operator
     namespace: openshift-marketplace
   spec:
     sourceType: grpc
     image: quay.io/<<QUAY_USER>>/cluster-kube-descheduler-operator-index:<<IMAGE_TAG>>
   ```

1. create `openshift-kube-descheduler-operator` namespace:
   ```
   $ oc create ns openshift-kube-descheduler-operator
   ```

1. open the console Operators -> OperatorHub, search for `descheduler operator` and install the operator


## Sample CR

A sample CR definition looks like below (the operator expects `cluster` CR under `openshift-kube-descheduler-operator` namespace):

```yaml
apiVersion: operator.openshift.io/v1
kind: KubeDescheduler
metadata:
  name: cluster
  namespace: openshift-kube-descheduler-operator
spec:
  deschedulingIntervalSeconds: 1800
  profiles:
  - AffinityAndTaints
  - LifecycleAndUtilization
  profileCustomizations:
    podLifetime: 5m
    namespaces:
      included:
      - ns1
      - ns2
```

The operator spec provides a `profiles` field, which allows users to set one or more descheduling profiles to enable.

These profiles map to preconfigured policy definitions, enabling several descheduler strategies grouped by intent, and
any that are enabled will be merged.

## Profiles

The following profiles are currently provided:
* [`AffinityAndTaints`](#AffinityAndTaints)
* [`TopologyAndDuplicates`](#TopologyAndDuplicates)
* [`SoftTopologyAndDuplicates`](#SoftTopologyAndDuplicates)
* [`LifecycleAndUtilization`](#LifecycleAndUtilization)
* [`LongLifecycle`](#LongLifecycle)
* [`CompactAndScale`](#compactandscale-techpreview)
* [`EvictPodsWithPVC`](#EvictPodsWithPVC)
* [`EvictPodsWithLocalStorage`](#EvictPodsWithLocalStorage)

Each of these enables cluster-wide descheduling (excluding openshift and kube-system namespaces) based on certain goals.

### AffinityAndTaints
This is the most basic descheduling profile and it removes running pods which violate node and pod affinity, and node
taints.

This profile enables the [`RemovePodsViolatingInterPodAntiAffinity`](https://github.com/kubernetes-sigs/descheduler/#removepodsviolatinginterpodantiaffinity),
[`RemovePodsViolatingNodeAffinity`](https://github.com/kubernetes-sigs/descheduler/#removepodsviolatingnodeaffinity), and
[`RemovePodsViolatingNodeTaints`](https://github.com/kubernetes-sigs/descheduler/#removepodsviolatingnodeaffinity) strategies.

### TopologyAndDuplicates
This profile attempts to balance pod distribution based on topology constraint definitions and evicting duplicate copies
of the same pod running on the same node. It enables the [`RemovePodsViolatingTopologySpreadConstraints`](https://github.com/kubernetes-sigs/descheduler/#removepodsviolatingtopologyspreadconstraint)
and [`RemoveDuplicates`](https://github.com/kubernetes-sigs/descheduler/#removeduplicates) strategies.

### SoftTopologyAndDuplicates
This profile is the same as `TopologyAndDuplicates`, however it will also consider pods with "soft" topology constraints
for eviction (ie, `whenUnsatisfiable: ScheduleAnyway`)

### LifecycleAndUtilization
This profile focuses on pod lifecycles and node resource consumption. It will evict any running pod older than 24 hours
and attempts to evict pods from "high utilization" nodes that can fit onto "low utilization" nodes. A high utilization
node is any node consuming more than 50% its available cpu, memory, *or* pod capacity. A low utilization node is any node
with less than 20% of its available cpu, memory, *and* pod capacity.

This profile enables the [`LowNodeUtilizaition`](https://github.com/kubernetes-sigs/descheduler/#lownodeutilization),
[`RemovePodsHavingTooManyRestarts`](https://github.com/kubernetes-sigs/descheduler/#removepodshavingtoomanyrestarts) and
[`PodLifeTime`](https://github.com/kubernetes-sigs/descheduler/#podlifetime) strategies. In the future, more configuration
may be made available through the operator for these strategies based on user feedback.

### LongLifecycle
This profile provides cluster resource balancing similar to [LifecycleAndUtilization](#LifecycleAndUtilization) for longer-running
clusters. It does not evict pods based on the 24 hour lifetime used by LifecycleAndUtilization.

### CompactAndScale
This profile seeks to evict pods to enable the same workload to run on a smaller set of nodes.
It will attempts to evict pods from "under utilized" nodes that can fit into fewer nodes.
An under utilized node is any node consuming less than 20% of its available cpu, memory, *and* pod capacity.

This profile enables the [`HighNodeUtilization`](https://github.com/kubernetes-sigs/descheduler/#highnodeutilization) strategy.
In the future, more configuration may be made available through the operator based on user feedback.

### EvictPodsWithPVC
By default, the operator prevents pods with PVCs from being evicted. Enabling this
profile in combination with any of the above profiles allows pods with PVCs to be
eligible for eviction.

### EvictPodsWithLocalStorage
By default, pods with local storage are not eligible to be considered for eviction by any
profile. Using this profile allows them to be evicted if necessary. A pod is defined as using
local storage if any of its volumes have `HostPath` or `EmptyDir` set (note that a pod that only
uses PVCs does not fit this definition, and will need the `EvictPodsWithPVC` profile instead. Pods
that use both will need both profiles to be evicted).

## Profile Customizations
Some profiles expose options which may be used to configure the underlying Descheduler strategy parameters. These are available under
the `profileCustomizations` field:

|Name|Type|Description|
|---|---|---|
|`podLifetime`|`time.Duration`|Sets the lifetime value for pods evicted by the `LifecycleAndUtilization` profile|
|`thresholdPriorityClassName`|`string`|Sets the priority class threshold by name for all strategies|
|`thresholdPriority`|`string`|Sets the priority class threshold by value for all strategies|
|`namespaces.included`, `namespaces.excluded`|`[]string`| Sets the included/excluded namespaces for all strategies (included namespaces are not allowed to include protected namespaces which consist of `kube-system`, `hypershift` and all `openshift-` prefixed namespaces)|
| `devLowNodeUtilizationThresholds` | `string` | Sets experimental thresholds for the [LowNodeUtilization](https://github.com/kubernetes-sigs/descheduler#lownodeutilization) strategy of the `LifecycleAndUtilization` profile in the following ratios: `Low` for 10%:30%, `Medium` for 20%:50%, `High` for 40%:70%|
|`devEnableEvictionsInBackground`|`bool`| Enables descheduler's EvictionsInBackground alpha feature. The EvictionsInBackground alpha feature is a subject to change. Currently provided as an experimental feature.|
| `devHighNodeUtilizationThresholds` | `string` | Sets thresholds for the [HighNodeUtilization](https://github.com/kubernetes-sigs/descheduler#highnodeutilization) strategy of the `CompactAndScale` profile in the following ratios: `Minimal` for 10%, `Modest` for 20%, `Moderate` for 30%. Currently provided as an experimental feature.|
|`devActualUtilizationProfile`|`string`| Sets a profile that gets translated into a predefined prometheus query |

## Prometheus query profiles
The operator provides the following profiles:
- `PrometheusCPUUsage`: `instance:node_cpu:rate:sum` (metric available in OpenShift by default)
- `PrometheusCPUPSIPressure`: `rate(node_pressure_cpu_waiting_seconds_total[1m])` (`node_pressure_cpu_waiting_seconds_total` is reported in OpenShift only for nodes configured with psi=1 kernel argument)
- `PrometheusCPUPSIPressureByUtilization`: `avg by (instance) ( rate(node_pressure_cpu_waiting_seconds_total[1m])) and (1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[1m]))) > 0.7 or avg by (instance) ( rate(node_pressure_cpu_waiting_seconds_total[1m])) * 0` (`node_pressure_cpu_waiting_seconds_total` is reported in OpenShift only for nodes configured with psi=1 kernel argument; the query is filtering out PSI pressure on nodes with average CPU utilization < 0.7 to filter out false positives pressure spikes due to self imposed CPU throttling)
- `PrometheusMemoryPSIPressure`: `rate(node_pressure_memory_waiting_seconds_total[1m])` (`node_pressure_memory_waiting_seconds_total` is reported in OpenShift only for nodes configured with psi=1 kernel argument)
- `PrometheusIOPSIPressure`: `rate(node_pressure_io_waiting_seconds_total[1m])` (`node_pressure_memory_waiting_seconds_total` is reported in OpenShift only for nodes configured with psi=1 kernel argument)

```yaml
apiVersion: operator.openshift.io/v1
kind: KubeDescheduler
metadata:
  name: cluster
  namespace: openshift-kube-descheduler-operator
spec:
  managementState: Managed
  deschedulingIntervalSeconds: 3600
  profiles:
  - LongLifecycle
  profileCustomizations:
    devActualUtilizationProfile: PrometheusCPUUsage
```

## Descheduling modes
The operator provides two modes of eviction:
- `Predictive`: configures the descheduler to only simulate eviction
- `Automatic`: configures the descheduler to evict pods

The predictive mode is the default mode.
The descheduler in either of the modes still produces metrics (unless the metrics are disabled).
When the predictive mode is configured, the reported metrics can serve as an estimation
of evicted pods in the cluster.


## How does the descheduler operator work?

Descheduler operator at a high level is responsible for watching the above CR
- Create a configmap that could be used by descheduler.
- Run descheduler as a deployment mounting the configmap as a policy file in the pod.

The configmap created from above sample CR definition looks like this:

```yaml
apiVersion: descheduler/v1alpha1
    kind: DeschedulerPolicy
    strategies:
      RemovePodsViolatingInterPodAntiAffinity:
        enabled: true
        ...
      RemovePodsViolatingNodeAffinity:
        enabled: true
        params:
          ...
          nodeAffinityType:
          - requiredDuringSchedulingIgnoredDuringExecution
      RemovePodsViolatingNodeTaints:
        enabled: true
        ...
```
(Some generated parameters omitted.)


## Parameters
The Descheduler operator exposes the following parameters in its CRD:

|Name|Type|Description|
|---|---|---|
|`deschedulingIntervalSeconds`|`int32`|Sets the number of seconds between descheduler runs|
|`profiles`|`[]string`|Sets which descheduler strategy profiles are enabled|
|`profileCustomizations`|`map`|Contains various parameters for modifying the default behavior of certain profiles|
|`mode`|`string`|Configures the descheduler to either evict pods or to simulate the eviction|
|`evictionLimits`|`map`|Restrict the number of evictions during each descheduling run. Available fields are: `total`|
|`evictionLimits.total`|`int32`|Restricts the maximum number of overall evictions|
