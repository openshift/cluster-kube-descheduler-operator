# KubeVirt Descheduler Plugins

Out-of-tree KubeVirt-aware plugins for the [Kubernetes descheduler](https://github.com/kubernetes-sigs/descheduler).

This repository is consumed by [openshift/descheduler](https://github.com/openshift/descheduler) as a statically-linked Go module.  It is maintained by the OpenShift Virtualization team independently of the upstream descheduler release cycle.

---

## Plugins

| Plugin | Extension points | Purpose |
|--------|-----------------|---------|
| [KubevirtMigrationAware](plugins/kubevirtmigrationaware/README.md) | `Filter`, `PreEvictionFilter` | Prevents eviction of `virt-launcher` pods while a VM live-migration is in progress and applies an exponential-backoff cooldown after migrations complete. |

See each plugin's own README for configuration details, tuning guidance, and operational metrics.

---

## Usage

Consumers import the root package and call `Register` once during plugin setup:

```go
import kubevirtplugins "github.com/openshift-virtualization/kubevirt-descheduler-plugins"

func RegisterDefaultPlugins(registry pluginregistry.Registry) {
    // ... other plugins ...
    kubevirtplugins.Register(registry)
}
```

Calling `Register` adds every plugin in this repository to the registry.  Adding a new plugin to this repo does **not** require changing the import or the call site in the downstream descheduler.

---

## Development

### Prerequisites

- Go 1.25+
- A local checkout of [openshift/descheduler](https://github.com/openshift/descheduler)

### Running tests

```bash
go test ./plugins/...
```

The `go.mod` `replace` directive pins `sigs.k8s.io/descheduler` to a specific commit of the `openshift/descheduler` fork.  If you need to test against a local descheduler checkout instead, override the replace directive:

```bash
go mod edit -replace sigs.k8s.io/descheduler=/path/to/openshift/descheduler
go test ./plugins/...
```

### Adding a new plugin

1. Create `plugins/<pluginname>/` with the plugin implementation.
2. Add a `pluginregistry.Register(...)` call for it in `register.go`.
3. Add a row to the table above and a `plugins/<pluginname>/README.md`.

No changes to the downstream descheduler carry commit are required.

---

## Repository structure

```
.
├── register.go              # root package — exposes Register(registry)
├── plugins/
│   └── kubevirtmigrationaware/
│       ├── README.md
│       └── *.go
└── .github/workflows/
    └── test.yml
```

---

## CI

Unit tests run on every pull request via GitHub Actions (see `.github/workflows/test.yml`).  The workflow checks out this repository, sets up Go, and runs `go build`, `go vet`, and `go test ./plugins/...`.

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
