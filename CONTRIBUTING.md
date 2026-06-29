# Contributing to the OpenShift Kube Descheduler Operator

This document serves as a guide for contributing to the OpenShift Kube Descheduler Operator, maintained by the OpenShift Control Plane group.

The OpenShift Kube Descheduler Operator manages the Kubernetes Descheduler in OpenShift clusters, evicting pods based on descheduling strategies to optimize cluster resource utilization. The descheduler evicts pods; the scheduler then decides where to reschedule them.

This document is explicitly for contributions to this component repository and not for high-level feature proposals within OpenShift.

Feature proposals should follow the OpenShift Enhancement Proposal process outlined in https://github.com/openshift/enhancements/blob/master/dev-guide/feature-zero-to-hero.md#openshift-feature-development-zero-to-hero-guide. If you are looking for a review on an OpenShift Enhancement Proposal that involves changes to components maintained by the control plane group, please request a review in the [`#forum-ocp-workloads`](https://redhat.enterprise.slack.com/archives/CKJR6200N) Slack channel.

This document contains the following sections:

- [Code conventions](#code-conventions) - A collection of guidelines, style suggestions, and tips for writing code.
- [Testing guidelines](#testing-guidelines) - Guidelines and expectations for testing of contributions.
- [Pull Request process/guidelines](#pull-request-process-and-guidelines) - Guidelines and expectations of pull requests containing contributions.
- [Review expectations](#review-expectations) - Guidelines and expectations for requesting reviews and interacting with reviewers.

## Code Conventions

We largely follow the [Kubernetes Code Conventions](https://github.com/kubernetes/community/blob/main/contributors/guide/coding-conventions.md#code-conventions).

Review both the Kubernetes Code Conventions and the ones specified here. There will be some overlap. If any conventions are at odds with one another, prefer the conventions explicitly documented here.

### Bash

- Follow the [shell styleguide](https://google.github.io/styleguide/shellguide.html).
- Use [`shellcheck`](https://github.com/koalaman/shellcheck) to identify common mistakes or caveats.
- Ensure that all scripts run consistently across Linux and MacOS.

### Golang (Go)

- Review [Effective Go](https://go.dev/doc/effective_go).
- Review common [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments).
- Review and avoid [Go Landmines](https://gist.github.com/lavalamp/4bd23295a9f32706a48f)
- Comment your code following the [Go comment conventions](https://go.dev/doc/comment).
    - Comments should be meaningful and add context and/or explain choices that cannot be expressed through clear code.
    - All exported types, functions, and methods must have descriptive comments.
    - All unexported types, functions, and methods should have descriptive comments.
- When adding command-line flags, use dashes/hyphens (`-`) and not underscores (`_`).
- Naming
    - Please consider package name when selecting an interface name, and avoid redundancy. For example, `storage.Interface` is better than `storage.StorageInterface`.
    - Do not use uppercase characters, underscores, or dashes in package names.
    - Please consider parent directory name when choosing a package name. For example, `pkg/controllers/autoscaler/foo.go` should say `package autoscaler` not `package autoscalercontroller`.
        - Unless there's a good reason, the package foo line should match the name of the directory in which the .go file exists.
        - Importers can use a different name if they need to disambiguate.
    - Locks should be called `lock` and should never be embedded (always `lock sync.Mutex`). When multiple locks are present, give each lock a distinct name following Go conventions: `stateLock`, `mapLock` etc.
- Error handling
    - Wrap errors with meaningful context before returning or logging them.
- When logging, follow the [Kubernetes Logging Conventions](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-instrumentation/logging.md).
- When patching OpenShift-maintained forks of "upstream" repositories, patches should be as small as reasonably possible and should minimize touch points with code that is likely to change and impact the rebasing process.

### General

Regardless of the programming language, make sure to take the following into consideration:
- Keep readability / maintainability in mind when writing code.
    - Clever code and abstractions are often harder to reason about after the fact. Keep clever code and abstractions to the minimum necessary to accomplish the end-goal.

### Directory and File Conventions

- Avoid package sprawl. Find an appropriate subdirectory for new packages.
    - Libraries with no appropriate home belong in new package subdirectories of `pkg/util`.
- Avoid general utility packages. Packages called "util" are suspect. Instead, derive a name that describes your desired function. For example, the utility functions dealing with waiting for operations are in the `wait` package and include functionality like `Poll`. The full name is `wait.Poll`.
- All filenames should be lowercase.
- Go source files and directories use underscores, not dashes.
    - Package directories should generally avoid using separators as much as possible. When package names are multiple words, they usually should be in nested subdirectories.

### Controller Patterns

All controllers must follow the **library-go factory pattern**:

```go
import (
    "github.com/openshift/library-go/pkg/controller/factory"
    "github.com/openshift/library-go/pkg/operator/events"
    "github.com/openshift/library-go/pkg/operator/v1helpers"
    "k8s.io/client-go/informers"
)

func NewMyController(
    operatorClient v1helpers.OperatorClient,
    kubeInformers informers.SharedInformerFactory,
    recorder events.Recorder,
) factory.Controller {
    c := &myController{
        operatorClient: operatorClient,
        lister:        kubeInformers.Apps().V1().Deployments().Lister(),
    }

    return factory.New().
        WithInformers(kubeInformers.Apps().V1().Deployments().Informer()).
        WithSync(c.sync).
        ToController("MyController", recorder)
}
```

**Key rules:**
- Use informers/listers for reading resources in sync loops
  - Never make direct API calls for Get/List operations on watched resources
  - Write operations (Create/Update/Delete/Patch) require direct API calls
- Handle errors in sync() properly
  - Return transient errors (API failures, network issues) to trigger rate-limited retry
  - Wrap errors with context using `fmt.Errorf("context: %w", err)` for debugging
  - For validation errors, handle appropriately (e.g., scale down deployment, update status) before returning
- Use `resourceapply` helpers from library-go when available (e.g., ApplyDeployment, ApplyServiceAccount)
  - For CRD resources, use `ApplyKnownUnstructured` if supported (ServiceMonitor, PrometheusRule, etc.)
  - For operations without helpers (e.g., UpdateScale, UpdateStatus), use direct API calls or v1helpers with proper error handling
- Register new controllers in `pkg/operator/starter.go`

## Testing Guidelines

These are high-level testing guidelines. Individual component repositories may have additional testing guidelines to follow when making contributions.

### Unit Tests

- **Required**: All changes must include unit test additions/changes (exceptions at reviewer/approver discretion)
- Table-driven tests are preferred for testing multiple scenarios/inputs. For an example, see https://github.com/openshift/cluster-authentication-operator/blob/a493799952e9b6838021ccc7d15d3d37d7ad3508/pkg/controllers/externaloidc/externaloidc_controller_test.go#L108
- Tests must pass on all platforms (at the very least, Linux + MacOS)
- Co-locate tests with source code (`*_test.go`)
- Mock external dependencies
- Aim for >70% code coverage
- Do not expect asynchronous operations to happen immediately - use wait and retry patterns instead

Example:

```go
func TestMyController_Sync(t *testing.T) {
    tests := []struct {
        name    string
        setup   func(*fakeClient)
        wantErr bool
    }{
        {
            name: "successful sync",
            setup: func(c *fakeClient) {
                // Setup test state
            },
            wantErr: false,
        },
        // More test cases...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### E2E Tests

- **Required for**: New descheduler profiles, mode changes, major features
- Significant features should come with integration and/or end-to-end tests where appropriate
    - End-to-end tests _may_ be scoped as a separate work item when they must be added to the openshift/origin repository (at reviewer/approver discretion)
- Use OpenShift Tests Extension (OTE) framework - see [Using OpenShift Tests Extension](#using-openshift-tests-extension-ote) section
- Place tests in `test/e2e/`
- Follow Ginkgo v2 conventions
- Use stable, deterministic test names (no dynamic pod names, timestamps, UUIDs)

**Topology compatibility requirements**:
- Add `[Skipped:MicroShift]` if not applicable to MicroShift
- Add `[Skipped:SingleReplicaTopology]` if requires multiple nodes
- Use `[apigroup:...]` labels to indicate API dependencies

**Manual testing**: For manual verification, use the [`Cluster Bot` Slack App](https://redhat.enterprise.slack.com/archives/D03KX7M1CRJ) to create test clusters. See [Verifying Your Changes](#verifying-your-changes--creating-an-openshift-cluster-from-a-pr) for details on using Cluster Bot and [Getting an OpenShift Cluster](#getting-an-openshift-cluster) for cluster setup. Follow https://github.com/openshift/enhancements/blob/master/dev-guide/operators.md for guidance on building component images and modifying cluster-operators.

## Pull Request Process and Guidelines

This section assumes that you have a functional understanding of `git` and how to create a pull request on GitHub.

If you do not, start with [GitHub's "Getting Started" guide](https://docs.github.com/en/get-started/start-your-journey).

### Prerequisites

Before you commit any changes or create any pull requests, you must adhere to OpenShift contribution policies. Currently, that means enabling commit signature verification.

See https://docs.google.com/document/d/1184EPSGunUkcSQYUK8T4a6iyawwi6f2zxdbB2jtG9nQ/edit?usp=sharing for details on enabling commit signature verification (requires Red Hat authentication).

### Creating a Pull Request

When creating a pull request, include the following:

- A brief, but descriptive, title.
    - All pull requests _should_ link to a Jira ticket associated with the work. There is automation that performs this linking when prefixing the title with the Jira ticket identifier like: `CNTRLPLANE-XXXX: my pull request title`. For pull requests that have no Jira ticket associated with it, you can prefix it with `NO-JIRA:` to signal that there is not a Jira ticket associated with it.
- A useful description of the changes being made and why they are important. Include links to supporting documents and any additional context that reviewers may need.

### CI / CD

For CI/CD, OpenShift uses Prow to run various checks. This can include unit tests, e2e tests, linters, etc.

The jobs configured for each repository are in https://github.com/openshift/release/tree/main/ci-operator/config/openshift.

There are often a mixture of required and optional checks as well as merge criteria that must be met before a pull request can merge. When any of these checks fail, the GitHub Prow bot will leave a comment on the PR with links to the run of that check that failed.

As the PR author, it is your responsibility to evaluate the failed checks and determine if there are any changes necessary to pass the checks. If you suspect that the check failure was a flake, you can trigger retests by commenting `/retest` (or `/retest-required` for retesting only the required checks) on the PR.

### Verifying Your Changes / Creating an OpenShift Cluster from a PR

As part of merging a PR, there is a requirement to verify that the changes you've made are working as expected using the `/verified` comment command.

While there are a lot of scenarios where the existing CI/CD checks may be sufficient to verify your changes are working (and can be denoted by commenting `/verified by ci`), there may be scenarios where manual verification is required.

You can use the `Cluster Bot` Slack App to create a cluster from a PR by sending it a message in the format of `launch ${OCP_VERSION},${PR_LINK} ${PLATFORM},${VARIANT}`. As an example, `launch 4.23,https://github.com/openshift/cluster-kube-descheduler-operator/pull/123 aws,techpreview` would launch an OpenShift 4.23 cluster with the changes made in openshift/cluster-kube-descheduler-operator#123 running on AWS with the TechPreviewNoUpgrade feature-set enabled. For more information on what `Cluster Bot` can do, you can send it a message saying `help` and it will respond with additional documentation on how it can be used.

Once you've verified your changes work as expected, you can mark the PR as verified by commenting `/verified by @{your_github_handle}` on the PR.

## Review Expectations

### Requesting a Review

If you are not a member of the OpenShift control plane team and you need a review on a PR, post it in the [#forum-ocp-workloads](https://redhat.enterprise.slack.com/archives/CKJR6200N) Slack channel or reach out to folks outlined in the OWNERS file directly.

If you are a member of the OpenShift control plane team, reviews should come from your feature team. In the event your feature team does not have someone that can approve a PR, post it in the [#control-plane](https://redhat.enterprise.slack.com/archives/CC3CZCQHM) Slack channel.

OpenShift uses AI code review tools as part of the code review process. Before requesting a review, address all feedback from the code review agent(s). It is up to your discretion as the contributor how you would like to address that feedback. Responding with an explanation as to why you are not going to take action on a comment made by the agent is an acceptable way to "address" its feedback.

### Interacting with Reviewers

When interacting with reviewers/approvers:

- Be professional.
- Be respectful of differing opinions, viewpoints, and experiences.
- Gracefully give and receive constructive feedback.
- Focus on what is best for the product/organization, not just us as individuals.

A special note on the usage of AI - to respect the time of those that are reviewing your contribution, please do not use AI to respond to review comments.

**Review timeline**: Most PRs are reviewed within 2-3 business days. PRs go through automated checks (unit tests, linters, verifications), code review (at least one maintainer approval required), and E2E tests before merging.

---

# Operator-Specific Development Guide

The following sections provide operator-specific guidance for development, testing, and debugging.

## Getting Started

To get started, [fork](https://help.github.com/articles/fork-a-repo) the [openshift/cluster-kube-descheduler-operator](https://github.com/openshift/cluster-kube-descheduler-operator) repo.

```bash
git clone https://github.com/<YOUR_USERNAME>/cluster-kube-descheduler-operator.git
cd cluster-kube-descheduler-operator
```

## Development Prerequisites

- **Go 1.25+** (check `go.mod` for exact version)
- **OpenShift cluster** (4.12+) or access to create one
- **oc CLI** installed and configured
- **make** for build automation
- **git** for version control
- **podman** or **docker** for building container images

## Building and Testing Locally

### Build the Operator

```bash
# Build all binaries (operator, tests-ext, soft-tainter)
make build
# Expected: Produces 3 binaries in current directory:
#   - ./cluster-kube-descheduler-operator
#   - ./cluster-kube-descheduler-operator-tests-ext
#   - ./soft-tainter
# Success: No compilation errors, exit code 0

# Verify code (runs gofmt, go vet, etc.)
make verify
# Expected: Runs gofmt, go vet, version checks
# Success: Exit code 0, no formatting/lint violations
# Failure: Shows which files need formatting or have vet issues

# Clean build artifacts
make clean
# Expected: Removes binaries and build artifacts
```

The operator binary is built as `cluster-kube-descheduler-operator`.

### Run E2E Tests

```bash
# Run end-to-end tests (requires OpenShift cluster with KUBECONFIG set)
export KUBECONFIG=/path/to/kubeconfig
export RELEASE_IMAGE_LATEST=<registry>/ocp/release:latest
export NAMESPACE=<ci-namespace>
make test-e2e
# Expected: Runs unit tests + E2E tests against the cluster
# Success: "PASS" for all test packages, exit code 0
# Output includes: Test suite results, timing, coverage
# Failure: Shows failed test names, assertion errors, stack traces
```

**Note**: E2E tests:
- Require a running OpenShift cluster
- Deploy the operator and descheduler
- Create test workloads and validate descheduling
- Can take 15-30 minutes to complete

### E2E Tests

See **[TESTING.md](./TESTING.md)** for comprehensive testing guide including how to write, run, and debug tests.

## Testing on an OpenShift Cluster

The easiest way to test your changes is to deploy to a live OpenShift 4.x cluster.

### Getting an OpenShift Cluster

**Option 1: Use existing cluster**
```bash
oc login <cluster-url>
```

**Option 2: Create a new cluster**

Go to [Red Hat Hybrid Cloud Console](https://console.redhat.com/openshift/create) to create an OpenShift cluster.

For the latest `openshift-install` and `oc` clients:
- **Stable releases**: [console.redhat.com/openshift/downloads](https://console.redhat.com/openshift/downloads)
- **All versions**: [mirror.openshift.com/pub/openshift-v4/clients/ocp/](https://mirror.openshift.com/pub/openshift-v4/clients/ocp/)
- **Development builds**: [mirror.openshift.com/pub/openshift-v4/clients/ocp-dev-preview/](https://mirror.openshift.com/pub/openshift-v4/clients/ocp-dev-preview/)

### Common Prerequisites: Building Your Custom Image

Before using any of the deployment methods below, you'll need to build and push your custom operator image:

```bash
export QUAY_USER=<your_quay_username>
export IMAGE_TAG=dev-$(git rev-parse --short HEAD)

# Build operator image
podman build -t quay.io/${QUAY_USER}/cluster-kube-descheduler-operator:${IMAGE_TAG} -f Dockerfile.rhel7 .

# Login to registry
podman login quay.io -u ${QUAY_USER}

# Push image
podman push quay.io/${QUAY_USER}/cluster-kube-descheduler-operator:${IMAGE_TAG}
```

### Option 1: Quick Development Deployment

This is the fastest way to test changes without OLM.

**Prerequisites**: Complete [Common Prerequisites](#common-prerequisites-building-your-custom-image) to build your image first.

1. **Update deployment manifest**:

Edit `deploy/05_deployment.yaml`:
- Update `.spec.template.spec.containers[0].image` to your image
- Update `RELATED_IMAGE_OPERAND_IMAGE` env var to the descheduler image (or use existing)

```yaml
# Example:
spec:
  template:
    spec:
      containers:
      - name: descheduler-operator
        image: quay.io/<YOUR_USER>/cluster-kube-descheduler-operator:dev-abc123
        env:
        - name: RELATED_IMAGE_OPERAND_IMAGE
          value: "quay.io/openshift/origin-descheduler:4.17.0"
```

2. **Update the CR** (optional):

Edit `deploy/02_kube-descheduler-operator.cr.yaml` to configure descheduler profiles and settings:

```yaml
spec:
  managementState: Managed
  deschedulingIntervalSeconds: 3600  # 1 hour
  mode: Automatic
  profiles:
  - AffinityAndTaints
  - TopologyAndDuplicates
```

3. **Deploy all manifests**:

```bash
oc apply -f deploy/
```

This creates:
- Namespace: `openshift-kube-descheduler-operator`
- ServiceAccount, RBAC (ClusterRole, ClusterRoleBinding)
- CRD: `KubeDescheduler`
- Deployment: descheduler-operator
- CR: `KubeDescheduler/cluster`

**Note**: If you see an error `no matches for kind "KubeDescheduler"`, the CRD needs a moment to be established by Kubernetes. Wait 5 seconds and apply the CR again:

```bash
oc apply -f deploy/02_kube-descheduler-operator.cr.yaml
```

4. **Verify deployment**:

```bash
oc get pods -n openshift-kube-descheduler-operator
# Expected: 1 pod named "descheduler-operator-*" with STATUS: Running
# If KubeVirtRelieveAndMigrate profile enabled: 1 additional "soft-tainter-*" pod

oc get deployment -n openshift-kube-descheduler-operator descheduler
# Expected: 1/1 READY, 1 UP-TO-DATE, 1 AVAILABLE
# If not ready: Check pod logs and events
```

For comprehensive status checks, see [Debugging Tips](#debugging-tips).

### Option 2: OLM-Aware Deployment Patching

When testing on production clusters or clusters where the operator is managed by OLM, you need to patch differently.

**Prerequisites**: Complete [Common Prerequisites](#common-prerequisites-building-your-custom-image) to build your image first.

1. **Patch the cluster version** to allow custom operator image:

```bash
oc patch clusterversion/version --patch '{"spec":{"overrides":[{"kind":"Deployment","name":"descheduler-operator","namespace":"openshift-kube-descheduler-operator","group":"apps","unmanaged":true}]}}' --type=merge
```

2. **Patch the deployment** to use your image:

**Note:** If the operator is deployed via OLM (check with `oc get csv -n openshift-kube-descheduler-operator`), you must patch the ClusterServiceVersion instead:

```bash
# For OLM-managed operators (recommended approach)
CSV_NAME=$(oc get csv -n openshift-kube-descheduler-operator -o jsonpath='{.items[0].metadata.name}')

oc patch csv $CSV_NAME \
  -n openshift-kube-descheduler-operator \
  --type='json' -p='[
    {"op": "replace", "path": "/metadata/annotations/containerImage", "value": "quay.io/<YOUR_USERNAME>/cluster-kube-descheduler-operator:dev"},
    {"op": "replace", "path": "/spec/relatedImages/1/image", "value": "quay.io/<YOUR_USERNAME>/cluster-kube-descheduler-operator:dev"},
    {"op": "replace", "path": "/spec/install/spec/deployments/0/spec/template/spec/containers/0/image", "value": "quay.io/<YOUR_USERNAME>/cluster-kube-descheduler-operator:dev"}
  ]'
```

**Alternative (for non-OLM deployments):**

```bash
oc patch deployment descheduler-operator \
  -n openshift-kube-descheduler-operator \
  --patch '{"spec":{"template":{"spec":{"containers":[{"name":"descheduler-operator","image":"quay.io/<YOUR_USERNAME>/cluster-kube-descheduler-operator:dev"}]}}}}' \
  --type=strategic
```

3. **Verify rollout**:

```bash
oc get pods -n openshift-kube-descheduler-operator -w
oc logs -n openshift-kube-descheduler-operator deployment/descheduler-operator -f
```

For comprehensive status checks, see [Debugging Tips](#debugging-tips).

### Option 3: Testing the Descheduler

1. **Create test workloads**:

```bash
# Create namespace
oc create namespace test-descheduler
# Expected: namespace/test-descheduler created

# Create pods with node affinity issues (for AffinityAndTaints profile)
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: test-pod-1
  namespace: test-descheduler
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: httpd
    image: registry.access.redhat.com/ubi9/httpd-24:latest
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
      runAsNonRoot: true
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: node-role.kubernetes.io/worker
            operator: Exists
EOF
# Expected: pod/test-pod-1 created

# Verify pod is running
oc get pod test-pod-1 -n test-descheduler
# Expected: NAME: test-pod-1, READY: 1/1, STATUS: Running
```

2. **Configure descheduler profiles**:

```bash
oc patch kubedescheduler cluster --type=merge -p '
{
  "spec": {
    "mode": "Automatic",
    "deschedulingIntervalSeconds": 60,
    "profiles": ["AffinityAndTaints", "TopologyAndDuplicates"]
  }
}'
# Expected: kubedescheduler.operator.openshift.io/cluster patched
# If CR doesn't exist: Apply deploy/02_kube-descheduler-operator.cr.yaml first
```

3. **Monitor descheduler activity**:

```bash
# Watch descheduler logs
oc logs -n openshift-kube-descheduler-operator deployment/descheduler -f
# Expected: Shows descheduler evaluation logs
# Look for: "Processing node", "Evicted pod" (Automatic mode), "would be evicted" (Predictive mode)

# Check pod evictions
oc get events -n test-descheduler --field-selector involvedObject.kind=Pod
# Expected: Shows pod lifecycle events (Scheduled, Pulled, Created, Started)
# In Automatic mode with matching strategy: May see "Evicted" events
```

4. **Cleanup**:

```bash
oc delete namespace test-descheduler
# Expected: namespace "test-descheduler" deleted
```

## Code Changes and Regeneration

### After Modifying CRD Types

If you modify `pkg/apis/descheduler/v1/types_descheduler.go`:

```bash
# Regenerate CRD manifests
make regen-crd

# This updates:
# - manifests/kube-descheduler-operator.crd.yaml
# - deploy/0000_00_kube-descheduler-operator.crd.yaml

# Verify changes
make verify
```

**Note**: `make regen-crd` handles most common cases, but is not foolproof. For complex API changes, the underlying generation script may need updates. Always verify the generated CRD manifests match your intended changes.

### After Modifying API Package

If you add new API versions or modify clientset/informers:

```bash
# Regenerate all clients
make generate-clients

# Or regenerate everything (CRDs + clients)
make generate
```

### After Modifying Dependencies

```bash
# Update a specific dependency
go get github.com/openshift/library-go@latest

# Tidy dependencies
go mod tidy

# Update vendor directory (if used)
go mod vendor
```

**Always verify changes**: After any code generation or dependency updates, run `make verify` to ensure consistency.

## Common Tasks

### Update Descheduler Image

The operator deploys a separate descheduler deployment (operand). To update its image:

1. Edit `deploy/05_deployment.yaml` or CSV:

```yaml
env:
- name: RELATED_IMAGE_OPERAND_IMAGE
  value: "quay.io/openshift/origin-descheduler:4.18.0"  # Update version
```

2. Redeploy operator or restart pod:

```bash
oc delete pod -n openshift-kube-descheduler-operator -l app=descheduler-operator
```

### Change Descheduling Interval

Update the CR:

```bash
oc patch kubedescheduler cluster --type=merge -p '
{
  "spec": {
    "deschedulingIntervalSeconds": 7200
  }
}'
```

### Disable the Operator

Set `managementState` to `Removed`:

```bash
oc patch kubedescheduler cluster --type=merge -p '
{
  "spec": {
    "managementState": "Removed"
  }
}'
```

This removes the descheduler deployment but leaves the operator running.

## Debugging Tips

### Operator Not Starting

```bash
# Check operator pod logs
oc logs -n openshift-kube-descheduler-operator deployment/descheduler-operator
# Look for: Errors, panics, "failed to" messages
# Common issues: Image pull errors, RBAC permissions, invalid CR

# Check events
oc get events -n openshift-kube-descheduler-operator --sort-by='.lastTimestamp' | tail -10
# Look for: Warning events, FailedScheduling, ImagePullBackOff, CrashLoopBackOff

# Check operator status
oc get kubedescheduler cluster -o jsonpath='{.status.conditions}' | jq
# Expected conditions:
#   - Available: "True"
#   - Progressing: "False"
#   - Degraded: "False"
# If Degraded: "True", check .status.conditions[].message for error details
```

### Descheduler Not Working

```bash
# Check descheduler deployment
oc get deployment -n openshift-kube-descheduler-operator descheduler
# Expected: 1/1 READY, STATUS should show deployment is available
# If 0/1: Check pod status below

oc get pods -n openshift-kube-descheduler-operator -l app=descheduler
# Expected: 1 pod with STATUS: Running
# Common issues: CrashLoopBackOff (check logs), ImagePullBackOff (check image)

# Check descheduler logs
oc logs -n openshift-kube-descheduler-operator -l app=descheduler
# Look for: "Processing node", "Evicted pod", strategy execution
# Predictive mode: Look for "pod would be evicted" (no actual evictions)
# Automatic mode: Look for "Evicted pod" (actual evictions)

# Check descheduler configuration
oc get kubedescheduler cluster -o yaml
# Verify: spec.profiles lists expected profiles
# Verify: spec.mode is "Automatic" or "Predictive"
# Verify: spec.deschedulingIntervalSeconds is set

# Check events
oc get events -n openshift-kube-descheduler-operator --field-selector involvedObject.kind=Pod
# Look for: Pod eviction events, "Evicted" reason
```

### Comprehensive Status Checks

```bash
# Check operator pod
oc get pods -n openshift-kube-descheduler-operator
# Expected: descheduler-operator-* pod with STATUS: Running
# Optional: soft-tainter-* pod if KubeVirtRelieveAndMigrate profile enabled

# Check CR status
oc get kubedescheduler cluster -o yaml
# Key fields to check:
#   - .status.conditions: Available=True, Degraded=False
#   - .spec.profiles: Lists enabled profiles
#   - .spec.mode: "Automatic" (evicts) or "Predictive" (simulates)
#   - .status.readyReplicas: Should equal desired replicas

# Check descheduler Deployment (created by operator)
oc get deployment -n openshift-kube-descheduler-operator descheduler

# Check ClusterOperator status
oc get clusteroperator kube-descheduler
```

### Enable Debug Logging

Add `--v=4` to operator args in `deploy/05_deployment.yaml`:

```yaml
spec:
  template:
    spec:
      containers:
      - name: descheduler-operator
        args:
        - "operator"
        - "--v=4"  # Add this for debug logging
```

Alternatively, for OLM-managed deployments (uses the same CSV patching pattern as [Option 2: OLM-Aware Deployment Patching](#option-2-olm-aware-deployment-patching)):

```bash
# For OLM-managed operators
CSV_NAME=$(oc get csv -n openshift-kube-descheduler-operator -o jsonpath='{.items[0].metadata.name}')

oc patch csv $CSV_NAME \
  -n openshift-kube-descheduler-operator \
  --type='json' -p='[
    {"op": "replace", "path": "/spec/install/spec/deployments/0/spec/template/spec/containers/0/args", "value": ["operator", "--v=4"]}
  ]'
```

## Release Process

(For maintainers)

1. **Update version** in relevant files (CSV, manifests)
2. **Tag release**:
   ```bash
   git tag v1.x.x
   git push origin v1.x.x
   ```
3. **Build images** via CI/CD
4. **Update release notes** in GitHub releases

## Getting Help

- **Issues**: [github.com/openshift/cluster-kube-descheduler-operator/issues](https://github.com/openshift/cluster-kube-descheduler-operator/issues)
- **Slack**: OpenShift Slack workspace (for Red Hat employees)
- **Docs**: [AGENTS.md](./AGENTS.md), [ARCHITECTURE.md](./ARCHITECTURE.md)

## Code of Conduct

This project follows the OpenShift Code of Conduct and community guidelines.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
