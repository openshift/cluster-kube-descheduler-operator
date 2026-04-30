# README

## FBC catalog rendering

To initiliaze catalog-template.yaml

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.13 ./catalog-migrate
$ mkdir -p v4.13/catalog/cluster-kube-descheduler-operator
$ opm alpha convert-template basic -o yaml ./catalog-migrate/cluster-kube-descheduler-operator/catalog.json > v4.13/catalog-template.yaml
```

To update the catalog

```
$ export REGISTRY_AUTH_FILE=...
$ opm alpha render-template basic v4.13/catalog-template.yaml > v4.13/catalog/cluster-kube-descheduler-operator/catalog.json
```

## Releases

| kdo version | bundle image                                                     |
| ----------- | ---------------------------------------------------------------- |
| 4.13.1      | 7673ba7d14a9e5e391032a480cb573c3d0d8dccc50d7e54d928cb616ac5d54b2 |
