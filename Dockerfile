FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.21 as builder
WORKDIR /go/src/github.com/openshift/cluster-kube-descheduler-operator
COPY . .
RUN make build --warn-undefined-variables

FROM registry.redhat.io/rhel9-2-els/rhel-minimal:9.2
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/cluster-kube-descheduler-operator /usr/bin/
RUN mkdir /licenses
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/LICENSE /licenses/.

LABEL io.k8s.display-name="Kube Descheduler Operator based on RHEL 9" \
      io.k8s.description="This is a component of OpenShift and manages the Descheduler based on RHEL 9" \
      com.redhat.component="kube-descheduler-operator-container" \
      name="kube-descheduler-operator-rhel-9" \
      cpe="cpe:/a:redhat:kube_descheduler_operator:5.0::el9" \
      version="${CI_CONTAINER_VERSION}" \
      summary="kube-descheduler-operator" \
      io.openshift.expose-services="" \
      io.openshift.tags="openshift,kube-descheduler-operator" \
      description="kube-descheduler-operator-container" \
      maintainer="AOS workloads team, <aos-workloads@redhat.com>"

USER nobody
