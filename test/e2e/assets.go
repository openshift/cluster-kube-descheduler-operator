package e2e

import (
	"embed"

	"github.com/openshift/cluster-kube-descheduler-operator/deploy"
)

//go:embed testdata/*
var testAssets embed.FS

func mustDeployAsset(name string) []byte {
	data, err := deploy.Assets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return data
}

func mustTestAsset(name string) []byte {
	data, err := testAssets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return data
}
