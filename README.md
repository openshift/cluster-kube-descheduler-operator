# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.21 ./catalog-migrate
$ mkdir -p v4.21/catalog/cluster-kube-descheduler-operator
$ opm alpha convert-template basic -o yaml ./catalog-migrate/cluster-kube-descheduler-operator/catalog.json > v4.21/catalog-template.yaml
```

To update the catalog

```
$ export REGISTRY_AUTH_FILE=...
$ opm alpha render-template basic v4.21/catalog-template.yaml --migrate-level bundle-object-to-csv-metadata > v4.21/catalog/cluster-kube-descheduler-operator/catalog.json
```
## Configure mirroring for opm

Under `/etc/containers/registries.conf` (or drop a file inside /etc/containers/registries.conf.d/):
```
[[registry]]
prefix = "registry.redhat.io/kube-descheduler-operator"
location = "registry.redhat.io/kube-descheduler-operator"
insecure = true

[[registry.mirror]]
prefix = "registry.redhat.io/kube-descheduler-operator"
location = "registry.stage.redhat.io/kube-descheduler-operator"
insecure = true
```
