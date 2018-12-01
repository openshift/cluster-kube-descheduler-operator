.PHONY: verify-gofmt test-unit clean build image test-e2e

IMAGE_REPOSITORY_NAME ?= openshift

build:
	go build -o descheduler-operator github.com/openshift/descheduler-operator/cmd/manager

image:
	imagebuilder -f Dockerfile -t $(IMAGE_REPOSITORY_NAME)/descheduler-operator .

clean:
	rm -rf descheduler-operator 

test-unit:
	go test -v github.com/openshift/descheduler-operator/pkg/controller/descheduler

verify-gofmt:
	hack/verify-gofmt.sh

test-e2e:
	hack/run-e2e.sh

