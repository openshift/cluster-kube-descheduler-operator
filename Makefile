all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
  targets/openshift/bindata.mk \
	targets/openshift/images.mk \
	targets/openshift/codegen.mk \
	targets/openshift/deps.mk \
	targets/openshift/crd-schema-gen.mk \
)

# Exclude e2e tests from unit testing
GO_TEST_PACKAGES :=./pkg/... ./cmd/...

IMAGE_REGISTRY :=registry.svc.ci.openshift.org

CODEGEN_OUTPUT_PACKAGE :=github.com/openshift/cluster-kube-descheduler-operator/pkg/generated
CODEGEN_API_PACKAGE :=github.com/openshift/cluster-kube-descheduler-operator/pkg/apis
CODEGEN_GROUPS_VERSION :=descheduler:v1beta1

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target name
# $2 - image ref
# $3 - Dockerfile path
# $4 - context directory for image build
$(call build-image,ocp-cluster-kube-descheduler-operator,$(IMAGE_REGISTRY)/ocp/4.4:cluster-kube-descheduler-operator, ./Dockerfile.rhel7,.)

# This will call a macro called "add-bindata" which will generate bindata specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - input dirs
# $3 - prefix
# $4 - pkg
# $5 - output
# It will generate targets {update,verify}-bindata-$(1) logically grouping them in unsuffixed versions of these targets
# and also hooked into {update,verify}-generated for broader integration.
$(call add-bindata,v4.1.0,./bindata/v4.1.0/...,bindata,v410_00_assets,pkg/operator/v410_00_assets/bindata.go)

$(call add-crd-gen,descheduler,./pkg/apis/descheduler/v1beta1,./manifests/4.7,./manifests/4.7)

test-e2e: GO_TEST_PACKAGES :=./test/e2e
test-e2e: test-unit
.PHONY: test-e2e

generate: update-codegen-crds generate-clients
.PHONY: generate

generate-clients:
	bash ./vendor/k8s.io/code-generator/generate-groups.sh all github.com/openshift/cluster-kube-descheduler-operator/pkg/generated github.com/openshift/cluster-kube-descheduler-operator/pkg/apis descheduler:v1beta1
.PHONY: generate-clients

clean:
	$(RM) ./cluster-kube-descheduler-operator
.PHONY: clean

update-codegen-crds-descheduler-prerelease:
	go run k8s.io/code-generator/cmd/prerelease-lifecycle-gen --go-header-file $(shell mktemp) --input-dirs ./pkg/apis/descheduler/v1beta1 --output-base ./

update-codegen-crds-descheduler: update-codegen-crds-descheduler-prerelease

verify-codegen-crds-descheduler-prerelease:
	go run k8s.io/code-generator/cmd/prerelease-lifecycle-gen --go-header-file $(shell mktemp) --input-dirs ./pkg/apis/descheduler/v1beta1 --output-base ./ --verify-only

verify-codegen-crds-descheduler: verify-codegen-crds-descheduler-prerelease
