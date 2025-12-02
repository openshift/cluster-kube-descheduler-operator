FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.24 as builder
WORKDIR /go/src/github.com/openshift/cluster-kube-descheduler-operator
COPY . .
RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:34880b64c07f28f64d95737f82f891516de9a3b43583f39970f7bf8e4cfa48b7
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/cluster-kube-descheduler-operator /usr/bin/
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/soft-tainter /usr/bin/
RUN mkdir /licenses
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/LICENSE /licenses/.

LABEL io.k8s.display-name="Kube Descheduler Operator based on RHEL 9" \
      io.k8s.description="This is a component of OpenShift and manages the Descheduler based on RHEL 9" \
      distribution-scope="public" \
      com.redhat.component="kube-descheduler-operator-container" \
      name="kube-descheduler-operator/kube-descheduler-rhel9-operator" \
      cpe="cpe:/a:redhat:kube_descheduler_operator:5.3::el9" \
      release="5.3.2" \
      version="5.3.2" \
      url="https://github.com/openshift/cluster-kube-descheduler-operator" \
      vendor="Red Hat, Inc." \
      summary="kube-descheduler-operator" \
      io.openshift.expose-services="" \
      io.openshift.tags="openshift,kube-descheduler-operator" \
      description="kube-descheduler-operator-container" \
      distribution-scope=public \
      maintainer="AOS workloads team, <aos-workloads@redhat.com>"

USER nobody
