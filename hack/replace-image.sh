#!/bin/bash

DIRECTORY_PATH=$1
REPLACED_IMG=$2
IMAGE=$3

find "$DIRECTORY_PATH/" -type f -exec sed -i "s|${REPLACED_IMG}|${IMAGE}|g" {} \;

if grep -rq "${REPLACED_IMG}" "$DIRECTORY_PATH"; then
    echo "Failed to replace image references"
    exit 1
else
    echo "Image references replaced"
fi

if grep -r "${IMAGE}" "$DIRECTORY_PATH"; then
    echo "New image references found"
else
    echo "No new image references found"
fi
