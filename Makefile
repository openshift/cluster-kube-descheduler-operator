.PHONY: verify-gofmt test-unit clean build image test-e2e

IMAGE_REPOSITORY_NAME ?= openshift

build:
	go build -o cluster-kube-descheduler-operator github.com/openshift/cluster-kube-descheduler-operator/cmd/manager

image:
	imagebuilder -f Dockerfile -t $(IMAGE_REPOSITORY_NAME)/cluster-kube-descheduler-operator .

clean:
	rm -rf cluster-kube-descheduler-operator 

test-unit:
	go test -race -v github.com/openshift/cluster-kube-descheduler-operator/pkg/controller/descheduler

verify-gofmt:
	hack/verify-gofmt.sh

test-e2e:
	hack/run-e2e.sh

