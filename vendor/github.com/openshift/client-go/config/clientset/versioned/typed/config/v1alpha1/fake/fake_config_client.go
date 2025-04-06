// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1alpha1"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeConfigV1alpha1 struct {
	*testing.Fake
}

func (c *FakeConfigV1alpha1) Backups() v1alpha1.BackupInterface {
	return newFakeBackups(c)
}

func (c *FakeConfigV1alpha1) ClusterImagePolicies() v1alpha1.ClusterImagePolicyInterface {
	return newFakeClusterImagePolicies(c)
}

func (c *FakeConfigV1alpha1) ClusterMonitorings() v1alpha1.ClusterMonitoringInterface {
	return newFakeClusterMonitorings(c)
}

func (c *FakeConfigV1alpha1) ImagePolicies(namespace string) v1alpha1.ImagePolicyInterface {
	return newFakeImagePolicies(c, namespace)
}

func (c *FakeConfigV1alpha1) InsightsDataGathers() v1alpha1.InsightsDataGatherInterface {
	return newFakeInsightsDataGathers(c)
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeConfigV1alpha1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
