# AGENTS.md

AI-readable quick reference with verified commands and code locations.

## Quick Links

- **[README.md](./README.md)** - User documentation and descheduler profile descriptions
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** - Contribution guidelines and controller patterns
- **[ARCHITECTURE.md](./ARCHITECTURE.md)** - System architecture and design decisions
- **[TESTING.md](./TESTING.md)** - Comprehensive testing guide

## Repository

The OpenShift Kube Descheduler Operator manages the Kubernetes Descheduler to optimize cluster resource utilization by evicting pods based on configured strategies. The descheduler only evicts pods; the scheduler then decides where to reschedule them (rescheduling is not guaranteed).

## Use Cases

**I want to build the operator binary:**
- Run `make build` to produce binaries
- Binaries are located at: `./cluster-kube-descheduler-operator`, `./cluster-kube-descheduler-operator-tests-ext`, `./soft-tainter`

**I want to understand how a specific descheduler profile works:**
- Profile definitions: `pkg/apis/descheduler/v1/types_descheduler.go` (search: "type DeschedulerProfile")
- Profile implementations: `pkg/operator/target_config_reconciler.go` (search: "<profileName>Profile")
- User-facing documentation: `README.md` section "Profiles"

**I want to find where the operator reconciles the KubeDescheduler CR:**
- Main reconciler: `pkg/operator/target_config_reconciler.go` (search: "func (c TargetConfigReconciler) sync")
- Operator client: `pkg/operator/operatorclient/interfaces.go`

**I want to add a new descheduler profile:**
1. Add profile constant to `pkg/apis/descheduler/v1/types_descheduler.go` (search: "var (")
2. Implement profile function in `pkg/operator/target_config_reconciler.go`
3. Add CRD validation rules in `manifests/kube-descheduler-operator.crd.yaml`
4. Regenerate CRD: `make regen-crd` (verify output; script may need updates)
5. Update `README.md` section "Profiles" with new profile documentation
6. Run `make verify`

**I want to update a Go dependency:**
```bash
go get github.com/openshift/library-go@latest
go mod tidy
go mod vendor
make verify
```

**I want to run tests:**
- E2E tests require: `export KUBECONFIG=/path/to/kubeconfig`, `export RELEASE_IMAGE_LATEST=<registry>/ocp/release:latest`, `export NAMESPACE=<ci-namespace>`
- Command: `make test-e2e`
- Unit tests are included in `make test-e2e`
- CI configuration: `.ci-operator.yaml` (OpenShift CI/Prow configuration)

**I want to understand the deployment manifests:**
- **Production (OLM)**: `manifests/cluster-kube-descheduler-operator.clusterserviceversion.yaml` (CSV)
- **Production (CRD)**: `manifests/kube-descheduler-operator.crd.yaml`
- **Testing only (non-OLM)**: `deploy/` directory (numbered YAML manifests - use `ls deploy/*.yaml` to list)
  - Key files: `deploy/05_deployment.yaml` (operator deployment), `deploy/02_kube-descheduler-operator.cr.yaml` (example CR)

**I want to find where soft-tainter is deployed:**
- Soft-tainter code: `pkg/softtainter/`
- Deployment logic: `pkg/operator/target_config_reconciler.go` (search: "SoftTainter")
- Enabled by: `KubeVirtRelieveAndMigrate` profile

**I want to understand controller patterns used in this operator:**
- Uses library-go factory pattern
- Informers/listers for reads (Get/List on watched resources)
- Direct API calls for writes (Create/Update/Delete/Patch)
- `resourceapply` helpers from library-go
- See `CONTRIBUTING.md` "Controller Patterns" section

**I want to find compatibility rules between profiles:**
- CRD validation rules: `manifests/kube-descheduler-operator.crd.yaml` (search: "cannot declare")

**I want to understand how profiles translate to upstream descheduler strategies:**
- Profile-to-strategy mapping: `ARCHITECTURE.md` section "Profile → Strategy Translation"
- Implementation: `pkg/operator/target_config_reconciler.go` (each profile function)
- User documentation: `README.md` section "Profiles"
- Upstream documentation: https://github.com/kubernetes-sigs/descheduler

## Quick Commands

```bash
# Build and verify
make build
# Expected: Produces 3 binaries in current directory:
#   - ./cluster-kube-descheduler-operator
#   - ./cluster-kube-descheduler-operator-tests-ext
#   - ./soft-tainter
# Success: No compilation errors, binaries are executable

make verify
# Expected: Runs gofmt, govet, golint, and other checks
# Success: Exit code 0, no formatting/lint violations reported

# Run tests
make test-e2e
# Requires: KUBECONFIG pointing to a running OpenShift cluster
# Expected: Runs unit tests + E2E tests against the cluster
# Success: "PASS" for all test packages, exit code 0
# Failure: Check test output for failed assertions or errors

# After modifying API types in pkg/apis/descheduler/v1/
make regen-crd
# Expected: Regenerates manifests/kube-descheduler-operator.crd.yaml
# Success: CRD file updated with your API changes
# Note: Script may need updates for complex validation rules

make verify
# Expected: Verifies generated CRD is correct and committed
# Success: Exit code 0, no uncommitted changes reported
```

## Code Locations

```bash
# API types and profiles
pkg/apis/descheduler/v1/types_descheduler.go  # Search: "type DeschedulerProfile string"

# Main reconciler
pkg/operator/target_config_reconciler.go      # Search: "func NewTargetConfigReconciler"

# Soft tainting controller
pkg/softtainter/                              # Soft tainting implementation

# Deployment manifests
manifests/cluster-kube-descheduler-operator.clusterserviceversion.yaml  # OLM deployment (CSV, production)
manifests/kube-descheduler-operator.crd.yaml  # CRD manifest
deploy/05_deployment.yaml                     # Non-OLM deployment (quick testing)
deploy/02_kube-descheduler-operator.cr.yaml   # Example KubeDescheduler CR

# Embedded assets
bindata/                                      # YAML manifests embedded in binary
```

## Finding Code Elements

**Profile definitions:**
- All operator profiles are defined as variables of type `DeschedulerProfile` 
- Location: `pkg/apis/descheduler/v1/types_descheduler.go`
- Look for `var (` block with pattern: `ProfileName DeschedulerProfile = "ProfileName"`

**Reconciliation logic:**
- Main sync function: `pkg/operator/target_config_reconciler.go` (search: `func sync`)
- Controller constructors: `pkg/operator/` (search: `func New*`)

**Profile implementation:**
- Profile to strategy mappings: `pkg/operator/target_config_reconciler.go` (functions like `affinityAndTaintsProfile`, `topologyAndDuplicatesProfile`, etc.)
- Profile validation: `pkg/apis/descheduler/v1/types_descheduler.go` (validation rules and XValidation markers)

## Key File Locations

```
pkg/operator/target_config_reconciler.go  # Main reconciliation logic
pkg/operator/operatorclient/              # Wrapper for reading/updating KubeDescheduler CR (constants, helpers)
pkg/operator/testdata/                    # Test fixtures and sample data
pkg/apis/descheduler/v1/types_descheduler.go  # CRD API types
pkg/softtainter/                          # Soft tainting controller
bindata/                                  # Embedded YAML manifests
deploy/                                   # Non-OLM deployment manifests (testing only, not for production)
test/e2e/                                 # E2E tests (OTE framework)
```

## Descheduler Profiles

All profiles are defined in `pkg/apis/descheduler/v1/types_descheduler.go`

**Active profiles:**
AffinityAndTaints, TopologyAndDuplicates, SoftTopologyAndDuplicates, LifecycleAndUtilization, EvictPodsWithLocalStorage, EvictPodsWithPVC, LongLifecycle, CompactAndScale, KubeVirtRelieveAndMigrate

**Deprecated profiles (auto-migrated):**
- DevPreviewLongLifecycle → LongLifecycle (removed in 4.19+)
- DevKubeVirtRelieveAndMigrate → KubeVirtRelieveAndMigrate (removed in 4.22+)

**Comprehensive documentation:**
- **README.md** - User-facing profile descriptions and examples
- **ARCHITECTURE.md** - Profile-to-strategy mapping table

## Controller Pattern (library-go factory)

All controllers follow this pattern (verified in target_config_reconciler.go):

```go
import (
    "github.com/openshift/library-go/pkg/controller/factory"
    "github.com/openshift/library-go/pkg/operator/events"
    "github.com/openshift/library-go/pkg/operator/v1helpers"
)

func NewMyController(
    operatorClient v1helpers.OperatorClient,
    kubeInformers informers.SharedInformerFactory,
    recorder events.Recorder,
) factory.Controller {
    return factory.New().
        WithInformers(kubeInformers.Apps().V1().Deployments().Informer()).
        WithSync(c.sync).
        ToController("MyController", recorder)
}
```

## Testing on OpenShift Cluster

**Prerequisites:**
- KUBECONFIG must be configured before running oc commands
- Without KUBECONFIG: run `oc login <cluster-url>` (requires interactive authentication)

**⚠️ Commands by permission level:**

**Requires human interaction** (cannot be automated):
- `podman login quay.io` - Interactive credential prompt
- `podman push` - Requires authentication, publishes to shared registry

**AI agents can execute** (when KUBECONFIG configured):
- `oc get pods`, `oc get kubedescheduler`, `oc logs` - Read cluster state
- `oc apply -f deploy/` - Deploy/modify cluster resources (AI agents handle this)

```bash
# Step 1: Build custom image (AI agents can do this locally)
export QUAY_USER=<your_quay_username>
export IMAGE_TAG=dev-$(git rev-parse --short HEAD)
podman build -t quay.io/${QUAY_USER}/cluster-kube-descheduler-operator:${IMAGE_TAG} -f Dockerfile.rhel7 .

# Step 2: Push image to registry (requires authentication)
# User must run: podman login quay.io -u ${QUAY_USER}
podman push quay.io/${QUAY_USER}/cluster-kube-descheduler-operator:${IMAGE_TAG}

# Step 3: Update deployment to use your image
# Edit deploy/05_deployment.yaml (search: "image: quay.io/openshift/origin"):
#   Change: image: quay.io/openshift/origin-cluster-kube-descheduler-operator:latest
#   To:     image: quay.io/${QUAY_USER}/cluster-kube-descheduler-operator:${IMAGE_TAG}
# Or use sed:
sed -i "s|image: quay.io/openshift/origin-cluster-kube-descheduler-operator:latest|image: quay.io/${QUAY_USER}/cluster-kube-descheduler-operator:${IMAGE_TAG}|" deploy/05_deployment.yaml

# Step 4: Deploy to OpenShift cluster (MODIFIES CLUSTER STATE - ask permission first)
# Requires: KUBECONFIG configured or oc login <cluster-url>
oc apply -f deploy/

# Verify deployment (AI agents can run these - read-only)
oc get pods -n openshift-kube-descheduler-operator
# Expected: 1 pod named "descheduler-operator-*" with STATUS: Running
# If KubeVirtRelieveAndMigrate profile enabled: 1 additional pod named "soft-tainter-*"

oc get kubedescheduler cluster -o yaml
# Expected conditions:
#   - type: Available, status: "True"
#   - type: Progressing, status: "False"
#   - type: Degraded, status: "False"
# If Degraded: "True", check operator logs for errors

oc logs -n openshift-kube-descheduler-operator deployment/descheduler-operator --tail=50
# Check for errors or warnings in recent logs

# Check ClusterOperator status (cluster-wide status reporting)
oc get clusteroperator kube-descheduler -o yaml
# Expected .status.conditions:
#   - type: Available, status: "True", reason: "AsExpected"
#   - type: Progressing, status: "False", reason: "AsExpected"
#   - type: Degraded, status: "False"
# If Degraded=True: Check message field for specific error
```

## Common Workflows

**Add a new profile:**
1. Add constant to `pkg/apis/descheduler/v1/types_descheduler.go` (search: "var (")
   - Add to the `var (` block with other profile constants
2. Update CRD validation logic in `manifests/kube-descheduler-operator.crd.yaml`
   - Add new profile to `enum` list under `spec.profiles.items`
   - Add conflict rules if profile is incompatible with others
3. Implement profile function in `pkg/operator/target_config_reconciler.go`
   - Create function: `func <profileName>Profile(...) (*v1alpha2.DeschedulerProfile, error)`
4. Regenerate CRD manifests: `make regen-crd`
   - Expected: CRD file updated with new profile in enum
   - Verify: Check git diff to ensure changes are correct
5. Run `make verify`
   - Expected: Exit code 0, no errors
6. Update `README.md` section "Profiles" with new profile documentation
7. Test with a KubeDescheduler CR using the new profile

**Update dependencies:**
```bash
go get github.com/openshift/library-go@latest
# Expected: Updates go.mod with new version
# Check: go.mod should show updated version number

go mod tidy
# Expected: Cleans up unused dependencies in go.mod and go.sum
# Success: No "unused module" warnings

go mod vendor
# Expected: Updates vendor/ directory with new dependency code
# Success: vendor/ directory updated, no errors

make verify
# Expected: Exit code 0, build succeeds with new dependencies
# Failure: May indicate breaking API changes in updated dependencies
```

## Namespaces

```
openshift-kube-descheduler-operator  # Operator and descheduler run here
```

## Always Do

- Use informers/listers for reading resources in sync loops (Get, List operations on watched resources)
  - Write operations (Create, Update, Delete, Patch) must use direct API calls
- Regenerate CRD manifests after changing API types (typically `make regen-crd`, but may require script updates for complex changes)
- Use `resourceapply` helpers from library-go when available (e.g., ApplyDeployment, ApplyServiceAccount)
  - For CRD resources, use `ApplyKnownUnstructured` if supported (ServiceMonitor, PrometheusRule, etc.)
  - For operations without helpers (e.g., UpdateScale, UpdateStatus), use direct API calls or v1helpers with proper error handling
- Handle errors in sync() properly
  - Return transient errors (API failures, network issues) to trigger rate-limited retry
  - Wrap errors with context using `fmt.Errorf("context: %w", err)` for debugging
  - For validation errors, handle appropriately (e.g., scale down deployment, update status) before returning
- Reference code locations using search hints instead of line numbers
  - In documentation: `pkg/operator/target_config_reconciler.go  # Search: "func sync"`
  - In commit messages: "Fixed bug in target_config_reconciler.go (search: 'manageConfigMap')"
  - In issues/PRs: "See pkg/apis/descheduler/v1/types_descheduler.go (search: 'type DeschedulerProfile')"
  - Line numbers drift as code changes; search hints stay accurate

## Never Do

- Never make direct API calls for reading resources in sync loops
  - Use informers/listers for Get/List operations on watched resources
  - Write operations (Create/Update/Delete/Patch) require direct API calls (use resourceapply helpers when available)
- Never modify vendor/ directly (use go get + go mod tidy + go mod vendor)
- Never modify CRD types without regenerating manifests (typically `make regen-crd`, but verify the generated output)
- Never enable aggressive descheduling by default
  - Mode defaults to Predictive (safe simulation) - Automatic mode requires explicit user opt-in
  - Use conservative descheduling intervals (example CR uses 3600s)
  - EvictionLimits are optional - use for limiting disruption when needed
  - Avoid disruptive profiles as defaults (e.g., CompactAndScale actively consolidates/evicts workloads)

## Resources

- Upstream: https://github.com/kubernetes-sigs/descheduler
- OpenShift Docs: https://docs.openshift.com/container-platform/latest/nodes/scheduling/nodes-descheduler.html
- Library-go: https://github.com/openshift/library-go
