# Kube Descheduler Operator

Run the descheduler in your OpenShift cluster to move pods based on specific strategies.

## Deploy the operator

### Quick Development

1. Build and push the operator image to a registry:
2. Ensure the `image` spec in `deploy/05_deployment.yaml` refers to the operator image you pushed
3. Run `oc create -f deploy/.`

### OperatorHub install with custom index image

This process refers to building the operator in a way that it can be installed locally via the OperatorHub with a custom index image

1. build and push the image to a registry (e.g. https://quay.io):
   ```sh
   $ podman build -t quay.io/<username>/ose-cluster-kube-descheduler-operator-bundle:latest -f Dockerfile .
   $ podman push quay.io/<username>/ose-cluster-kube-descheduler-operator-bundle:latest
   ```

1. build and push image index for operator-registry (pull and build https://github.com/operator-framework/operator-registry/ to get the `opm` binary)
   ```sh
   $ ./bin/linux-amd64-opm index add --bundles quay.io/<username>/ose-cluster-kube-descheduler-operator-bundle:latest --tag quay.io/<username>/ose-cluster-kube-descheduler-operator-bundle-index:1.0.0
   $ podman push quay.io/<username>/ose-cluster-kube-descheduler-operator-bundle-index:1.0.0
   ```

   Don't forget to increase the number of open files, .e.g. `ulimit -n 100000` in case the current limit is insufficient.

1. create and apply catalogsource manifest:
   ```yaml
   apiVersion: operators.coreos.com/v1alpha1
   kind: CatalogSource
   metadata:
     name: cluster-kube-descheduler-operator
     namespace: openshift-marketplace
   spec:
     sourceType: grpc
     image: quay.io/<username>/ose-cluster-kube-descheduler-operator-bundle-index:1.0.0
   ```

1. create `cluster-kube-descheduler-operator` namespace:
   ```
   $ oc create ns cluster-kube-descheduler-operator
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
* [`DoNotEvictPodsWithPVC`](#DoNotEvictPodsWithPVC)
* [`EvictPodsWithLocalStorage`](#EvictPodsWithLocalStorage)
  
Along with the following profiles, which are in development and may change:
* [`DevPreviewLongLifecycle`](#DevPreviewLongLifecycle)

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

### DevPreviewLongLifecycle
This profile provides cluster resource balancing similar to [LifecycleAndUtilization](#LifecycleAndUtilization) for longer-running 
clusters. It does not evict pods based on the 24 hour lifetime used by LifecycleAndUtilization.

### DoNotEvictPodsWithPVC
This profile is intended to be used in combination with any of the above profiles to prevent
them from evicting pods that have PVCs. Without this profile, these pods are eligible
to be evicted by any profile.

### EvictPodsWithLocalStorage
By default, pods with local storage are not eligible to be considered for eviction by any
profile. Using this profile allows them to be evicted if necessary.

## Profile Customizations
Some profiles expose options which may be used to configure the underlying Descheduler strategy parameters. These are available under
the `profileCustomizations` field:

|Name|Type|Description|
|---|---|---|
|`podLifetime`|`time.Duration`|Sets the lifetime value for pods evicted by the `LifecycleAndUtilization` profile|

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

* `deschedulingIntervalSeconds` - this sets the number of seconds between descheduler runs
* `profiles` - which descheduler profiles that are enabled
