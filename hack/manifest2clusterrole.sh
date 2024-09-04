#!/usr/bin/env bash

# Copyright 2024 The Kubernetes Authors.
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

set -o errexit
set -o nounset
set -o pipefail

CURRENT_DIR=$(dirname "${BASH_SOURCE}")

cat << EOF > deploy/02_clusterrole.yaml
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: descheduler-operator
rules:
EOF

cat ${CURRENT_DIR}/../manifests/cluster-kube-descheduler-operator.clusterserviceversion.yaml | yq -M '.spec.install.spec.clusterPermissions[0].rules' >> deploy/02_clusterrole.yaml

yq -i -P deploy/02_clusterrole.yaml
