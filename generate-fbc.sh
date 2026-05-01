#!/bin/bash

for OCP_VERSION in $(find . -maxdepth 1 -type d -name 'v4.*' | sed 's|^\./||' | sort -t. -k1,1V -k2,2nr); do
    echo "OCP_VERSION: ${OCP_VERSION}"
    VERSION_NUM=$(echo $OCP_VERSION | cut -d. -f2)
    if [ "$VERSION_NUM" -ge 17 ]; then
        opm alpha render-template basic $OCP_VERSION/catalog-template.yaml --migrate-level bundle-object-to-csv-metadata > $OCP_VERSION/catalog/cluster-kube-descheduler-operator/catalog.json
    else
        opm alpha render-template basic $OCP_VERSION/catalog-template.yaml > $OCP_VERSION/catalog/cluster-kube-descheduler-operator/catalog.json
    fi
done
