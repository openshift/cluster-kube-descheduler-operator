set -o errexit
set -o nounset
set -o pipefail

DESCHEDULER_OPERATOR_ROOT=$(dirname "${BASH_SOURCE}")/..

# Get operator-sdk binary.
# TODO: Make this to download the same version we have in godeps
wget -O /tmp/operator-sdk https://github.com/operator-framework/operator-sdk/releases/download/v0.2.0/operator-sdk-v0.2.0-x86_64-linux-gnu
chmod +x /tmp/operator-sdk
cd $DESCHEDULER_OPERATOR_ROOT
kubectl create -f deploy/namespace.yaml 
/tmp/operator-sdk test local ./test/e2e --up-local --namespace=openshift-descheduler-operator
kubectl delete -f deploy/namespace.yaml --kubeconfig /tmp/admin.kubeconfig
