package deploy

import "embed"

// Assets embeds the deploy/ YAML manifests so e2e tests can apply the same
// files used for local installation. The OTE test binary has no repo checkout.
//
// Tests apply a named subset of these files via resourceapply. Do not apply
// 02_kube-descheduler-operator.cr.yaml from e2e; that sample CR uses a
// different profile and interval than the test fixtures in testdata/.
//
//go:embed *.yaml
var Assets embed.FS
