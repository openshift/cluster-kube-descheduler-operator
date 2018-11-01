# descheduler-operator
An operator to run descheduler on OpenShift

To deploy the operator:

```
oc create -f deploy/ns.yaml
oc create -f deploy/crd.yaml
oc create -f deploy/rbac.yaml
oc create -f deploy/cr
oc create -f ../deploy/olm-catalog/csv.yaml
oc create -f ../deploy/cr.yaml
```

Replace `oc` with `kubectl` in case you want descheduler to run with kubernetes. All the required components are created in `openshift-descheduler-operator` namespace. 

