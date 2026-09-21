FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.20 as builder
WORKDIR /go/src/github.com/openshift/cluster-kube-descheduler-operator
COPY . .

RUN mkdir licenses
COPY ./LICENSE licenses/.

ARG OPERATOR_IMAGE=registry.redhat.io/kube-descheduler-operator/kube-descheduler-rhel9-operator@sha256:5f44557255d2d94d21019a3d26a22df89f652ebcda3123f9b20833d108011c2a
# artificial distance to avoid rebase conflicts when the operand and the operator image gets updated at the same time
#
#
#
#
#
#
#
ARG OPERAND_IMAGE=registry.redhat.io/kube-descheduler-operator/descheduler-rhel9@sha256:7b59e695bdbf0b5a68199bd4d1a331a27a155bb50c4da4c86efbdb793ae720a4
ARG REPLACED_OPERATOR_IMG=registry-proxy.engineering.redhat.com/rh-osbs/kube-descheduler-operator-rhel-9:latest
ARG REPLACED_OPERAND_IMG=registry-proxy.engineering.redhat.com/rh-osbs/descheduler-rhel-9:latest

RUN hack/replace-image.sh manifests ${REPLACED_OPERATOR_IMG} ${OPERATOR_IMAGE}
RUN hack/replace-image.sh manifests ${REPLACED_OPERAND_IMG} ${OPERAND_IMAGE}
RUN sed -i "s/createdAt: \".*\"/createdAt: \"$(date -I)\"/" manifests/cluster-kube-descheduler-operator.clusterserviceversion.yaml

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:7b8e25a1b56ca4d00219198f3b5b51a3e1693a5c4f5369c5e190d7d6cb3f980e

COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/manifests /manifests
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/metadata /metadata
COPY --from=builder /go/src/github.com/openshift/cluster-kube-descheduler-operator/licenses /licenses

LABEL operators.operatorframework.io.bundle.mediatype.v1="registry+v1"
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1="cluster-kube-descheduler-operator"
LABEL operators.operatorframework.io.bundle.channels.v1=stable
LABEL operators.operatorframework.io.bundle.channel.default.v1=stable
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.34.2
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v4

LABEL com.redhat.component="kube-descheduler-operator-bundle-container"
LABEL description="Descheduler support for OpenShift based on RHEL 9"
LABEL distribution-scope="public"
LABEL name="kube-descheduler-operator/kube-descheduler-operator-bundle"
LABEL cpe="cpe:/a:redhat:kube_descheduler_operator:4.14::el9"
LABEL release="4.14.4"
LABEL version="4.14.4"
LABEL url="https://github.com/openshift/cluster-kube-descheduler-operator"
LABEL vendor="Red Hat, Inc."
LABEL summary="Descheduler support for OpenShift"
LABEL io.openshift.expose-services=""
LABEL io.k8s.display-name="kube-descheduler-operator based on RHEL 9"
LABEL io.k8s.description="Descheduler support for OpenShift based on RHEL 9"
LABEL io.openshift.tags="openshift,kube-descheduler-operator"
LABEL com.redhat.delivery.operator.bundle=true
LABEL com.redhat.openshift.versions="v4.14"
LABEL com.redhat.delivery.appregistry=true
LABEL distribution-scope=public
LABEL maintainer="AOS workloads team, <aos-workloads-staff@redhat.com>"

USER 1001
