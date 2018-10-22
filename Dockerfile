FROM openshift/origin-release:golang-1.10
COPY . /go/src/github.com/openshift/descheduler-operator
RUN cd /go/src/github.com/openshift/descheduler-operator && go build ./cmd/descheduler-operator

FROM centos:7
COPY --from=0 /go/src/github.com/openshift/descheduler-operator/descheduler-operator /usr/bin/descheduler-operator

