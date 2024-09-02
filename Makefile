all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/images.mk \
	targets/openshift/codegen.mk \
	targets/openshift/deps.mk \
	targets/openshift/crd-schema-gen.mk \
	targets/openshift/bindata.mk \
)

# Exclude e2e tests from unit testing
GO_TEST_PACKAGES :=./pkg/... ./cmd/...
GO_BUILD_FLAGS :=-tags strictfipsruntime

IMAGE_REGISTRY :=registry.svc.ci.openshift.org

CODEGEN_OUTPUT_PACKAGE :=github.com/openshift/cluster-kube-descheduler-operator/pkg/generated
CODEGEN_API_PACKAGE :=github.com/openshift/cluster-kube-descheduler-operator/pkg/apis
CODEGEN_GROUPS_VERSION :=descheduler:v1

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target name
# $2 - image ref
# $3 - Dockerfile path
# $4 - context directory for image build
$(call build-image,ocp-cluster-kube-descheduler-operator,$(IMAGE_REGISTRY)/ocp/4.4:cluster-kube-descheduler-operator, ./Dockerfile.rhel7,.)

$(call verify-golang-versions,Dockerfile.rhel7)

$(call add-crd-gen,descheduler,./pkg/apis/descheduler/v1,./manifests,./manifests)

# This will call a macro called "add-bindata" which will generate bindata specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - input dirs
# $3 - prefix
# $4 - pkg
# $5 - output
# It will generate targets {update,verify}-bindata-$(1) logically grouping them in unsuffixed versions of these targets
# and also hooked into {update,verify}-generated for broader integration.
$(call add-bindata,operator,./pkg/operator/testdata/...,,operator,pkg/operator/bindata.go)

test-e2e: GO_TEST_PACKAGES :=./test/e2e
test-e2e: GO_TEST_ARGS :=-v
test-e2e: test-unit
.PHONY: test-e2e

regen-crd:
	go build -o _output/tools/bin/controller-gen ./vendor/sigs.k8s.io/controller-tools/cmd/controller-gen
	cp manifests/kube-descheduler-operator.crd.yaml manifests/operator.openshift.io_kubedeschedulers.yaml
	./_output/tools/bin/controller-gen crd paths=./pkg/apis/descheduler/v1/... schemapatch:manifests=./manifests output:crd:dir=./manifests
	mv manifests/operator.openshift.io_kubedeschedulers.yaml manifests/kube-descheduler-operator.crd.yaml

generate: update-codegen-crds generate-clients
.PHONY: generate

generate-clients:
	bash ./vendor/k8s.io/code-generator/generate-groups.sh all github.com/openshift/cluster-kube-descheduler-operator/pkg/generated github.com/openshift/cluster-kube-descheduler-operator/pkg/apis descheduler:v1 --go-header-file=./hack/boilerplate.go.txt
.PHONY: generate-clients

clean:
	$(RM) ./cluster-kube-descheduler-operator
.PHONY: clean
