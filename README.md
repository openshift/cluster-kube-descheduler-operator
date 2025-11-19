# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.14 ./catalog-migrate
$ mkdir -p v4.14/catalog/cluster-kube-descheduler-operator
$ opm alpha convert-template basic ./catalog-migrate/cluster-kube-descheduler-operator/catalog.json > v4.14/catalog-template.json
```

To update the catalog

```
$ cd v4.14
$ export REGISTRY_AUTH_FILE=...
$ opm alpha render-template basic catalog-template.json > catalog/cluster-kube-descheduler-operator/catalog.json
```

## Releases

| kdo version | bundle image                                                     |
| ----------- | ---------------------------------------------------------------- |
| 4.14.1      | 73e3b45b83ecbb91a805780467b1326171c0d860ac0b640f3e888980cde9a6e9 |
