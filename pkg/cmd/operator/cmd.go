package operator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"sigs.k8s.io/yaml"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	configlistersv1 "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/version"
	"github.com/openshift/library-go/pkg/config/client"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	libgoapiserver "github.com/openshift/library-go/pkg/operator/configobserver/apiserver"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
)

const (
	configFileName  = "cluster-kube-descheduler-operator-config.yaml"
	apiServerCRName = "cluster"
)

type tlsConfig struct {
	MinTLSVersion string
	CipherSuites  []string
}

func NewOperator() *cobra.Command {
	// Create cancellable context for the operator
	// This context controls both the operator itself and the TLS watcher
	// Cancelling it will terminate the entire operator process
	operatorContext, operatorContextCancel := context.WithCancel(context.Background())

	// Create command with the shared context
	cmd := controllercmd.
		NewControllerCommandConfig("openshift-cluster-kube-descheduler-operator", version.Get(), operator.RunOperator, clock.RealClock{}).
		NewCommandWithContext(operatorContext)

	cmd.Use = "operator"
	cmd.Short = "Start the Cluster kube-descheduler Operator"

	// Add PersistentPreRunE to:
	// 1. Generate config with TLS settings BEFORE operator starts
	// 2. Set up APIServer CR watcher to restart on TLS changes
	originalPreRunE := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		if originalPreRunE != nil {
			if err := originalPreRunE(c, args); err != nil {
				return err
			}
		}

		// Read the kubeconfig flag to ensure we use the same config as the operator
		kubeconfigPath, err := c.Flags().GetString("kubeconfig")
		if err != nil {
			return fmt.Errorf("failed to get kubeconfig flag: %w", err)
		}

		kubeConfig, err := client.GetKubeConfigOrInClusterConfig(kubeconfigPath, nil)
		if err != nil {
			return fmt.Errorf("failed to get kube config: %w", err)
		}

		configClient, err := configclient.NewForConfig(kubeConfig)
		if err != nil {
			return fmt.Errorf("failed to create config client: %w", err)
		}

		configFile, tlsConfig, err := generateOperatorConfigWithTLS(operatorContext, configClient)
		if err != nil {
			return fmt.Errorf("failed to generate operator config: %w", err)
		}

		configInformerFactory := configinformers.NewSharedInformerFactory(configClient, 0)
		if err := registerAPIServerTLSWatcher(operatorContext, configInformerFactory, tlsConfig, operatorContextCancel); err != nil {
			return fmt.Errorf("failed to register APIServer TLS watcher: %w", err)
		}

		configInformerFactory.Start(operatorContext.Done())
		if !cache.WaitForCacheSync(operatorContext.Done(), configInformerFactory.Config().V1().APIServers().Informer().HasSynced) {
			return fmt.Errorf("failed to sync APIServer informer cache")
		}

		klog.Infof("Using operator config: %s", configFile)
		if err := c.Flags().Set("config", configFile); err != nil {
			return fmt.Errorf("failed to set config flag: %w", err)
		}

		return nil
	}

	return cmd
}

func generateOperatorConfigWithTLS(ctx context.Context, configClient configclient.Interface) (string, *tlsConfig, error) {
	apiServer, err := configClient.ConfigV1().APIServers().Get(ctx, apiServerCRName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return "", nil, fmt.Errorf("failed to get APIServer CR: %w", err)
		}
		apiServer = nil
	}

	tlsConfig, err := extractTLSConfigFromAPIServerObject(apiServer)
	if err != nil {
		return "", nil, fmt.Errorf("failed to extract TLS config from APIServer CR: %w", err)
	}

	if tlsConfig == nil {
		return "", nil, fmt.Errorf("tlsConfig is nil (should not happen)")
	}

	operatorConfig := &operatorv1alpha1.GenericOperatorConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.openshift.io/v1alpha1",
			Kind:       "GenericOperatorConfig",
		},
	}

	if tlsConfig.MinTLSVersion != "" || len(tlsConfig.CipherSuites) > 0 {
		servingInfo := configv1.ServingInfo{}
		if tlsConfig.MinTLSVersion != "" {
			servingInfo.MinTLSVersion = tlsConfig.MinTLSVersion
		}
		if len(tlsConfig.CipherSuites) > 0 {
			servingInfo.CipherSuites = tlsConfig.CipherSuites
		}
		operatorConfig.ServingInfo = configv1.HTTPServingInfo{
			ServingInfo: servingInfo,
		}
	}

	configFile, err := writeOperatorConfig(operatorConfig)
	if err != nil {
		return "", nil, fmt.Errorf("failed to write a config file: %w", err)
	}

	klog.Infof("Generated operator config with TLS settings: MinTLSVersion=%s, CipherSuites=%d ciphers",
		tlsConfig.MinTLSVersion, len(tlsConfig.CipherSuites))

	return configFile, tlsConfig, nil
}

// writeOperatorConfig writes the operator config to a temporary file and returns the path
func writeOperatorConfig(config *operatorv1alpha1.GenericOperatorConfig) (string, error) {
	tmpDir := os.TempDir()
	configFile := filepath.Join(tmpDir, configFileName)

	data, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("Failed to marshal operator config: %v", err)
	}

	if err := os.WriteFile(configFile, data, 0644); err != nil {
		return "", fmt.Errorf("Failed to write operator config file: %v", err)
	}

	return configFile, nil
}

func handleTLSChange(obj interface{}, currentConfig *tlsConfig, cancel context.CancelFunc) {
	apiServer, ok := obj.(*configv1.APIServer)
	if !ok {
		klog.Errorf("Expected APIServer object, got %T", obj)
		return
	}

	if apiServer.Name != apiServerCRName {
		return
	}

	newConfig, err := extractTLSConfigFromAPIServerObject(apiServer)
	if err != nil {
		klog.Errorf("Failed to extract TLS config from APIServer CR: %v", err)
		return
	}

	tlsChanged := false
	if currentConfig.MinTLSVersion != newConfig.MinTLSVersion {
		klog.Infof("APIServer MinTLSVersion changed, triggering restart")
		tlsChanged = true
	}
	if !tlsChanged && !reflect.DeepEqual(currentConfig.CipherSuites, newConfig.CipherSuites) {
		klog.Infof("APIServer CipherSuites changed, triggering restart")
		tlsChanged = true
	}

	if tlsChanged {
		klog.Info("Terminating operator to pick up new TLS configuration on restart")
		cancel()
	}
}

func registerAPIServerTLSWatcher(ctx context.Context, configInformerFactory configinformers.SharedInformerFactory, currentConfig *tlsConfig, cancel context.CancelFunc) error {
	apiServerInformer := configInformerFactory.Config().V1().APIServers().Informer()

	_, err := apiServerInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handleTLSChange(obj, currentConfig, cancel)
		},

		UpdateFunc: func(oldObj, newObj interface{}) {
			handleTLSChange(newObj, currentConfig, cancel)
		},

		// DeleteFunc handles APIServer CR deletion
		DeleteFunc: func(obj interface{}) {
			apiServer, ok := obj.(*configv1.APIServer)
			if !ok {
				// Handle tombstone case (object was deleted but we got the cached version)
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					klog.Errorf("Couldn't get object from tombstone %#v", obj)
					return
				}
				apiServer, ok = tombstone.Obj.(*configv1.APIServer)
				if !ok {
					klog.Errorf("Tombstone contained object that is not an APIServer %#v", obj)
					return
				}
			}

			if apiServer.Name != apiServerCRName {
				return
			}

			klog.Infof("APIServer CR 'cluster' was deleted, triggering restart to revert to default TLS configuration")

			// Cancel the operator context to trigger restart
			cancel()
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	klog.Info("APIServer TLS configuration watcher handlers registered")
	return nil
}

func extractTLSConfigFromAPIServerObject(apiServer *configv1.APIServer) (*tlsConfig, error) {
	existingConfig := map[string]interface{}{}

	observedConfig, errs := libgoapiserver.ObserveTLSSecurityProfile(
		&simpleAPIServerLister{lister: &singleObjectAPIServerLister{apiServer: apiServer}},
		events.NewLoggingEventRecorder("tls-config-extractor", clock.RealClock{}),
		existingConfig,
	)
	if len(errs) > 0 {
		return nil, fmt.Errorf("errors observing TLS config: %v", errs)
	}

	minTLS, _, err := unstructured.NestedString(observedConfig, "servingInfo", "minTLSVersion")
	if err != nil {
		return nil, fmt.Errorf("failed to get minTLSVersion: %w", err)
	}

	cipherSuites, _, err := unstructured.NestedStringSlice(observedConfig, "servingInfo", "cipherSuites")
	if err != nil {
		return nil, fmt.Errorf("failed to get cipherSuites: %w", err)
	}

	return &tlsConfig{
		MinTLSVersion: minTLS,
		CipherSuites:  cipherSuites,
	}, nil
}

// simpleAPIServerLister implements the configobserver.Listers interface needed by library-go
type simpleAPIServerLister struct {
	lister configlistersv1.APIServerLister
}

func (l *simpleAPIServerLister) APIServerLister() configlistersv1.APIServerLister {
	return l.lister
}

func (l *simpleAPIServerLister) ResourceSyncer() resourcesynccontroller.ResourceSyncer {
	return nil
}

func (l *simpleAPIServerLister) PreRunHasSynced() []cache.InformerSynced {
	return nil
}

// singleObjectAPIServerLister is a fake lister for tests and watcher that holds a single APIServer object
type singleObjectAPIServerLister struct {
	apiServer *configv1.APIServer
}

func (s *singleObjectAPIServerLister) Get(name string) (*configv1.APIServer, error) {
	if name == apiServerCRName && s.apiServer != nil {
		return s.apiServer, nil
	}
	return nil, apierrors.NewNotFound(
		configv1.Resource("apiservers"),
		name,
	)
}

func (s *singleObjectAPIServerLister) List(selector labels.Selector) ([]*configv1.APIServer, error) {
	if s.apiServer != nil {
		return []*configv1.APIServer{s.apiServer}, nil
	}
	return []*configv1.APIServer{}, nil
}
