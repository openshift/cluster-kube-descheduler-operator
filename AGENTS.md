# Cluster Kube Descheduler Operator

An OpenShift operator that manages the Kubernetes Descheduler. Built on [library-go](https://github.com/openshift/library-go), it reconciles the `KubeDescheduler` custom resource and deploys the descheduler workload to optimize cluster resource utilization by evicting pods based on configured strategy profiles. The descheduler evicts pods; the scheduler then decides where to reschedule them (rescheduling is not guaranteed). Managed by OLM (Operator Lifecycle Manager).

## Tech Stack

- Go, library-go operator framework
- Upstream [descheduler](https://github.com/kubernetes-sigs/descheduler) (vendored)
- OLM for lifecycle management
- OTE (OpenShift Tests Extension) for e2e tests

## Repository Structure

Listing notable files only.

- **Bundle**
  - `bundle.Dockerfile` - OLM bundle image (used by Konflux)
  - `manifests/` - OLM manifests (ClusterServiceVersion, CRD)
- **Operator**
  - `Dockerfile` - Main operator image build (used by Konflux)
  - `bindata/` - Embedded asset manifests (RBAC, deployments, etc.) reconciled by the operator
  - `cmd/` - operator, soft-tainter (deployed only with KubeVirt profile), tests (OTE framework)
  - `pkg/apis/` - API definitions
  - `pkg/operator/` - Operator controller logic
  - `pkg/softtainter/` - Soft tainter controller logic
  - `pkg/cmd/` - Command line handling
  - `test/` - E2E tests compatible with OpenShift Tests Extension (OTE) framework
- **Konflux**
  - `.tekton/` - Pipeline configuration (updated regularly via automated updates)
- **Auxiliary**
  - `.ci-operator.yaml` - OpenShift CI configuration (needed for automatic updates of Dockerfile.rhel7)
  - `Dockerfile.rhel7` - RHEL7-based operator image (used by CI, but not in production)
  - `deploy/` - Deployment manifests for quick development installation

## Build and Test

```bash
make generate                    # Generate CRD manifests and clients
make update                      # Run gofmt
make verify                      # Run gofmt, go vet, and verify version consistency
make build                       # Build operator and test binaries
make test-unit                   # Run unit tests
make test-e2e                    # Run end-to-end tests
```

Go version: see `go.mod`.

## Controller Pattern

Controllers use the library-go `factory.Controller` base. The main reconciler (`TargetConfigReconciler`) in `pkg/operator/target_config_reconciler.go` has a `sync()` method called by the framework on informer events or periodic resyncs.

The operator wires four controllers in `pkg/operator/starter.go` via `RunOperator()`:
1. `TargetConfigReconciler` — main reconciler for operand deployment
2. `ConfigObserver` — observes cluster config changes
3. `ResourceSyncController` — syncs resources between namespaces
4. `LogLevelController` — manages operator log level

Profile translation: each profile function (e.g., `affinityAndTaintsProfile()`, `lifecycleAndUtilizationProfile()`) receives the `KubeDescheduler` spec and returns a descheduler policy config. Profiles are registered and composed in `target_config_reconciler.go`.

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

### Operator RBAC

ServiceAccount: `openshift-kube-descheduler-operator/openshift-descheduler`

The operator RBAC permissions need to include permissions of both the descheduler operand and the soft-tainter controller (the operator needs to be granted permissions it grants). In addition, the operator has the following extra permissions to manage the operands:

Permissions (extra beyond operands):
- ClusterRole: `openshift-descheduler` + CR binding
  - Read only: `config.openshift.io/schedulers`, `config.openshift.io/infrastructures`, `config.openshift.io/apiservers`, `endpoints`, `apps/replicasets`
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

## Key Conventions

- **Namespace:** `openshift-kube-descheduler-operator` — operator, descheduler operand, and soft tainter all run here. Constant: `operatorclient.OperatorNamespace`.
- **Singleton CR:** The operator expects a single `KubeDescheduler` CR named `cluster` in the operator namespace.
- **Logging:** `k8s.io/klog/v2` with verbosity levels.
- **Error handling:** wrap with `fmt.Errorf("context: %w", err)`.
- **Protected namespaces:** The descheduler never evicts from `openshift-*`, `kube-system`, or `hypershift` namespaces.
- **Soft tainting:** deployed only with `KubeVirtRelieveAndMigrate` profile. Code in `pkg/softtainter/`. Applies soft taints (`PreferNoSchedule`) based on node utilization metrics. Node updates validated by `openshift-descheduler-softtainter-vap` ValidatingAdmissionPolicy.

## Always Do

- Use informers/listers for reading resources in sync loops. Write operations (Create, Update, Delete, Patch) must use direct API calls.
- Use `resourceapply` helpers from library-go when available (e.g., `ApplyDeployment`, `ApplyServiceAccount`, `ApplyClusterRole`).
- When a function accepts a `context.Context`, pass it through to downstream calls. Never discard a context or substitute `context.Background()`/`context.TODO()`.
- Write unit tests for every code change. E2E tests for significant features.
- Deprecate before removing — backwards compatibility matters.
- Before modifying the operator API, ensure there is a corresponding enhancement proposal in [openshift/enhancements](https://github.com/openshift/enhancements). API changes require design review and approval.

## Never Do

- Do not create git commits directly — commits must be signed by the user. Stage changes and let the user commit.
- Do not modify files under `vendor/` directly. Use `go mod tidy && go mod vendor`.
- Do not edit `bindata/assets.go` — it uses Go's `//go:embed` directive. Edit the YAML files under `bindata/assets/kube-descheduler/` directly.
- Never enable aggressive descheduling by default. Mode defaults to Predictive; Automatic requires explicit user opt-in.
- Do not make fixes to library-go functionality in this repo — submit them upstream in [library-go](https://github.com/openshift/library-go).

## References

- **[ARCHITECTURE.md](ARCHITECTURE.md)** — Profiles overview, operator design
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — Code conventions, testing, PR process, review expectations
- **[README.md](README.md)** — Profiles, customizations, parameters, deployment, OTE test commands

`CLAUDE.md` is a pointer to this file (`@AGENTS.md`), so Claude Code and all other coding agents (Codex, Cursor, Copilot, OpenCode, ...) share the same instructions.
