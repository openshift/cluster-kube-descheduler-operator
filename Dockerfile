FROM registry.svc.ci.openshift.org/openshift/release:golang-1.14 AS builder
WORKDIR /go/src/github.com/openshift/cluster-kube-descheduler-operator
COPY . .
# image-references file is not recognized by OLM
RUN rm manifests/4*/image-references
RUN make build --warn-undefined-variables

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/cluster-kube-descheduler-operator /usr/bin/
# Upstream bundle and index images does not support versioning so
# we need to copy a specific version under /manifests layout directly
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/manifests/4.4/* /manifests/
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/metadata /metadata

LABEL io.k8s.display-name="OpenShift Descheduler Operator" \
      io.k8s.description="This is a component of OpenShift and manages the descheduler" \
      io.openshift.tags="openshift,cluster-kube-descheduler-operator" \
      com.redhat.delivery.appregistry=true \
      maintainer="AOS workloads team, <aos-workloads@redhat.com>"
