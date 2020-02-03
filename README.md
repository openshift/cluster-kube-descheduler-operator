# descheduler-operator
An operator to run descheduler on OpenShift

To deploy the operator:

```
oc create -f deploy/ns.yaml
oc create -f deploy/crd.yaml
oc create -f deploy/role.yaml
oc create -f deploy/role_binding.yaml
oc create -f deploy/operator.yaml
oc create -f deploy/cr.yaml
```

Replace `oc` with `kubectl` in case you want descheduler to run with kubernetes. All the required components are created in `openshift-descheduler-operator` namespace.

## Sample CR

A sample CR definition looks like below (the operator expects `config` CR under `openshift-kube-descheduler-operator` namespace):

```yaml
apiVersion: operator.openshift.io/v1beta1
kind: KubeDescheduler
metadata:
  name: config
  namespace: openshift-kube-descheduler-operator
spec:
  schedule: "*/1 * * * ?"
  strategies:
    - name: "lownodeutilization"
      params:
       - name: "cputhreshold"
         value: "10"
       - name: "memorythreshold"
         value: "20"
       - name: "podsthreshold"
         value: "30"
       - name: "memorytargetthreshold"
         value: "40"
       - name: "cputargetthreshold"
         value: "50"
       - name: "podstargetthreshold"
         value: "60"
       - name: "nodes"
         value: "3"
    - name: "duplicates"
```
The valid list of strategies are "lownodeutilization", "duplicates", "interpodantiaffinity", "nodeaffinity". Out of the above only lownodeutilization has parameters like cputhreshold, memorythreshold etc. Using the above strategies defined in CR we create a configmap in openshift-descheduler-operator namespace. As of now, adding new strategies could be done through code. Schedule field contains schedule of the cron job which would run the descheduler as a pod. Nodes field indicate on how many nodes the lownodeutilization strategy should run.

## How does the descheduler operator work?

Descheduler operator at a high level is responsible for watching the above CR
- Create a configmap that could be used by descheduler.
- Run descheduler as a cron job after creating configmap.

The configmap created from above sample CR definition looks like this:

```yaml
apiVersion: "kubedeschedulers.operator.openshift.io/v1beta1"
kind: "DeschedulerPolicy"
strategies:
  "RemoveDuplicates":
     enabled: true
  "LowNodeUtilization":
     enabled: true
     params:
       nodeResourceUtilizationThresholds:
         thresholds:
           "cpu" : 10
           "memory": 20
           "pods": 30
         targetThresholds:
           "cpu" : 40
           "memory": 50
           "pods": 60
         numberOfNodes: 3
```

The above configmap would be mounted as a volume in descheduler cron job pod created. Whenever we change strategies, parameters or schedule in the CR, the descheduler operator is responsible for identifying those changes and regenerating the configmap. For more information on how descheduler works, please visit [descheduler](https://docs.openshift.com/container-platform/3.11/admin_guide/scheduling/descheduler.html)
