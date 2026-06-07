package e2e

import (
	"testing"

	o "github.com/onsi/gomega"
)

// NOTE: This test is also available in the OTE framework (test/e2e/operator.go).
// This dual implementation allows tests to run both as standard Go tests (via go test)
// and through the Ginkgo/OTE framework (for OpenShift CI integration).
//
// The actual test logic is in operator.go's standalone functions, which are called
// by both this standard Go test and the Ginkgo specs.

// TestExtended runs the operator tests using standard Go testing.
func TestExtended(t *testing.T) {
	// Register Gomega with the testing framework for standard Go test mode
	o.RegisterTestingT(t)

	t.Run("Descheduler Operator", func(t *testing.T) {
		// Setup operator and wait for it to be ready
		ctx, cancelFnc, kubeClient, err := setupOperator(t)
		if err != nil {
			t.Fatalf("Failed to setup operator: %v", err)
		}
		defer cancelFnc()

		t.Run("Deploying soft tainter controller", func(t *testing.T) {
			testSoftTainterController(t, ctx, kubeClient)
		})

		t.Run("Deploying soft tainter controller with Validating Admission Policy", func(t *testing.T) {
			testSoftTainterControllerWithVAP(t, ctx, kubeClient)
		})

		t.Run("Descheduling a pod", func(t *testing.T) {
			testPodDescheduling(t, ctx, kubeClient)
		})

		t.Run("Metrics service exists", func(t *testing.T) {
			testMetricsService(t, ctx, kubeClient)
		})

		t.Run("ServiceMonitor exists", func(t *testing.T) {
			testServiceMonitor(t, ctx, kubeClient)
		})

		t.Run("Prometheus target is up", func(t *testing.T) {
			testPrometheusTarget(t, ctx, kubeClient)
		})

		t.Run("Metrics data available", func(t *testing.T) {
			testMetricsData(t, ctx, kubeClient)
		})
	})
}
