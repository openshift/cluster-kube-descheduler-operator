# Kube Descheduler Operator

## RBAC Configuration

RBAC manifests: `bindata/assets/kube-descheduler/`

### Descheduler Operand RBAC

ServiceAccount: `openshift-kube-descheduler-operator/openshift-descheduler-operand`

Permissions:
- ClusterRole: `openshift-descheduler-operand` + CR binding
  - Most of the resources have read only permissions to observe cluster-scope resources so the descheduler can evaluate cluster state for eviction decisions.*. E.g. `pods`, `nodes`. The list may change over time.
  - `pods/eviction`: create
  - `events.k8s.io/events`: get, watch, list, create, update, patch, delete
  - `coordination.k8s.io/leases`: create (any), get/patch/delete (resourceName: `descheduler`)
- Extra ClusterRoleBindings:
  - `cluster-monitoring-view` ClusterRole to allow Prometheus Queries when evaluating metrics usage for KubeVirt
- Role: `openshift-kube-descheduler-operator/prometheus-k8s`
  - Read only for `services`, `endpoints` and `pods`. Allows `openshift-monitoring/prometheus-k8s` SA to scrape metrics (pattern used accross OpenShift)

*TODO: Inspect whether `delete` permission on `events` and `coordination.k8s.io/leases` is actually needed - may be excessive. Also investigate whether lease permissions can be scoped to the descheduler namespace only (use Role instead of ClusterRole).*

---

### Soft Tainter Controller RBAC

ServiceAccount: `openshift-kube-descheduler-operator/openshift-descheduler-softtainter`

Permissions:
- ClusterRole: `openshift-descheduler-softtainter` + CR binding
  - Read only: `operator.openshift.io/kubedeschedulers`
  - `nodes`: get, watch, list, patch, update (patch/update operations are validated by `openshift-descheduler-softtainter-vap` ValidatingAdmissionPolicy which checks SA and restricts modifications to descheduler-specific taints only; correct functionality validated by `test/e2e` testSoftTainterControllerWithVAP test)
  - `events.k8s.io/events`: get, watch, list, create, update, patch, delete
  - `coordination.k8s.io/leases`: create (any), get/patch/update/delete (resourceName: `soft-tainter-lock`)
- Extra ClusterRoleBindings:
  - `cluster-monitoring-view` ClusterRole to allow querying Prometheus metrics

*TODO: Inspect whether `delete` permission on `events` and `coordination.k8s.io/leases` is actually needed - may be excessive. Also investigate whether lease permissions can be scoped to the descheduler namespace only (use Role instead of ClusterRole).*

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
  - `coordination.k8s.io/leases`: get, watch, list, create, update, patch, delete (unrestricted, not scoped to specific resourceNames)
  - `admissionregistration.k8s.io/validatingadmissionpolicies`, `admissionregistration.k8s.io/validatingadmissionpolicybindings`: get, watch, list, create, update, patch, delete, deletecollection

- Role: `openshift-descheduler-operator/descheduler-operator` + RoleBinding (namespace-scoped)
  - Core API resources (`services`, `configmaps`, `secrets`, `events`, `serviceaccounts`): get, watch, list, create, update, patch, delete, deletecollection
  - `apps/deployments`, `apps/deployments/scale`: get, watch, list, create, update, patch, delete, deletecollection
  - These permissions are scoped to `openshift-kube-descheduler-operator` namespace only
