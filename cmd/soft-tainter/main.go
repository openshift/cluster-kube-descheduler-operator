package main

import (
	"fmt"
	"os"

	operatorv1 "github.com/openshift/api/operator/v1"
	deschedulerv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/apis/descheduler/v1"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/softtainter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	goflag "flag"
	flag "github.com/spf13/pflag"
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

const (
	HealthProbeHost             = "0.0.0.0"
	HealthProbePort       int32 = 6060
	MetricsHost                 = "0.0.0.0"
	MetricsPort           int32 = 8443
	CertsDir                    = "/certs-dir"
	readinessEndpointName       = "/readyz"
	livenessEndpointName        = "/livez"
)

// Restricts the cache's ListWatch to specific fields/labels per GVK at the specified object to control the memory impact
// this is used to completely overwrite the NewCache function so all the interesting objects should be explicitly listed here
func getCacheOption(operatorNamespace string) cache.Options {
	namespaceSelector := fields.Set{"metadata.namespace": operatorNamespace}.AsSelector()

	cacheOptions := cache.Options{
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Node{}: {},
			&deschedulerv1.KubeDescheduler{}: {
				Field: namespaceSelector,
			},
		},
	}

	return cacheOptions

}

func getManagerOptions(operatorNamespace string, needLeaderElection bool, scheme *apiruntime.Scheme) manager.Options {
	return manager.Options{
		HealthProbeBindAddress:     fmt.Sprintf("%s:%d", HealthProbeHost, HealthProbePort),
		ReadinessEndpointName:      readinessEndpointName,
		LivenessEndpointName:       livenessEndpointName,
		LeaderElection:             needLeaderElection,
		LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
		LeaderElectionID:           "soft-tainter-lock",
		LeaderElectionNamespace:    operatorNamespace,
		Cache:                      getCacheOption(operatorNamespace),
		Scheme:                     scheme,
		Metrics: metricsserver.Options{
			BindAddress:   fmt.Sprintf("%s:%d", MetricsHost, MetricsPort),
			SecureServing: true,
			CertDir:       CertsDir,
			CertName:      "tls.crt",
			KeyName:       "tls.key",
		},
	}
}

func main() {
	entryLog := log.Log.WithName("soft-tainter")

	var policyConfigFile string

	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	flag.StringVar(&policyConfigFile, "policy-config-file", "/policy-dir/policy.yaml", "File with descheduler policy configuration.")
	flag.CommandLine.ParseErrorsWhitelist.UnknownFlags = true
	flag.Parse()

	// Setup Scheme for all resources
	scheme := apiruntime.NewScheme()

	for _, f := range resourcesSchemeFuncs {
		err := f(scheme)
		if err != nil {
			entryLog.Error(err, "unable to register resource schema")
			os.Exit(1)
		}
	}

	// Setup a Manager
	entryLog.Info("setting up manager")
	needLeaderElection := true
	mgr, err := manager.New(config.GetConfigOrDie(), getManagerOptions(operatorclient.OperatorNamespace, needLeaderElection, scheme))
	if err != nil {
		entryLog.Error(err, "unable to set up overall controller manager")
		os.Exit(1)
	}

	err = mgr.AddHealthzCheck("ping", healthz.Ping)
	if err != nil {
		entryLog.Error(err, "unable to add health check")
		os.Exit(1)
	}

	err = mgr.AddReadyzCheck("ping", healthz.Ping)
	if err != nil {
		entryLog.Error(err, "unable to add ready check")
		os.Exit(1)
	}

	err = softtainter.RegisterReconciler(mgr, policyConfigFile)
	if err != nil {
		entryLog.Error(err, "unable to register the controller")
		os.Exit(1)
	}

	entryLog.Info("starting manager")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		entryLog.Error(err, "unable to run manager")
		os.Exit(1)
	}
}
