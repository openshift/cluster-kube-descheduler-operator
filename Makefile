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

test-e2e: GO_TEST_PACKAGES :=./test/e2e
test-e2e: GO_TEST_ARGS :=-v
test-e2e: test-unit
.PHONY: test-e2e

regen-crd:
	go build -o _output/tools/bin/controller-gen ./vendor/sigs.k8s.io/controller-tools/cmd/controller-gen
	cp manifests/kube-descheduler-operator.crd.yaml manifests/operator.openshift.io_kubedeschedulers.yaml
	for i in $$(seq 1 50); do \
		echo "Attempt $$i..."; \
		if ./_output/tools/bin/controller-gen crd paths=./pkg/apis/descheduler/v1/... schemapatch:manifests=./manifests output:crd:dir=./manifests; then \
			break; \
		fi; \
		if [ $$i -eq 50 ]; then \
			echo "Failed after 50 attempts"; \
			exit 1; \
		fi; \
	done
	mv manifests/operator.openshift.io_kubedeschedulers.yaml manifests/kube-descheduler-operator.crd.yaml
	# Remove leading --- from CRD file
	sed -i '1{/^---$$/d;}' manifests/kube-descheduler-operator.crd.yaml
	# Remove .annotations to drop controller-gen.kubebuilder.io/version as the only key set
	yq eval 'del(.metadata.annotations)' -i manifests/kube-descheduler-operator.crd.yaml
	cp manifests/kube-descheduler-operator.crd.yaml test/e2e/bindata/assets/00_kube-descheduler-operator-crd.yaml

generate: update-codegen-crds generate-clients
.PHONY: generate

generate-clients:
	GO=GO111MODULE=on GOFLAGS=-mod=readonly hack/update-codegen.sh
.PHONY: generate-clients

clean:
	$(RM) ./cluster-kube-descheduler-operator
.PHONY: clean
