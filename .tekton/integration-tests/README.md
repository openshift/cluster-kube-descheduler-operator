# KDO DAST Integration Tests on Konflux

## Overview

This directory contains the DAST (Dynamic Application Security Testing) pipeline for the
Kube Descheduler Operator (KDO) on Konflux. The pipeline runs automatically when the KDO
bundle image is built, provisioning an ephemeral OCP cluster and executing security scans.

## Pipeline

**File:** `pipelines/kube-descheduler-operator-dast-pipeline-4-21.yaml`

### What it does

1. **Provisions** an ephemeral OCP 4.21 cluster via EaaS (Environment as a Service)
2. **Installs** the KDO operator from the Konflux-built SNAPSHOT bundle using `operator-sdk run bundle`
3. **Applies** an enriched `KubeDescheduler` CR with all spec fields populated to maximize test coverage
4. **Runs oobtkube** (out-of-box testing for Kubernetes) as a Job on the ephemeral cluster to detect
   blind command injection vulnerabilities in the operator's CR reconciliation logic
5. **Runs Trivy** misconfiguration scan against deployed KDO workloads
6. **Uploads results** to GCS (`secaut-bucket`) and surfaces findings in the Konflux UI
   via `TEST_OUTPUT` / `SCAN_OUTPUT` task results

### Architecture

```
Konflux Build Pipeline
        |
        v  (SNAPSHOT with bundle image)
IntegrationTestScenario (konflux-release-data)
        |
        v  (git resolver)
DAST Pipeline (this repo)
        |
        +-- parse-metadata
        +-- eaas-provision-space (ownerKind: PipelineRun)
        +-- provision-cluster (OCP 4.21 + imageContentSources)
        +-- install-kdo-operator (operator-sdk run bundle from SNAPSHOT)
        +-- dast-test
              +-- get-kubeconfig
              +-- orchestrate-dast (kubectl: ns, RBAC, ConfigMap, RapiDAST Job)
              +-- run-trivy (trivy k8s --kubeconfig)
              +-- post-process (TEST_OUTPUT + SCAN_OUTPUT)
```

oobtkube runs **on the ephemeral cluster** as a Kubernetes Job (not as a Tekton step).
This ensures the oobtkube TCP listener and the KDO operator share the same network,
allowing the callback mechanism to work correctly.

### Container images used

| Step | Image | Why |
|------|-------|-----|
| install-operator, orchestrate-dast, post-process | `quay.io/konflux-ci/konflux-test:latest` | Has `oc`, `kubectl`, `jq`, `/utils.sh` |
| RapiDAST Job (on ephemeral cluster) | `quay.io/redhatproductsecurity/rapidast:latest` | Has `rapidast.py`, `oobtkube.py` (via `python3.12`), `trivy`, `kubectl` |
| run-trivy | `quay.io/redhatproductsecurity/rapidast:latest` | Has `trivy` |
| results-fetcher | `registry.access.redhat.com/ubi9/ubi:latest` | Pod to mount PVC for `kubectl cp` (needs `tar`) |

## Prerequisites

### GCS Secret

A Google Cloud Storage service account key must be provisioned in the `kdo-workloads-tenant`
namespace on the Konflux cluster:

- **Secret name:** `rapidast-sa-kdo-key`
- **Key:** `sa-key` (JSON service account credentials)
- **Bucket:** `secaut-bucket`

Request access via the SecAut Bucket Access Repository (same process as RHOSDT).

### IntegrationTestScenario

The ITS resource `kube-descheduler-operator-4-21-dast` must exist in
`konflux-release-data` under the `kdo-workloads-tenant`. It is configured with:

- `test.appstudio.openshift.io/optional: "true"` label (non-blocking)
- Context filter: `component_kube-descheduler-operator-bundle-4-21` (triggers on bundle builds)
- Param: `kdo_version: "4.21"` (for GCS path tagging)

### imageContentSources

The ephemeral cluster is configured with image content source mirrors so that
`operator-sdk run bundle` can pull images built by Konflux:

| Registry source | Konflux mirror |
|----------------|----------------|
| `registry.redhat.io/kube-descheduler-operator/kube-descheduler-rhel9-operator` | `quay.io/redhat-user-workloads/kdo-workloads-tenant/kube-descheduler-operator` |
| `registry.redhat.io/kube-descheduler-operator/descheduler-rhel9` | `quay.io/redhat-user-workloads/kdo-workloads-tenant/kube-descheduler-operator` |
| `registry.redhat.io/kube-descheduler-operator/kube-descheduler-operator-bundle` | `quay.io/redhat-user-workloads/kdo-workloads-tenant/kube-descheduler-operator-bundle` |

## Interpreting Results

### Konflux UI

- **TEST_OUTPUT**: Shows `SUCCESS` if the pipeline completed. Check `SCAN_OUTPUT` for details.
- **SCAN_OUTPUT**: JSON with vulnerability counts by severity (critical/high/medium/low).

### GCS Archive

Results are uploaded to `gs://secaut-bucket/kdo/<version>/oobtkube/` by RapiDAST's
native `google.cloud.storage` integration. Contains the full SARIF report.

### oobtkube Findings

oobtkube detects blind command injection by injecting `curl <pod-ip>:<port>` payloads
into CR string fields. The pod IP is injected via the Kubernetes downward API (`POD_IP` env var).
If the operator executes any injected command, oobtkube's listener receives the callback
and reports a finding. Any finding indicates a critical vulnerability.

The scanner is configured as `generic_oobtkube` in RapiDAST (using the `generic_<name>` pattern)
and uses `python3.12` (the default `python3` in the RapiDAST image lacks `pyyaml`).

### Trivy Findings

Trivy scans deployed KDO workloads for Kubernetes misconfigurations (HIGH/CRITICAL severity).
Results are saved as JSON in the shared volume.

## Enriched CR

The test uses an enriched `KubeDescheduler` CR with maximum field coverage:

- **5 compatible profiles**: AffinityAndTaints, SoftTopologyAndDuplicates, LifecycleAndUtilization,
  EvictPodsWithLocalStorage, EvictPodsWithPVC
- **All profileCustomizations** including free-string fields (`devActualUtilizationProfile`,
  `thresholdPriorityClassName`) that are primary oobtkube injection targets
- **All numeric/boolean fields** populated to exercise full operator reconciliation paths

## Known Limitations

- **Trivy namespace flag**: Trivy k8s uses `--include-namespaces` (not `--namespace`).
- **`python3.12` required for oobtkube**: The RapiDAST image's default `python3` (3.9) lacks
  `pyyaml`. The config must use `python3.12` for the oobtkube inline command.
- **`hostname` unavailable in RapiDAST container**: Pod IP must be injected via the Kubernetes
  downward API (`POD_IP` env var) instead of `$(hostname -i)`.
- **`operator-sdk` runtime download**: Currently downloaded at runtime. Consider building
  a lightweight KDO test image if more integration tests are added.
- **Non-hermetic**: Integration test pipelines on Konflux are not subject to hermetic build
  constraints. Runtime downloads and `:latest` tags are acceptable per Konflux docs.
