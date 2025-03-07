package main

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	deschedulerv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/softtainter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	apiruntime "k8s.io/apimachinery/pkg/runtime"
)

func init() {
	log.SetLogger(zap.New())
}

var (
	resourcesSchemeFuncs = []func(*apiruntime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		operatorv1.Install,
		deschedulerv1.AddToScheme,
	}
)

// Restricts the cache's ListWatch to specific fields/labels per GVK at the specified object to control the memory impact
// this is used to completely overwrite the NewCache function so all the interesting objects should be explicitly listed here
func getCacheOption(operatorNamespace string) cache.Options {
	namespaceSelector := fields.Set{"metadata.namespace": operatorNamespace}.AsSelector()

	cacheOptions := cache.Options{
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Node{}: {},
			&corev1.ConfigMap{}: {
				Field: namespaceSelector,
			},
			&deschedulerv1.KubeDescheduler{}: {},
		},
	}

	return cacheOptions

}

func getManagerOptions(operatorNamespace string, needLeaderElection bool, scheme *apiruntime.Scheme) manager.Options {
	return manager.Options{
		// TODO: configure metrics and readiness and liveness probes
		//Metrics: server.Options{
		//	BindAddress:    fmt.Sprintf("%s:%d", MetricsHost, MetricsPort),
		//	FilterProvider: authorization.HttpWithBearerToken,
		//},
		//HealthProbeBindAddress: fmt.Sprintf("%s:%d", HealthProbeHost, HealthProbePort),
		//ReadinessEndpointName:  ReadinessEndpointName,
		//LivenessEndpointName:   LivenessEndpointName,
		LeaderElection:             needLeaderElection,
		LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
		LeaderElectionID:           "soft-tainter-lock",
		Cache:                      getCacheOption(operatorNamespace),
		Scheme:                     scheme,
	}
}

func main() {
	entryLog := log.Log.WithName("soft-tainter")

	// Setup Scheme for all resources
	scheme := apiruntime.NewScheme()
	for _, f := range resourcesSchemeFuncs {
		err := f(scheme)
		entryLog.Error(err, "unable to register resource schema")
		os.Exit(1)
	}

	// Setup a Manager
	entryLog.Info("setting up manager")
	needLeaderElection := false // TODO: fix me
	mgr, err := manager.New(config.GetConfigOrDie(), getManagerOptions(operatorclient.OperatorNamespace, needLeaderElection, scheme))
	if err != nil {
		entryLog.Error(err, "unable to set up overall controller manager")
		os.Exit(1)
	}

	err = softtainter.RegisterReconciler(mgr)
	if err != nil {
		entryLog.Error(err, "unable to register the controller")
		os.Exit(1)
	}

	// TODO: configure the manager cache to read only the descheduler CM

	entryLog.Info("starting manager")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		entryLog.Error(err, "unable to run manager")
		os.Exit(1)
	}
}
