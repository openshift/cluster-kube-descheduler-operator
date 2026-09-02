# Cluster Kube Descheduler Operator

An OpenShift operator that manages the Kubernetes Descheduler. Built on [library-go](https://github.com/openshift/library-go), it reconciles the `KubeDescheduler` custom resource and deploys the descheduler workload to optimize cluster resource utilization by evicting pods based on configured strategy profiles. The descheduler evicts pods; the scheduler then decides where to reschedule them (rescheduling is not guaranteed). Managed by OLM (Operator Lifecycle Manager).

## Tech Stack

- Go, library-go operator framework
- Upstream [descheduler](https://github.com/kubernetes-sigs/descheduler) (vendored)
- OLM for lifecycle management
- OTE (OpenShift Tests Extension) for e2e tests

## Controller Pattern

Controllers use the library-go `factory.Controller` base. The main reconciler (`TargetConfigReconciler`) in `pkg/operator/target_config_reconciler.go` has a `sync()` method called by the framework on informer events or periodic resyncs.

The operator wires four controllers in `pkg/operator/starter.go` via `RunOperator()`:
1. `TargetConfigReconciler` — main reconciler for operand deployment
2. `ConfigObserver` — observes cluster config changes
3. `ResourceSyncController` — syncs resources between namespaces
4. `LogLevelController` — manages operator log level

Profile translation: each profile function (e.g., `affinityAndTaintsProfile()`, `lifecycleAndUtilizationProfile()`) receives the `KubeDescheduler` spec and returns a descheduler policy config. Profiles are registered and composed in `target_config_reconciler.go`.

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

- **[CLAUDE.md](CLAUDE.md)** — Repository structure, build/test commands, RBAC configuration
- **[ARCHITECTURE.md](ARCHITECTURE.md)** — Profiles overview, operator design
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — Code conventions, testing, PR process, review expectations
- **[README.md](README.md)** — Profiles, customizations, parameters, deployment, OTE test commands
