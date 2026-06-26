# Kube Descheduler Operator

## RBAC Configuration

RBAC manifests: `bindata/assets/kube-descheduler/`

### Descheduler Operand RBAC

ServiceAccount: `openshift-kube-descheduler-operator/openshift-descheduler-operand`

Permissions:
- ClusterRole: `openshift-descheduler-operand` + CR binding
  - Most of the resources have read only permissions to observe cluster-scope resources so the descheduler can evaluate cluster state for eviction decisions. E.g. `pods`, `nodes`. The list may change over time.
  - `pods/eviction`: create
  - `events.k8s.io/events`: get, watch, list, create, update, patch, delete
- Role: `openshift-kube-descheduler-operator/openshift-descheduler-operand` + RoleBinding (namespace-scoped)
  - `coordination.k8s.io/leases`: create, get, patch, delete
  - Leader election leases scoped to operator namespace only
- Extra ClusterRoleBindings:
  - `cluster-monitoring-view` ClusterRole to allow Prometheus Queries when evaluating metrics usage for KubeVirt
- Role: `openshift-kube-descheduler-operator/prometheus-k8s`
  - Read only for `services`, `endpoints` and `pods`. Allows `openshift-monitoring/prometheus-k8s` SA to scrape metrics (pattern used across OpenShift)

---

### Soft Tainter Controller RBAC

ServiceAccount: `openshift-kube-descheduler-operator/openshift-descheduler-softtainter`

Permissions:
- ClusterRole: `openshift-descheduler-softtainter` + CR binding
  - Read only: `operator.openshift.io/kubedeschedulers`
  - `nodes`: get, watch, list, patch, update (patch/update operations are validated by `openshift-descheduler-softtainter-vap` ValidatingAdmissionPolicy which checks SA and restricts modifications to descheduler-specific taints only; correct functionality validated by `test/e2e` testSoftTainterControllerWithVAP test)
  - `events.k8s.io/events`: get, watch, list, create, update, patch, delete
- Role: `openshift-kube-descheduler-operator/openshift-descheduler-softtainter` + RoleBinding (namespace-scoped)
  - `coordination.k8s.io/leases`: create, get, patch, update, delete
  - Leader election leases scoped to operator namespace only
- Extra ClusterRoleBindings:
  - `cluster-monitoring-view` ClusterRole to allow querying Prometheus metrics

---

### Operator RBAC

ServiceAccount: `openshift-kube-descheduler-operator/openshift-descheduler`

The operator RBAC permissions need to include permissions of both the descheduler operand and the soft-tainter controller (the operator needs to be granted permissions it grants). In addition, the operator has the following extra permissions to manage the operands:

Permissions (extra beyond operands):
- ClusterRole: `openshift-descheduler` + CR binding
  - Read only: `config.openshift.io/schedulers`, `config.openshift.io/infrastructures`, `config.openshift.io/apiservers`, `route.openshift.io/routes`, `endpoints`, `apps/replicasets`
  - `operator.openshift.io/kubedeschedulers`, `operator.openshift.io/kubedeschedulers/status`: get, watch, list, create, update, patch, delete, deletecollection
  - `monitoring.coreos.com/servicemonitors`, `monitoring.coreos.com/prometheusrules`: get, watch, list, create, update, patch, delete, deletecollection
  - `monitoring.coreos.com/prometheuses/api` (resourceName: `k8s`): get, create, update
  - `rbac.authorization.k8s.io/clusterroles`: create (unrestricted); get, watch, list, update, patch, delete (resourceNames: `openshift-descheduler-operand`, `openshift-descheduler-softtainter`)
  - `rbac.authorization.k8s.io/clusterrolebindings`: create (unrestricted); get, watch, list, update, patch, delete (resourceNames: `openshift-descheduler-operand`, `openshift-descheduler-softtainter`, `cluster-monitoring-view-cr`, `openshift-descheduler-softtainter-monitoring`)
  - `rbac.authorization.k8s.io/roles`: create (unrestricted); get, watch, list, update, patch, delete (resourceNames: `openshift-descheduler-operand`, `openshift-descheduler-softtainter`, `prometheus-k8s`)
  - `rbac.authorization.k8s.io/rolebindings`: create (unrestricted); get, watch, list, update, patch, delete (resourceNames: `openshift-descheduler-operand`, `openshift-descheduler-softtainter`, `prometheus-k8s`)
  - `admissionregistration.k8s.io/validatingadmissionpolicies`: create (unrestricted); get, watch, list, update, patch, delete (resourceNames: `openshift-descheduler-softtainter-vap`)
  - `admissionregistration.k8s.io/validatingadmissionpolicybindings`: create (unrestricted); get, watch, list, update, patch, delete (resourceNames: `openshift-descheduler-softtainter-vap-binding`)

- Role: `openshift-descheduler-operator/descheduler-operator` + RoleBinding (namespace-scoped)
  - Core API resources (`services`, `configmaps`, `secrets`, `events`, `serviceaccounts`): get, watch, list, create, update, patch, delete, deletecollection
  - `apps/deployments`, `apps/deployments/scale`: get, watch, list, create, update, patch, delete, deletecollection
  - `coordination.k8s.io/leases`: get, watch, list, create, update, patch, delete
  - These permissions are scoped to `openshift-kube-descheduler-operator` namespace only
