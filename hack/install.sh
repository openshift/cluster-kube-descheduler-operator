oc project kube-system
oc create -f deploy/crd.yaml
oc create -f deploy/rbac.yaml
kubectl run nginx --image=nginx --replicas=5 #Testing for validating that some pods do get evicts for duplicate pod strategy.
# 
sleep 10
oc create -f deploy/operator.yaml
oc create -f deploy/cr.yaml
