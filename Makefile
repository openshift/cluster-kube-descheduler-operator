GOFLAGS :=
DOCKER_ORG ?= $(USER)

all build:
	go build $(GOFLAGS) ./cmd/descheduler-operator
.PHONY: all build

images:
	imagebuilder -f Dockerfile -t openshift/origin-descheduler-operator .
.PHONY: images

clean:
	$(RM) ./descheduler-operator
.PHONY: clean
