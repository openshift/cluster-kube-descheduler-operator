# !/bin/sh

if [ -z "${QUAY_USER}" ]; then
  echo "QUAY_USER env not set"
  exit 1
fi

BUNDLE_IMAGE=$(brew list-builds --package=kube-descheduler-operator-bundle-container --quiet | tail -1 | cut -d' ' -f1)

echo "Found ${BUNDLE_IMAGE}"

IMAGE_PULL=$(brew --noauth call --json getBuild ${BUNDLE_IMAGE} |jq -r '.extra.image.index.pull[0]')

echo "Found ${IMAGE_PULL}"

IMAGE_TAG=$(date +"%d-%m-%Y-%H-%M-%S")

# opm binary from https://github.com/operator-framework/operator-registry/releases
opm index add --bundles ${IMAGE_PULL} --tag quay.io/${QUAY_USER}/kube-descheduler-operator-index:${IMAGE_TAG}
podman push quay.io/${QUAY_USER}/kube-descheduler-operator-index:${IMAGE_TAG}

cat << EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: kube-descheduler-operator
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: quay.io/${QUAY_USER}/kube-descheduler-operator-index:${IMAGE_TAG}
EOF

watch -n 1 oc get catalogsource -A
