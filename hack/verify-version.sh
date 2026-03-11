#!/bin/bash

set -e

VERSION=$1

if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 5.2.3"
    exit 1
fi

# Validate version format (basic check for semantic versioning)
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: Version must be in format X.Y.Z (e.g., 5.2.3)"
    exit 1
fi

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "$REPO_ROOT"

echo "Verifying changes..."
FAILED=0

# Verify Dockerfiles
for file in Dockerfile Dockerfile.rhel7 bundle.Dockerfile; do
    if grep -q "release=\"$VERSION\"" "$file" && grep -q "version=\"$VERSION\"" "$file"; then
        echo "✓ $file: version updated successfully"
    else
        echo "✗ $file: version update failed (expected: $VERSION)"
        FAILED=1
    fi
done

# Verify CSV file
CSV_FILE="manifests/cluster-kube-descheduler-operator.clusterserviceversion.yaml"

if grep -q "name: clusterkubedescheduleroperator.v$VERSION" "$CSV_FILE"; then
    echo "✓ $CSV_FILE: metadata.name updated successfully"
else
    echo "✗ $CSV_FILE: metadata.name update failed (expected: clusterkubedescheduleroperator.v$VERSION)"
    FAILED=1
fi

if grep -q "olm-status-descriptors: cluster-kube-descheduler-operator.v$VERSION" "$CSV_FILE"; then
    echo "✓ $CSV_FILE: spec.labels.olm-status-descriptors updated successfully"
else
    echo "✗ $CSV_FILE: spec.labels.olm-status-descriptors update failed (expected: cluster-kube-descheduler-operator.v$VERSION)"
    FAILED=1
fi

if grep -q "olm.skipRange:.*<$VERSION" "$CSV_FILE"; then
    echo "✓ $CSV_FILE: olm.skipRange upper bound updated successfully"
else
    echo "✗ $CSV_FILE: olm.skipRange upper bound update failed (expected upper bound: <$VERSION)"
    FAILED=1
fi

if grep -q "version: $VERSION" "$CSV_FILE"; then
    echo "✓ $CSV_FILE: spec.version updated successfully"
else
    echo "✗ $CSV_FILE: spec.version update failed (expected: $VERSION)"
    FAILED=1
fi

if grep -q "value: $VERSION" "$CSV_FILE" && grep -B2 "value: $VERSION" "$CSV_FILE" | grep -q "OPERAND_VERSION"; then
    echo "✓ $CSV_FILE: OPERAND_VERSION environment variable updated successfully"
else
    echo "✗ $CSV_FILE: OPERAND_VERSION environment variable update failed (expected: $VERSION)"
    FAILED=1
fi

# Verify spec.skips includes all necessary patch versions and excludes >= target
MAJOR=$(echo "$VERSION" | cut -d. -f1)
MINOR=$(echo "$VERSION" | cut -d. -f2)
PATCH=$(echo "$VERSION" | cut -d. -f3)
SKIPS_VERIFIED=true

# Get current skips list once for all checks
CURRENT_SKIPS=$(yq eval '.spec.skips[]' "$CSV_FILE")

for ((p=0; p<PATCH; p++)); do
    SKIP_VERSION="clusterkubedescheduleroperator.v${MAJOR}.${MINOR}.${p}"
    if ! echo "$CURRENT_SKIPS" | grep -q "^${SKIP_VERSION}$"; then
        echo "✗ $CSV_FILE: spec.skips missing $SKIP_VERSION"
        SKIPS_VERIFIED=false
        FAILED=1
    fi
done

# Check that no skips >= target version exist for this major.minor
while IFS= read -r skip_entry; do
    if [[ "$skip_entry" =~ clusterkubedescheduleroperator\.v${MAJOR}\.${MINOR}\.([0-9]+) ]]; then
        SKIP_PATCH="${BASH_REMATCH[1]}"
        if [ "$SKIP_PATCH" -ge "$PATCH" ]; then
            echo "✗ $CSV_FILE: spec.skips contains $skip_entry (>= target version)"
            SKIPS_VERIFIED=false
            FAILED=1
        fi
    fi
done <<< "$CURRENT_SKIPS"

if [ "$SKIPS_VERIFIED" = true ]; then
    echo "✓ $CSV_FILE: spec.skips updated successfully"
fi

if [ $FAILED -eq 1 ]; then
    echo ""
    echo "Error: Version verification failed for one or more files"
    exit 1
fi

echo ""
echo "Version successfully verified in all files"
