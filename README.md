# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.17 ./catalog-migrate
$ mkdir -p v4.17/catalog/cluster-kube-descheduler-operator
$ opm alpha convert-template basic ./catalog-migrate/cluster-kube-descheduler-operator/catalog.json > v4.17/catalog-template.json
```

To update the catalog

```
$ cd v4.17
$ opm alpha render-template basic catalog-template.json --migrate-level bundle-object-to-csv-metadata > catalog/cluster-kube-descheduler-operator/catalog.json
```

## Releases

| kdo version | bundle image                                                     |
| ----------- | ---------------------------------------------------------------- |
| 5.0.1       | b26e5ebec8ffcfaf18c8c4acb8c25a71318ea7d3b372c9e3e170b6026a681a89 |
| 5.0.2       | 72c2aeb630281a636cad334fbbf0e67b70afba26c61a1b25a2b93277765e5ac7 |
| 5.0.3       | d601b1ab843f10dfcb984e4e3853ee8dad64a96c5be0e4f17cc0c32882926f71 |
| 5.1.0       | d8ccfec899fbd543a076c28bce386e9ec764bada413350ae53132863ebddaa71 |
| 5.1.1       | 618345991268504019c4fac34d09f3ea7225ffcee2aec48479f9f7e9189b16fc |
| 5.1.2       | f9a77a6732f74a55644c33dcec4d413baabf9280f60b5a68841359d6a1bae956 |
| 5.1.3       | 37afe091e3b9656c107196d7120ce171d8b0bf895264a9791c4559e97c81a00a |
| 5.1.4       | a8b80a453bb67cba00d970a8a2e7e4191668af5edb429af5cea3e8b5d6483c2e |
