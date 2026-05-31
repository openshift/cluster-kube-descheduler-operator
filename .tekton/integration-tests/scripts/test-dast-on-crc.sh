#!/bin/bash
#
# Test KDO DAST pipeline locally on CRC.
# Simulates the Konflux integration test pipeline without Tekton/EaaS.
#
# Prerequisites:
#   - CRC running: crc start
#   - Logged in:   eval $(crc console --credentials) && oc login ...
#
# Usage:
#   ./test-dast-on-crc.sh [BUNDLE_IMAGE]
#
# If BUNDLE_IMAGE is omitted, the script installs KDO from OperatorHub instead.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIGS_DIR="${SCRIPT_DIR}/../configs"
KDO_VERSION="4.21"
KDO_NAMESPACE="openshift-kube-descheduler-operator"
DAST_NAMESPACE="rapidast-kdo"
BUNDLE_IMAGE="${1:-}"
RESULTS_DIR="/tmp/kdo-dast-results"
OOBTKUBE_DURATION="${OOBTKUBE_DURATION:-60}"

header() { echo -e "\n\033[1;36m=== $1 ===\033[0m"; }
ok()     { echo -e "\033[1;32m✓ $1\033[0m"; }
fail()   { echo -e "\033[1;31m✗ $1\033[0m"; }

cleanup() {
    header "Cleanup"
    echo "Cleaning up test resources..."
    oc delete namespace "${DAST_NAMESPACE}" --ignore-not-found --wait=false 2>/dev/null || true
    oc delete clusterrolebinding rapidast-privileged --ignore-not-found 2>/dev/null || true
    if [ -n "${BUNDLE_IMAGE}" ]; then
        operator-sdk cleanup kube-descheduler-operator \
            --namespace "${KDO_NAMESPACE}" 2>/dev/null || true
    fi
    oc delete namespace "${KDO_NAMESPACE}" --ignore-not-found --wait=false 2>/dev/null || true
    oc delete imagedigestmirrorset kdo-konflux-mirrors --ignore-not-found 2>/dev/null || true
    ok "Cleanup complete"
}

trap cleanup EXIT

# ─────────────────────────────────────────────────────────────────────────────
# Phase 1: Verify CRC is running
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 1: Verify CRC"
if ! oc whoami &>/dev/null; then
    fail "Not logged into OpenShift. Run: eval \$(crc console --credentials)"
    exit 1
fi
CLUSTER_VERSION=$(oc get clusterversion version -o jsonpath='{.status.desired.version}' 2>/dev/null || echo "unknown")
ok "Connected to CRC (OCP ${CLUSTER_VERSION}) as $(oc whoami)"

# ─────────────────────────────────────────────────────────────────────────────
# Phase 2: Install operator-sdk (if not present)
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 2: Install operator-sdk"
if command -v operator-sdk &>/dev/null; then
    ok "operator-sdk already installed: $(operator-sdk version | head -1)"
else
    echo "Downloading operator-sdk..."
    MACHINE=$(uname -m)
    case "${MACHINE}" in
        x86_64)       ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *)            ARCH="${MACHINE}" ;;
    esac
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    LOCAL_BIN="${HOME}/.local/bin"
    mkdir -p "${LOCAL_BIN}"
    OPERATOR_SDK_DL_URL="https://github.com/operator-framework/operator-sdk/releases/latest/download"
    curl -sLO "${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH}"
    chmod +x "operator-sdk_${OS}_${ARCH}"
    mv "operator-sdk_${OS}_${ARCH}" "${LOCAL_BIN}/operator-sdk"
    export PATH="${LOCAL_BIN}:${PATH}"
    ok "operator-sdk installed to ${LOCAL_BIN}: $(operator-sdk version | head -1)"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Phase 3: Install KDO operator
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 3: Install KDO operator"

# Mirror registry.redhat.io images to Konflux Quay builds (same as pipeline imageContentSources)
cat <<'MIRROREOF' | oc apply -f -
apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
  name: kdo-konflux-mirrors
spec:
  imageDigestMirrors:
    - source: registry.redhat.io/kube-descheduler-operator/kube-descheduler-rhel9-operator
      mirrors:
        - quay.io/redhat-user-workloads/kdo-workloads-tenant/kube-descheduler-operator-4-21
    - source: registry.redhat.io/kube-descheduler-operator/descheduler-rhel9
      mirrors:
        - quay.io/redhat-user-workloads/kdo-workloads-tenant/kube-descheduler-operator-4-21
    - source: registry.redhat.io/kube-descheduler-operator/kube-descheduler-operator-bundle
      mirrors:
        - quay.io/redhat-user-workloads/kdo-workloads-tenant/kube-descheduler-operator-bundle-4-21
MIRROREOF
echo "Waiting for ImageDigestMirrorSet to propagate (CRC node may restart)..."
for i in $(seq 1 24); do
    if oc get nodes 2>/dev/null | grep -q " Ready"; then
        break
    fi
    echo "  Node not ready yet (attempt ${i}/24)..."
    sleep 10
done
sleep 10
ok "Mirror set applied, node is Ready"

oc create namespace "${KDO_NAMESPACE}" 2>/dev/null || true

if [ -n "${BUNDLE_IMAGE}" ]; then
    echo "Installing from bundle: ${BUNDLE_IMAGE}"

    # Pre-create OperatorGroup so OLM sees it before the CSV lands
    cat <<OGEOF | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: kdo-og
  namespace: ${KDO_NAMESPACE}
spec:
  targetNamespaces:
    - ${KDO_NAMESPACE}
OGEOF
    sleep 5

    operator-sdk run bundle --timeout=10m \
        --namespace "${KDO_NAMESPACE}" \
        --skip-tls-verify \
        "${BUNDLE_IMAGE}" --verbose
else
    echo "No bundle image provided. Installing from OperatorHub..."
    cat <<'EOF' | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: kdo-og
  namespace: openshift-kube-descheduler-operator
spec:
  targetNamespaces:
    - openshift-kube-descheduler-operator
EOF
    cat <<'EOF' | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: cluster-kube-descheduler-operator
  namespace: openshift-kube-descheduler-operator
spec:
  channel: stable
  installPlanApproval: Automatic
  name: cluster-kube-descheduler-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
EOF
    echo "Waiting for operator CSV to succeed..."
    for i in $(seq 1 30); do
        CSV=$(oc get csv -n "${KDO_NAMESPACE}" -o jsonpath='{.items[0].status.phase}' 2>/dev/null || echo "Waiting")
        if [ "${CSV}" = "Succeeded" ]; then break; fi
        echo "  CSV status: ${CSV} (attempt ${i}/30)"
        sleep 10
    done
fi

echo "Waiting for operator deployment..."
# Label may differ between bundle install and OperatorHub; wait for any deployment
for i in $(seq 1 30); do
    DEPLOY=$(oc get deployment -n "${KDO_NAMESPACE}" -o name 2>/dev/null | head -1)
    if [ -n "${DEPLOY}" ]; then
        echo "  Found: ${DEPLOY}"
        oc wait --for condition=Available -n "${KDO_NAMESPACE}" "${DEPLOY}" --timeout=180s
        break
    fi
    echo "  No deployment yet (attempt ${i}/30)"
    sleep 10
done
if [ -z "${DEPLOY:-}" ]; then
    fail "No operator deployment found in ${KDO_NAMESPACE}"
    exit 1
fi
ok "KDO operator is running"

# ─────────────────────────────────────────────────────────────────────────────
# Phase 4: Prepare config files
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 4: Prepare config files"
mkdir -p "${RESULTS_DIR}"

if [ ! -f "${CONFIGS_DIR}/rapidastconfig.yaml" ] || [ ! -f "${CONFIGS_DIR}/kdo-cr.yaml" ]; then
    fail "Config files not found in ${CONFIGS_DIR}"
    exit 1
fi

# Strip GCS block for local testing and expand KDO_VERSION
export KDO_VERSION
awk '
BEGIN { skip=0 }
/^  googleCloudStorage:/ { skip=1; next }
skip && /^  [a-zA-Z]/ { skip=0 }
skip && /^[a-zA-Z]/ { skip=0 }
skip { next }
{ print }
' "${CONFIGS_DIR}/rapidastconfig.yaml" | sed "s/-d 300/-d ${OOBTKUBE_DURATION}/" | envsubst '${KDO_VERSION}' > "${RESULTS_DIR}/rapidastconfig.yaml"
cp "${CONFIGS_DIR}/kdo-cr.yaml" "${RESULTS_DIR}/kdo-cr.yaml"
ok "Config files ready in ${RESULTS_DIR}"

# ─────────────────────────────────────────────────────────────────────────────
# Phase 5: Create DAST namespace and RBAC
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 5: Create DAST namespace + RBAC"
oc create namespace "${DAST_NAMESPACE}" 2>/dev/null || true
oc create sa privileged-sa -n "${DAST_NAMESPACE}" 2>/dev/null || true
oc create clusterrolebinding rapidast-privileged \
    --clusterrole=cluster-admin \
    --serviceaccount="${DAST_NAMESPACE}:privileged-sa" 2>/dev/null || true
ok "Namespace ${DAST_NAMESPACE} ready with cluster-admin SA"

# ─────────────────────────────────────────────────────────────────────────────
# Phase 6: Create PVC + ConfigMap
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 6: Create PVC + ConfigMap"
cat <<EOF | oc apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: rapidast-pvc
  namespace: ${DAST_NAMESPACE}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
EOF

oc create configmap rapidast-kdo-config \
    --from-file=rapidastconfig.yaml="${RESULTS_DIR}/rapidastconfig.yaml" \
    --from-file=kdo-cr.yaml="${RESULTS_DIR}/kdo-cr.yaml" \
    --namespace="${DAST_NAMESPACE}" \
    --dry-run=client -o yaml | oc apply -f -
ok "PVC and ConfigMap created"

# ─────────────────────────────────────────────────────────────────────────────
# Phase 7: Apply enriched CR
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 7: Apply enriched KubeDescheduler CR"
oc apply -f "${RESULTS_DIR}/kdo-cr.yaml"

echo "Waiting 15s for operator to reconcile..."
sleep 15
DEPLOY=$(oc get deployment -n "${KDO_NAMESPACE}" -o name 2>/dev/null | head -1)
if [ -n "${DEPLOY}" ]; then
    oc wait --for condition=Available -n "${KDO_NAMESPACE}" "${DEPLOY}" --timeout=120s
fi
ok "CR applied and operator reconciled"

# ─────────────────────────────────────────────────────────────────────────────
# Phase 8: Create RapiDAST Job
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 8: Launch RapiDAST oobtkube Job"
cat <<'EOF' | oc apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: rapidast-kdo-oobtkube
  namespace: rapidast-kdo
spec:
  backoffLimit: 0
  template:
    spec:
      serviceAccountName: privileged-sa
      containers:
        - name: rapidast
          image: quay.io/redhatproductsecurity/rapidast:latest
          imagePullPolicy: Always
          command: ["/bin/bash", "-c"]
          args:
            - |
              rapidast.py --log-level debug --config /opt/rapidast/config/rapidastconfig.yaml
              RESULT=$?
              cp /opt/rapidast/results/*.sarif /opt/rapidast/pvc-results/ 2>/dev/null || true
              exit $RESULT
          env:
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
          securityContext:
            privileged: true
          resources:
            requests:
              cpu: 250m
              memory: 256Mi
            limits:
              cpu: 1000m
              memory: 1Gi
          volumeMounts:
            - name: config
              mountPath: /opt/rapidast/config
            - name: results
              mountPath: /opt/rapidast/pvc-results
      volumes:
        - name: config
          configMap:
            name: rapidast-kdo-config
        - name: results
          persistentVolumeClaim:
            claimName: rapidast-pvc
      restartPolicy: Never
EOF
ok "RapiDAST Job created"

# ─────────────────────────────────────────────────────────────────────────────
# Phase 9: Wait for Job + collect results
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 9: Wait for Job completion"
echo "Tailing Job logs (this can take ~5-10 min)..."
echo "You can also watch in another terminal: oc logs -f job/rapidast-kdo-oobtkube -n ${DAST_NAMESPACE}"
echo ""

oc wait --for=condition=complete job/rapidast-kdo-oobtkube \
    -n "${DAST_NAMESPACE}" --timeout=600s 2>/dev/null \
    || oc wait --for=condition=failed job/rapidast-kdo-oobtkube \
    -n "${DAST_NAMESPACE}" --timeout=30s 2>/dev/null \
    || echo "Job still running after timeout"

JOB_STATUS=$(oc get job rapidast-kdo-oobtkube -n "${DAST_NAMESPACE}" \
    -o jsonpath='{.status.conditions[0].type}' 2>/dev/null || echo "Unknown")
echo "Job status: ${JOB_STATUS}"

echo ""
echo "--- Job logs (last 50 lines) ---"
oc logs job/rapidast-kdo-oobtkube -n "${DAST_NAMESPACE}" --tail=50 2>/dev/null || true
echo "--- End logs ---"

header "Phase 9b: Retrieve SARIF from PVC"
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: results-fetcher
  namespace: ${DAST_NAMESPACE}
spec:
  containers:
    - name: fetcher
      image: registry.access.redhat.com/ubi9/ubi:latest
      command: ["sleep", "300"]
      volumeMounts:
        - name: results
          mountPath: /results
  volumes:
    - name: results
      persistentVolumeClaim:
        claimName: rapidast-pvc
  restartPolicy: Never
EOF
oc wait --for=condition=Ready pod/results-fetcher -n "${DAST_NAMESPACE}" --timeout=60s

echo "Listing PVC contents:"
oc exec results-fetcher -n "${DAST_NAMESPACE}" -- ls -la /results/ 2>/dev/null || true

oc cp "${DAST_NAMESPACE}/results-fetcher:/results/oobtkube-results.sarif" \
    "${RESULTS_DIR}/oobtkube-results.sarif" 2>/dev/null && \
    ok "SARIF retrieved: ${RESULTS_DIR}/oobtkube-results.sarif" || \
    fail "No SARIF file found on PVC"

# ─────────────────────────────────────────────────────────────────────────────
# Phase 10: Trivy scan
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 10: Trivy misconfiguration scan"
if command -v trivy &>/dev/null; then
    trivy k8s --report summary --severity HIGH,CRITICAL \
        --include-namespaces "${KDO_NAMESPACE}" \
        --format json --output "${RESULTS_DIR}/trivy-results.json" 2>/dev/null && \
        ok "Trivy results: ${RESULTS_DIR}/trivy-results.json" || \
        fail "Trivy scan failed"
else
    echo "Trivy not installed locally. Running via container on cluster..."
    cat <<TRIVYEOF | oc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: trivy-scan
  namespace: ${DAST_NAMESPACE}
spec:
  serviceAccountName: privileged-sa
  containers:
    - name: trivy
      image: quay.io/redhatproductsecurity/rapidast:latest
      command: ["/bin/bash", "-c"]
      args:
        - |
          trivy k8s --report summary --severity HIGH,CRITICAL \
            --include-namespaces ${KDO_NAMESPACE} --format table 2>&1
          exit 0
  restartPolicy: Never
TRIVYEOF
    oc wait --for=condition=Ready pod/trivy-scan -n "${DAST_NAMESPACE}" --timeout=60s 2>/dev/null || true
    oc wait --for=jsonpath='{.status.phase}'=Succeeded pod/trivy-scan \
        -n "${DAST_NAMESPACE}" --timeout=120s 2>/dev/null || true
    echo "--- Trivy output ---"
    oc logs trivy-scan -n "${DAST_NAMESPACE}" 2>/dev/null && \
        ok "Trivy scan complete" || \
        fail "Trivy scan failed (non-critical, oobtkube results still valid)"
    echo "--- End Trivy ---"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Phase 11: Display results
# ─────────────────────────────────────────────────────────────────────────────
header "Phase 11: Results Summary"
echo ""
echo "Results directory: ${RESULTS_DIR}"
ls -la "${RESULTS_DIR}/" 2>/dev/null
echo ""

if [ -s "${RESULTS_DIR}/oobtkube-results.sarif" ]; then
    TOTAL=$(jq '[.runs[0].results[]?] | length' "${RESULTS_DIR}/oobtkube-results.sarif" 2>/dev/null || echo "?")
    CRITICAL=$(jq '[.runs[0].results[]? | select(.level == "error")] | length' "${RESULTS_DIR}/oobtkube-results.sarif" 2>/dev/null || echo "?")
    HIGH=$(jq '[.runs[0].results[]? | select(.level == "warning")] | length' "${RESULTS_DIR}/oobtkube-results.sarif" 2>/dev/null || echo "?")
    MEDIUM=$(jq '[.runs[0].results[]? | select(.level == "note")] | length' "${RESULTS_DIR}/oobtkube-results.sarif" 2>/dev/null || echo "?")

    echo "┌─────────────────────────────────────┐"
    echo "│  oobtkube DAST Results               │"
    echo "├─────────────────────────────────────┤"
    echo "│  Total findings:  ${TOTAL}"
    echo "│  Critical:        ${CRITICAL}"
    echo "│  High:            ${HIGH}"
    echo "│  Medium:          ${MEDIUM}"
    echo "└─────────────────────────────────────┘"
    ok "DAST test PASSED - SARIF produced successfully"
else
    echo "┌─────────────────────────────────────┐"
    echo "│  No SARIF file produced.             │"
    echo "│  Check Job logs for errors.          │"
    echo "└─────────────────────────────────────┘"
    fail "DAST test - no SARIF output (check logs above)"
fi

echo ""
echo "Full Job logs: oc logs job/rapidast-kdo-oobtkube -n ${DAST_NAMESPACE}"
echo "SARIF file:    ${RESULTS_DIR}/oobtkube-results.sarif"
echo ""
ok "CRC DAST test complete!"
