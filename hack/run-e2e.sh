set -o errexit
set -o nounset
set -o pipefail

DESCHEDULER_OPERATOR_ROOT=$(dirname "${BASH_SOURCE}")/..

# Get operator-sdk binary.
# TODO: Make this to download the same version we have in godeps
wget -O /tmp/operator-sdk https://github.com/operator-framework/operator-sdk/releases/download/v0.2.0/operator-sdk-v0.2.0-x86_64-linux-gnu && chmod +x /tmp/operator-sdk
wget -O /tmp/kubectl https://storage.googleapis.com/kubernetes-release/release/v1.11.3/bin/linux/amd64/kubectl && chmod +x /tmp/kubectl 
cd $DESCHEDULER_OPERATOR_ROOT
/tmp/kubectl create -f deploy/namespace.yaml 
/tmp/operator-sdk test local ./test/e2e --up-local --namespace=openshift-descheduler-operator
/tmp/kubectl delete -f deploy/namespace.yaml 
