# Testing Guide - Cluster Kube Descheduler Operator

This guide covers testing for the Cluster Kube Descheduler Operator.

## Quick Links

- **OTE Integration Guide:** [openshift-tests-extension/docs/INTEGRATION_GUIDE.md](https://github.com/openshift-eng/openshift-tests-extension/blob/main/docs/INTEGRATION_GUIDE.md)
- **OTE Framework:** https://github.com/openshift-eng/openshift-tests-extension
- **Contributing Guide:** [CONTRIBUTING.md](./CONTRIBUTING.md)

---

## Overview

This repository uses the [OpenShift Tests Extension (OTE)](https://github.com/openshift-eng/openshift-tests-extension) framework for end-to-end testing.

**Test Binary:** `cluster-kube-descheduler-operator-tests-ext`

**Test Implementation:** [cmd/cluster-kube-descheduler-operator-tests-ext/main.go](./cmd/cluster-kube-descheduler-operator-tests-ext/main.go)

---

## Prerequisites

- **Go 1.25+** (check [go.mod](./go.mod) for exact version)
- **OpenShift cluster** (4.12+)
- **oc CLI** installed and configured
- **KUBECONFIG** environment variable set

**Quick environment check:**

```bash
# Verify prerequisites
go version                    # Should be 1.25+
oc whoami                     # Should connect to cluster
oc auth can-i create namespaces  # Should return "yes"
```

---

## Quick Start

### Build Test Binary

```bash
# Build all binaries (operator + test binary)
make build
```

This produces:
- `./cluster-kube-descheduler-operator` - Operator binary
- `./cluster-kube-descheduler-operator-tests-ext` - OTE test binary
- `./soft-tainter` - Soft tainter binary

### Run All Tests

```bash
# Set KUBECONFIG first
export KUBECONFIG=/path/to/kubeconfig

# Run all tests
./cluster-kube-descheduler-operator-tests-ext run-suite openshift/cluster-kube-descheduler-operator/all
```

### List Available Tests

```bash
# List all test suites
./cluster-kube-descheduler-operator-tests-ext list suites

# List tests in serial suite
./cluster-kube-descheduler-operator-tests-ext list tests \
  --suite=openshift/cluster-kube-descheduler-operator/operator/serial
```

---

## Test Suites

This operator includes the following test suites:

### Serial Operator Tests

**Suite:** `openshift/cluster-kube-descheduler-operator/operator/serial`

Tests that must run serially (one at a time) due to cluster-wide operator modifications.

**Run:**
```bash
./cluster-kube-descheduler-operator-tests-ext run-suite \
  openshift/cluster-kube-descheduler-operator/operator/serial -c 1
```

**Tests included:**
- Operator deployment and lifecycle tests
- KubeDescheduler CR creation and updates
- Profile configuration tests
- Mode switching (Automatic/Predictive)
- Operator status conditions

**Why serial?**  
These tests modify the cluster-wide `KubeDescheduler` CR and operator state, so they must run one at a time.

### All Tests

**Suite:** `openshift/cluster-kube-descheduler-operator/all`

Runs all available test suites.

**Run:**
```bash
./cluster-kube-descheduler-operator-tests-ext run-suite \
  openshift/cluster-kube-descheduler-operator/all
```

---

## Running Tests

### Basic Test Execution

```bash
# Run specific suite
./cluster-kube-descheduler-operator-tests-ext run-suite \
  openshift/cluster-kube-descheduler-operator/operator/serial

# Run specific test by name
./cluster-kube-descheduler-operator-tests-ext run-test \
  "should create descheduler deployment"
```

### Serial Execution

For serial test suites (prevents parallel execution):

```bash
# Force serial execution with -c 1
./cluster-kube-descheduler-operator-tests-ext run-suite \
  openshift/cluster-kube-descheduler-operator/operator/serial -c 1
```

### JUnit XML Output

Generate JUnit XML reports for CI systems:

```bash
# Run with JUnit output
./cluster-kube-descheduler-operator-tests-ext run-suite \
  openshift/cluster-kube-descheduler-operator/all \
  --junit-path=/tmp/junit.xml

# Verify output
cat /tmp/junit.xml
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `KUBECONFIG` | Path to kubeconfig file | Required |
| `NAMESPACE` | Operator namespace | `openshift-kube-descheduler-operator` |
| `ARTIFACT_DIR` | Directory for test artifacts | `/tmp/artifacts` |

**Example:**
```bash
KUBECONFIG=/path/to/kubeconfig \
ARTIFACT_DIR=/tmp/test-artifacts \
  ./cluster-kube-descheduler-operator-tests-ext run-suite \
    openshift/cluster-kube-descheduler-operator/all
```

---

## Writing Tests

### Test Location

All E2E tests are in:
```
test/e2e/
├── operator.go          # Main operator tests
├── operator_test.go     # Test suite setup
└── helpers.go           # Test helper functions
```

### Test Structure

Tests follow Ginkgo v2 conventions:

```go
package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[Operator][Serial] Feature Name", func() {
	It("should behave correctly", func() {
		// Arrange
		// Act
		// Assert
	})
})
```

**Test naming convention:**
- Use `[Operator]` prefix for operator-specific tests
- Use `[Serial]` for tests that modify cluster state
- Use descriptive, stable names (no timestamps/UUIDs)

### Example Test

```go
var _ = Describe("[Operator][Serial] KubeDescheduler CR", func() {
	It("should create descheduler deployment when CR is created", func() {
		// Create KubeDescheduler CR
		cr := &operatorv1.KubeDescheduler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster",
				Namespace: operatorNamespace,
			},
			Spec: operatorv1.KubeDeschedulerSpec{
				Mode: operatorv1.Automatic,
				Profiles: []operatorv1.DeschedulerProfile{
					operatorv1.AffinityAndTaints,
				},
			},
		}
		Expect(client.Create(context.TODO(), cr)).To(Succeed())

		// Wait for deployment
		Eventually(func() bool {
			deployment := &appsv1.Deployment{}
			err := client.Get(context.TODO(), 
				types.NamespacedName{
					Name:      "descheduler",
					Namespace: operatorNamespace,
				}, deployment)
			return err == nil && deployment.Status.ReadyReplicas > 0
		}).WithTimeout(5 * time.Minute).
		  WithPolling(10 * time.Second).
		  Should(BeTrue())
	})
})
```

### Test Helpers

Common helpers are in [test/e2e/helpers.go](./test/e2e/helpers.go):

```go
// Get operator namespace
namespace := getOperatorNamespace()

// Wait for deployment ready
waitForDeploymentReady(deploymentName, namespace)

// Get KubeDescheduler CR
cr := getKubeDeschedulerCR()

// Update CR spec
updateKubeDeschedulerSpec(func(spec *operatorv1.KubeDeschedulerSpec) {
	spec.Mode = operatorv1.Automatic
})
```

---

## Component-Specific Testing

### Testing Descheduler Profiles

For complete descheduler profile descriptions and available profiles, see **[README.md - Profiles](./README.md#profiles)**.

Test profile configurations:

```go
It("should enable AffinityAndTaints profile", func() {
	// Update CR with profile
	updateKubeDeschedulerSpec(func(spec *operatorv1.KubeDeschedulerSpec) {
		spec.Profiles = []operatorv1.DeschedulerProfile{
			operatorv1.AffinityAndTaints,
		}
	})

	// Wait for descheduler configmap update
	Eventually(func() bool {
		cm := &corev1.ConfigMap{}
		err := client.Get(context.TODO(),
			types.NamespacedName{
				Name:      "descheduler-policy",
				Namespace: operatorNamespace,
			}, cm)
		if err != nil {
			return false
		}
		
		// Verify policy contains expected strategies
		policy := cm.Data["policy.yaml"]
		return strings.Contains(policy, "RemovePodsViolatingNodeAffinity")
	}).WithTimeout(2 * time.Minute).Should(BeTrue())
})
```

### Testing Descheduling Modes

Test mode switching (Automatic vs Predictive):

```go
It("should switch from Predictive to Automatic mode", func() {
	// Set Predictive mode
	updateKubeDeschedulerSpec(func(spec *operatorv1.KubeDeschedulerSpec) {
		spec.Mode = operatorv1.Predictive
	})
	
	// Wait for mode update
	waitForDeschedulerMode(operatorv1.Predictive)

	// Switch to Automatic
	updateKubeDeschedulerSpec(func(spec *operatorv1.KubeDeschedulerSpec) {
		spec.Mode = operatorv1.Automatic
	})
	
	waitForDeschedulerMode(operatorv1.Automatic)
})
```

### Testing Soft Tainter (KubeVirtRelieveAndMigrate)

Test soft-tainter deployment:

```go
It("should deploy soft-tainter when KubeVirtRelieveAndMigrate profile enabled", func() {
	// Enable profile
	updateKubeDeschedulerSpec(func(spec *operatorv1.KubeDeschedulerSpec) {
		spec.Profiles = []operatorv1.DeschedulerProfile{
			operatorv1.KubeVirtRelieveAndMigrate,
		}
	})

	// Wait for soft-tainter deployment
	Eventually(func() bool {
		deployment := &appsv1.Deployment{}
		err := client.Get(context.TODO(),
			types.NamespacedName{
				Name:      "soft-tainter",
				Namespace: operatorNamespace,
			}, deployment)
		return err == nil && deployment.Status.ReadyReplicas > 0
	}).WithTimeout(3 * time.Minute).Should(BeTrue())
})
```

---

## Debugging Tests

### Common Issues

**Issue: Tests fail with "connection refused"**

```bash
# Check KUBECONFIG is set
echo $KUBECONFIG

# Verify cluster access
oc whoami
oc get nodes
```

**Issue: Tests timeout waiting for operator**

```bash
# Check operator pod logs
oc logs -n openshift-kube-descheduler-operator \
  deployment/descheduler-operator --tail=50

# Check operator status
oc get clusteroperator kube-descheduler -o yaml
```

**Issue: Test reports "no matches for kind KubeDescheduler"**

```bash
# Verify CRD is installed
oc get crd kubedeschedulers.operator.openshift.io

# If missing, deploy the operator first
oc apply -f deploy/
```

### Viewing Logs

**Operator logs:**
```bash
# Tail operator logs
oc logs -n openshift-kube-descheduler-operator \
  deployment/descheduler-operator -f

# Previous container logs (if crashed)
oc logs -n openshift-kube-descheduler-operator \
  deployment/descheduler-operator -p
```

**Descheduler logs:**
```bash
# Check descheduler pod logs
oc logs -n openshift-kube-descheduler-operator \
  deployment/descheduler -f
```

**Soft-tainter logs:**
```bash
# Check soft-tainter logs (if KubeVirtRelieveAndMigrate enabled)
oc logs -n openshift-kube-descheduler-operator \
  deployment/soft-tainter -f
```

### Test Artifacts

Collect test artifacts when tests fail:

```bash
# Run with artifact collection
ARTIFACT_DIR=/tmp/test-artifacts \
  ./cluster-kube-descheduler-operator-tests-ext run-suite \
    openshift/cluster-kube-descheduler-operator/all

# Check collected artifacts
ls -la /tmp/test-artifacts/
```

### Must-Gather

For comprehensive cluster state collection:

```bash
# Collect must-gather
oc adm must-gather --dest-dir=/tmp/must-gather

# Check operator-specific resources
ls /tmp/must-gather/*/namespaces/openshift-kube-descheduler-operator/
```

---

## CI Integration

### Prow Job Configuration

This repository's CI jobs are configured in:
- **Config:** `https://github.com/openshift/release/tree/master/ci-operator/config/openshift/cluster-kube-descheduler-operator`
- **Jobs:** `https://github.com/openshift/release/tree/master/ci-operator/jobs/openshift/cluster-kube-descheduler-operator`

### Running Tests in CI

The Makefile target `test-e2e` is used by CI:

```makefile
test-e2e:
	./cluster-kube-descheduler-operator-tests-ext run-suite \
	  openshift/cluster-kube-descheduler-operator/all \
	  --junit-path=${ARTIFACT_DIR}/junit.xml
```

### Debugging CI Failures

**Step 1: Check test output**
- Click "Details" on failed check
- Search for `FAIL:` or test name in build log

**Step 2: Download artifacts**
- Prow provides artifact download links
- Look for `junit.xml`, logs, must-gather

**Step 3: Reproduce locally**
```bash
# Use same OCP version as CI
# Check Prow job config for environment variables
```

**Step 4: Retrigger tests**
```bash
# Comment on PR to retrigger all tests
/retest

# Retrigger only required tests
/retest-required
```

---

## Contributing Tests

### Test Coverage Expectations

- ✅ **New descheduler profiles** must include E2E tests
- ✅ **Mode changes** (Automatic/Predictive) must be tested
- ✅ **Major features** should have comprehensive test coverage
- ✅ **Bug fixes** should include regression tests

### Test Review Process

1. **Write tests** following conventions in this guide
2. **Run locally** to verify tests pass
3. **Create PR** with test changes
4. **Address review feedback**
5. **Verify CI passes** all test suites

See [CONTRIBUTING.md - Testing Guidelines](./CONTRIBUTING.md#testing-guidelines) for detailed review expectations.

---

## Additional Resources

### Documentation
- **OTE Integration Guide:** https://github.com/openshift-eng/openshift-tests-extension/blob/main/docs/INTEGRATION_GUIDE.md
- **Contributing Guide:** [CONTRIBUTING.md](./CONTRIBUTING.md)
- **Architecture:** [ARCHITECTURE.md](./ARCHITECTURE.md)
- **Agent Guide:** [AGENTS.md](./AGENTS.md)

### External Resources
- **OTE Framework:** https://github.com/openshift-eng/openshift-tests-extension
- **Ginkgo v2 Docs:** https://onsi.github.io/ginkgo/
- **OpenShift CI:** https://docs.ci.openshift.org/

---

## Getting Help

- **Issues:** https://github.com/openshift/cluster-kube-descheduler-operator/issues
- **Slack:** [#forum-ocp-workloads](https://redhat.enterprise.slack.com/archives/CKJR6200N) (Red Hat employees)
- **Team Slack:** [#control-plane](https://redhat.enterprise.slack.com/archives/CC3CZCQHM) (Red Hat employees)
