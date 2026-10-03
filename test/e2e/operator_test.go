package e2e

import (
	"testing"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	o.RegisterFailHandler(g.Fail)
	g.RunSpecs(t, "Cluster Kube Descheduler Operator E2E Suite")
}
