# descheduler-operator
An operator to run descheduler on OpenShift

To deploy the operator:

```
oc project kube-system 
oc create -f deploy/crd.yaml
oc create -f deploy/rbac.yaml
kubectl run nginx --image=nginx --replicas=5 #Testing for validating that some pods do get evicts for duplicate pod strategy.
oc create -f deploy/operator.yaml
oc create -f deploy/cr.yaml
```

To uninstall the operator:
```
oc project kube-system
oc delete -f deploy/cr.yaml
oc delete -f deploy/operator.yaml
oc delete -f deploy/rbac.yaml
oc delete -f deploy/crd.yaml
oc delete job descheduler-job
oc delete configmap descheduler-policy-configmap
```
