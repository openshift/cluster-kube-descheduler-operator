# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.18 ./catalog-migrate
$ mkdir -p v4.19/catalog/cluster-kube-descheduler-operator
$ opm alpha convert-template basic ./catalog-migrate/cluster-kube-descheduler-operator/catalog.json > v4.19/catalog-template.json
```

To update the catalog

```
$ cd v4.19
$ export REGISTRY_AUTH_FILE=...
$ opm alpha render-template basic catalog-template.json --migrate-level bundle-object-to-csv-metadata > catalog/cluster-kube-descheduler-operator/catalog.json
```

## Releases

| kdo version | bundle image                                                     |
| ----------- | ---------------------------------------------------------------- |
| 5.1.0       | d8ccfec899fbd543a076c28bce386e9ec764bada413350ae53132863ebddaa71 |
| 5.1.1       | 618345991268504019c4fac34d09f3ea7225ffcee2aec48479f9f7e9189b16fc |
| 5.1.2       | f9a77a6732f74a55644c33dcec4d413baabf9280f60b5a68841359d6a1bae956 |
| 5.1.3       | 37afe091e3b9656c107196d7120ce171d8b0bf895264a9791c4559e97c81a00a |
| 5.2.0       | 020eeb41c7c24c3caf77a3a3f6f598076d5e22bf793b85d3c3e89eb705896c0e |
| 5.2.1       | 203768b3fd5783d8572c1e626327ead0838cb4613ca30aea86beeb2f78935051 |
