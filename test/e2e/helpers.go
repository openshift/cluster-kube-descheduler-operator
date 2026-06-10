package e2e

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	o "github.com/onsi/gomega"
	routeclientv1 "github.com/openshift/client-go/route/clientset/versioned"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringclient "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apiextclientv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/openshift/cluster-kube-descheduler-operator/bindata"
	deschclient "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator/operatorclient"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
)

// getRestConfig returns a REST config for the Kubernetes cluster
func getRestConfig() *rest.Config {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	o.Expect(err).NotTo(o.HaveOccurred(), "should build kubeconfig")
	return config
}

// GetKubeClient returns a Kubernetes clientset or fails the test
func GetKubeClient() *k8sclient.Clientset {
	config := getRestConfig()
	client, err := k8sclient.NewForConfig(config)
	o.Expect(err).NotTo(o.HaveOccurred(), "should create kubernetes client")
	return client
}

// GetApiExtensionClient returns an API extension clientset or fails the test
func GetApiExtensionClient() *apiextclientv1.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	o.Expect(err).NotTo(o.HaveOccurred(), "should build kubeconfig")

	client, err := apiextclientv1.NewForConfig(config)
	o.Expect(err).NotTo(o.HaveOccurred(), "should create API extension client")

	return client
}

// GetDeschedulerClient returns a Descheduler clientset or fails the test
func GetDeschedulerClient() *deschclient.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	o.Expect(err).NotTo(o.HaveOccurred(), "should build kubeconfig")

	client, err := deschclient.NewForConfig(config)
	o.Expect(err).NotTo(o.HaveOccurred(), "should create Descheduler client")

	return client
}

// GetRouteClient returns an OpenShift Route clientset or fails the test
func GetRouteClient() *routeclientv1.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	o.Expect(err).NotTo(o.HaveOccurred(), "should build kubeconfig")

	client, err := routeclientv1.NewForConfig(config)
	o.Expect(err).NotTo(o.HaveOccurred(), "should create Route client")

	return client
}

// GetMonitoringClient returns a Prometheus Operator monitoring clientset or fails the test
func GetMonitoringClient() *monitoringclient.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	o.Expect(err).NotTo(o.HaveOccurred(), "should build kubeconfig")

	client, err := monitoringclient.NewForConfig(config)
	o.Expect(err).NotTo(o.HaveOccurred(), "should create Monitoring client")

	return client
}

// GetDynamicClient returns a dynamic client for accessing arbitrary resources or fails the test
func GetDynamicClient() dynamic.Interface {
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	o.Expect(err).NotTo(o.HaveOccurred(), "should build kubeconfig")

	client, err := dynamic.NewForConfig(config)
	o.Expect(err).NotTo(o.HaveOccurred(), "should create Dynamic client")

	return client
}

// getMetricsService returns the metrics service object from bindata
func getMetricsService() *corev1.Service {
	serviceBytes := bindata.MustAsset("assets/kube-descheduler/service.yaml")
	return resourceread.ReadServiceV1OrDie(serviceBytes)
}

// getServiceMonitor returns the service monitor object from bindata
func getServiceMonitor() *monitoringv1.ServiceMonitor {
	serviceMonitorBytes := bindata.MustAsset("assets/kube-descheduler/servicemonitor.yaml")

	// Create a scheme with monitoring types
	scheme := runtime.NewScheme()
	_ = monitoringv1.AddToScheme(scheme)

	// Create a decoder
	decode := serializer.NewCodecFactory(scheme).UniversalDeserializer().Decode

	// Decode the YAML
	obj, _, err := decode(serviceMonitorBytes, nil, nil)
	if err != nil {
		panic(err)
	}

	return obj.(*monitoringv1.ServiceMonitor)
}

// getDeschedulerDeployment returns the deployment object from bindata
func getDeschedulerDeployment() *appsv1.Deployment {
	deploymentBytes := bindata.MustAsset("assets/kube-descheduler/deployment.yaml")
	return resourceread.ReadDeploymentV1OrDie(deploymentBytes)
}

// PrometheusQueryResponse represents the response from Prometheus /api/v1/query endpoint
type PrometheusQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string                  `json:"resultType"`
		Result     []PrometheusQueryResult `json:"result"`
	} `json:"data"`
}

// PrometheusQueryResult represents a single result from a Prometheus query
type PrometheusQueryResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"`
}

// getPrometheusToken retrieves the token for authenticating with Prometheus
func getPrometheusToken(ctx context.Context, kubeClient *k8sclient.Clientset, podLabels map[string]string) (string, error) {
	// Get the token from the descheduler pod's service account
	// This service account should have permissions to access Prometheus metrics

	// Build label selector from pod labels (should be exactly one label)
	var labelSelector string
	for key, value := range podLabels {
		labelSelector = fmt.Sprintf("%s=%s", key, value)
		break // Only one label expected
	}

	// First, get the descheduler pod to find out which service account it's using
	pods, err := kubeClient.CoreV1().Pods(operatorclient.OperatorNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list descheduler pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no descheduler pods found with label selector %s", labelSelector)
	}

	pod := pods.Items[0]
	serviceAccountName := pod.Spec.ServiceAccountName
	if serviceAccountName == "" {
		serviceAccountName = "default"
	}

	klog.Infof("Using service account '%s' from pod '%s'", serviceAccountName, pod.Name)

	// Create a token request for the service account (Kubernetes 1.24+)
	treq := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: func() *int64 { i := int64(3600); return &i }(),
		},
	}

	tokenRequest, err := kubeClient.CoreV1().ServiceAccounts(operatorclient.OperatorNamespace).CreateToken(
		ctx,
		serviceAccountName,
		treq,
		metav1.CreateOptions{},
	)
	if err != nil {
		klog.Warningf("Failed to create token request: %v, trying to read from secrets", err)
		// Fallback: try to get token from secrets
		return getTokenFromSecrets(ctx, kubeClient, operatorclient.OperatorNamespace, serviceAccountName)
	}

	if tokenRequest.Status.Token != "" {
		klog.Infof("Successfully created token for service account %s", serviceAccountName)
		return tokenRequest.Status.Token, nil
	}

	return "", fmt.Errorf("token request succeeded but token is empty for service account %s", serviceAccountName)
}

// getTokenFromSecrets tries to get a service account token from secrets
func getTokenFromSecrets(ctx context.Context, kubeClient *k8sclient.Clientset, namespace, serviceAccountName string) (string, error) {
	sa, err := kubeClient.CoreV1().ServiceAccounts(namespace).Get(ctx, serviceAccountName, metav1.GetOptions{})
	if err != nil {
		klog.Warningf("Failed to get service account %s: %v", serviceAccountName, err)
		return "", nil
	}

	// Try to get token from secrets
	if len(sa.Secrets) > 0 {
		for _, secretRef := range sa.Secrets {
			secret, err := kubeClient.CoreV1().Secrets(namespace).Get(ctx, secretRef.Name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			if token, ok := secret.Data["token"]; ok && len(token) > 0 {
				klog.Infof("Found token from secret %s", secretRef.Name)
				return string(token), nil
			}
		}
	}

	klog.Warningf("No token found in secrets for service account %s", serviceAccountName)
	return "", nil
}

// queryPrometheus performs a GET request to Prometheus API and returns the response body
func queryPrometheus(ctx context.Context, routeClient *routeclientv1.Clientset, token, path string) ([]byte, error) {
	// Get the prometheus-k8s route from openshift-monitoring namespace
	prometheusURL, err := getPrometheusURL(ctx, routeClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get Prometheus route: %w", err)
	}

	// Construct the full URL
	fullURL := fmt.Sprintf("https://%s%s", prometheusURL, path)
	klog.Infof("Querying Prometheus at: %s", fullURL)

	// Create HTTP client with TLS skip verify (for self-signed certs)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add bearer token if available
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query Prometheus API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Prometheus API returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// queryPrometheusTarget queries the Prometheus API for a specific target using the 'up' metric
// This is more efficient than fetching all targets as it queries only the specific job
func queryPrometheusTarget(ctx context.Context, kubeClient *k8sclient.Clientset, routeClient *routeclientv1.Clientset, token string, jobName string, instance string) (*PrometheusQueryResponse, error) {
	// Use the 'up' metric to check if the target is available
	// The 'up' metric is automatically created by Prometheus for each scraped target
	query := fmt.Sprintf("up{%s}", fmt.Sprintf("namespace=\"%s\",instance=\"%s\"", operatorclient.OperatorNamespace, instance))
	return queryPrometheusMetric(ctx, kubeClient, routeClient, token, query)
}

// getPrometheusURL retrieves the Prometheus URL from the route
func getPrometheusURL(ctx context.Context, routeClient *routeclientv1.Clientset) (string, error) {
	// Get the prometheus-k8s route from openshift-monitoring namespace
	route, err := routeClient.RouteV1().Routes(operator.PromNamespace).Get(ctx, operator.PromRouteName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to get %s/%s route: %w", operator.PromNamespace, operator.PromRouteName, err)
	}

	// Validate route has ingress information
	if len(route.Status.Ingress) == 0 {
		return "", fmt.Errorf("no ingress found in %s/%s route", operator.PromNamespace, operator.PromRouteName)
	}

	if route.Status.Ingress[0].Host == "" {
		return "", fmt.Errorf("host for status.ingress[0] in %s/%s route is empty", operator.PromNamespace, operator.PromRouteName)
	}

	host := route.Status.Ingress[0].Host
	klog.Infof("Found Prometheus route host: %s", host)
	return host, nil
}

// queryPrometheusMetric queries Prometheus for a specific metric
func queryPrometheusMetric(ctx context.Context, kubeClient *k8sclient.Clientset, routeClient *routeclientv1.Clientset, token string, query string) (*PrometheusQueryResponse, error) {
	path := fmt.Sprintf("/api/v1/query?query=%s", url.QueryEscape(query))
	body, err := queryPrometheus(ctx, routeClient, token, path)
	if err != nil {
		return nil, err
	}

	var queryResp PrometheusQueryResponse
	if err := json.Unmarshal(body, &queryResp); err != nil {
		return nil, fmt.Errorf("failed to decode Prometheus response: %w", err)
	}

	return &queryResp, nil
}

// getEndpointAddressesForService gets all endpoint addresses from EndpointSlices matching the service labels
func getEndpointAddressesForService(ctx context.Context, kubeClient *k8sclient.Clientset, namespace string, serviceLabels map[string]string) ([]string, error) {
	// Build label selector from service labels
	labelSelector := metav1.FormatLabelSelector(&metav1.LabelSelector{
		MatchLabels: serviceLabels,
	})

	// List EndpointSlices matching the service labels
	endpointSlices, err := kubeClient.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list EndpointSlices: %w", err)
	}

	// Collect all unique endpoint addresses
	addressSet := make(map[string]bool)
	for _, slice := range endpointSlices.Items {
		for _, endpoint := range slice.Endpoints {
			// Only include ready endpoints
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				continue
			}
			for _, address := range endpoint.Addresses {
				addressSet[address] = true
			}
		}
	}

	// Convert set to slice
	addresses := make([]string, 0, len(addressSet))
	for addr := range addressSet {
		addresses = append(addresses, addr)
	}

	return addresses, nil
}
