# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.15 ./catalog-migrate
$ mkdir -p v4.15/catalog/cluster-kube-descheduler-operator
$ opm alpha convert-template basic ./catalog-migrate/cluster-kube-descheduler-operator/catalog.json > v4.15/catalog-template.json
```

To update the catalog

```
$ cd v4.15
$ export REGISTRY_AUTH_FILE=...
$ opm alpha render-template basic catalog-template.json > catalog/cluster-kube-descheduler-operator/catalog.json
```
