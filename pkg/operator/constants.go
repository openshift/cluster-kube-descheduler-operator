package operator

const (
	// ThanosQuerierHost is the supported in-cluster query endpoint. Its serving
	// certificate is signed by the OpenShift service CA.
	ThanosQuerierHost = "thanos-querier.openshift-monitoring.svc.cluster.local:9091"
)
