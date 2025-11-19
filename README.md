# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.12 ./catalog-migrate
$ mkdir -p v4.12/catalog/cluster-kube-descheduler-operator
$ opm alpha convert-template basic ./catalog-migrate/cluster-kube-descheduler-operator/catalog.json > v4.12/catalog-template.json
```

To update the catalog

```
$ cd v4.12
$ export REGISTRY_AUTH_FILE=...
$ opm alpha render-template basic catalog-template.json > catalog/cluster-kube-descheduler-operator/catalog.json
```

## Releases

| kdo version | bundle image                                                     |
| ----------- | ---------------------------------------------------------------- |
| 4.14.1      | c1999b71015affba0d132a9704d14f7a17db1b0d03231a5ecaa5dd6f70e9d540 |
