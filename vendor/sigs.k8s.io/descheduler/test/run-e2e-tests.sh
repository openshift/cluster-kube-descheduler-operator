#!/usr/bin/env bash

# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -x
set -o errexit
set -o nounset

REGISTRY=$(echo ${RELEASE_IMAGE_LATEST} | cut -d"/" -f1)
DESCHEDULER_IMAGE="${REGISTRY}/${NAMESPACE}/pipeline:descheduler"

# Deploy rbac, sa and binding for a descheduler running through a deployment
oc apply -f kubernetes/base/rbac.yaml

collect_logs() {
  echo "Collecting pods and logs"
  kubectl get pods -n default

  for pod in $(kubectl get pods -n default -o name); do
    echo "Logs for ${pod}"
    kubectl logs -n default ${pod}
  done
}

trap "collect_logs" ERR

PRJ_PREFIX="sigs.k8s.io/descheduler"
# Skip tests that are currently not supported
TESTS=$(go test ${PRJ_PREFIX}/test/e2e -list '.' --args --descheduler-image ${DESCHEDULER_IMAGE} | grep -vE "TestLowNodeUtilizationKubernetesMetrics|TestLiveMigrationInBackground" | grep Test | sed -z 's/\n\(.\)/$|^\1/g')
TESTS="^${TESTS}\$"
go test ./test/e2e/ -v -timeout 0 -run "${TESTS}" --args --descheduler-image ${DESCHEDULER_IMAGE}
