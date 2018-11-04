FROM openshift/origin-release:golang-1.10
COPY . /go/src/github.com/openshift/descheduler-operator
RUN cd /go/src/github.com/openshift/descheduler-operator && go build -o descheduler-operator ./cmd/manager

FROM centos:7
COPY --from=0 /go/src/github.com/openshift/descheduler-operator/descheduler-operator /usr/bin/descheduler-operator

LABEL io.k8s.display-name="OpenShift Descheduler Operator" \
      io.k8s.description="This is a component of OpenShift Container Platform and manages the OpenShift descheduler" \
      io.openshift.tags="openshift,descheduler-operator" \
      maintainer="AOS pod team, <aos-pod@redhat.com>"

