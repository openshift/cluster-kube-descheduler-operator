# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.19 ./catalog-migrate
$ mkdir -p v4.20/catalog/cluster-kube-descheduler-operator
$ opm alpha convert-template basic ./catalog-migrate/cluster-kube-descheduler-operator/catalog.json > v4.20/catalog-template.json
```

To update the catalog

```
$ cd v4.20
$ export REGISTRY_AUTH_FILE=...
$ opm alpha render-template basic catalog-template.json --migrate-level bundle-object-to-csv-metadata > catalog/cluster-kube-descheduler-operator/catalog.json
```
