# descheduler-operator
An operator to run descheduler on OpenShift

To deploy the operator:

```
oc create -f deploy/ns.yaml
oc create -f deploy/crd.yaml
oc create -f deploy/rbac.yaml
oc create -f deploy/operator.yaml
oc create -f deploy/cr.yaml
```

Replace `oc` with `kubectl` in case you want descheduler to run with kubernetes. All the required components are created in `openshift-descheduler-operator` namespace. 

## Sample CR

A sample CR definition looks like below:

```yaml
apiVersion: descheduler.io/v1alpha1
kind: Descheduler
metadata:
  name: example-descheduler-1
spec:
  # Add fields here
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
    - name: "duplicates"
```
The valid list of strategies are "lownodeutilization", "duplicates", "interpodantiaffinity", "nodeaffinity". Out of the above only lownodeutilization has parameters like cputhreshold, memorythreshold etc. Using the above strategies defined in CR we create a configmap in openshift-descheduler-operator namespace. As of now, adding new strategies could be done through code.

## How does the descheduler operator work?

Descheduler operator at a high level is responsible for watching the above CR 
- Create a configmap that could be used by descheduler.
- Run descheduler as a job after creating configmap.

The configmap created from above sample CR definition looks like this:

```yaml
apiVersion: "descheduler/v1alpha1"
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
```

The above configmap would be mounted as a volume in descheduler job pod created in next step. Whenever we change strategies or parameters in the CR, the descheduler operator is responsible for identifying those changes and regenerating the configmap.
