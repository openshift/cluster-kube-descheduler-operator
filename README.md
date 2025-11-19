# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.16 ./catalog-migrate
$ mkdir -p v4.16/catalog/cluster-kube-descheduler-operator
$ opm alpha convert-template basic ./catalog-migrate/cluster-kube-descheduler-operator/catalog.json > v4.16/catalog-template.json
```

To update the catalog

```
$ cd v4.16
$ opm alpha render-template basic catalog-template.json > catalog/cluster-kube-descheduler-operator/catalog.json
```

## Releases

| kdo version | bundle image                                                     |
| ----------- | ---------------------------------------------------------------- |
| 4.14.1      | 73e3b45b83ecbb91a805780467b1326171c0d860ac0b640f3e888980cde9a6e9 |
| 5.0.2       | 72c2aeb630281a636cad334fbbf0e67b70afba26c61a1b25a2b93277765e5ac7 |
| 5.0.3       | 8ff24b9075c528a97f230ae78bb14b050e5264294d065aa12c941c90af8d69ca |
