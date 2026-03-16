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

# Function to update version and release labels in a Dockerfile
update_dockerfile_version() {
    local file=$1
    local version=$2

    echo "Updating $file..."
    sed -i "s/release=\"[^\"]*\"/release=\"$version\"/g" "$file"
    sed -i "s/version=\"[^\"]*\"/version=\"$version\"/g" "$file"
}

# Function to update ClusterServiceVersion YAML file
update_csv_version() {
    local file=$1
    local version=$2
    local current_date=$(date -I)

    echo "Updating $file..."

    # Update metadata.name field (e.g., clusterkubedescheduleroperator.v5.2.1 -> clusterkubedescheduleroperator.v5.2.2)
    yq eval -i ".metadata.name = \"clusterkubedescheduleroperator.v${version}\"" "$file"

    # Update metadata.annotations.createdAt to current date
    yq eval -i ".metadata.annotations.createdAt = \"${current_date}\"" "$file"

    # Update olm.skipRange annotation upper bound (e.g., ">=5.1.0 <5.2.1" -> ">=5.1.0 <5.2.2")
    # Extract current skipRange and update the upper bound
    local current_skip_range=$(yq eval '.metadata.annotations."olm.skipRange"' "$file")
    local new_skip_range=$(echo "$current_skip_range" | sed "s/<[0-9]\+\.[0-9]\+\.[0-9]\+/<${version}/")
    yq eval -i ".metadata.annotations.\"olm.skipRange\" = \"${new_skip_range}\"" "$file"

    # Update spec.version
    yq eval -i ".spec.version = \"${version}\"" "$file"

    # Update spec.labels.olm-status-descriptors
    yq eval -i ".spec.labels.\"olm-status-descriptors\" = \"cluster-kube-descheduler-operator.v${version}\"" "$file"

    # Update OPERAND_VERSION environment variable
    yq eval -i "(.spec.install.spec.deployments[0].spec.template.spec.containers[0].env[] | select(.name == \"OPERAND_VERSION\").value) = \"${version}\"" "$file"

    # Update spec.skips to include all patch versions less than target version
    # Parse version into major.minor.patch
    local major=$(echo "$version" | cut -d. -f1)
    local minor=$(echo "$version" | cut -d. -f2)
    local patch=$(echo "$version" | cut -d. -f3)

    # First, remove any skip entries >= target version for this major.minor
    # Get all current skips and filter out ones that are >= target version
    local current_skips=$(yq eval '.spec.skips[]' "$file")
    while IFS= read -r skip_entry; do
        if [[ "$skip_entry" =~ clusterkubedescheduleroperator\.v${major}\.${minor}\.([0-9]+) ]]; then
            local skip_patch="${BASH_REMATCH[1]}"
            if [ "$skip_patch" -ge "$patch" ]; then
                echo "  Removing ${skip_entry} from spec.skips (>= target version)"
                yq eval -i "del(.spec.skips[] | select(. == \"${skip_entry}\"))" "$file"
            fi
        fi
    done <<< "$current_skips"

    # Add all patch versions from 0 to (patch - 1) for current minor version
    for ((p=0; p<patch; p++)); do
        local skip_version="clusterkubedescheduleroperator.v${major}.${minor}.${p}"
        # Check if this version is already in the skips list
        if ! yq eval ".spec.skips | contains([\"${skip_version}\"])" "$file" | grep -q "true"; then
            echo "  Adding ${skip_version} to spec.skips"
            yq eval -i ".spec.skips += [\"${skip_version}\"]" "$file"
        fi
    done
}

echo "Bumping version to $VERSION in Dockerfiles..."

# Update all Dockerfiles
update_dockerfile_version "Dockerfile" "$VERSION"
update_dockerfile_version "Dockerfile.rhel7" "$VERSION"
update_dockerfile_version "bundle.Dockerfile" "$VERSION"

echo "Bumping version to $VERSION in ClusterServiceVersion..."

# Update ClusterServiceVersion
update_csv_version "manifests/cluster-kube-descheduler-operator.clusterserviceversion.yaml" "$VERSION"

echo ""
echo "Version update completed"
