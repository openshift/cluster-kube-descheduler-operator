package operatorclient

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	operatorconfigclientv1beta1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned/typed/descheduler/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

const OperatorNamespace = "openshift-kube-descheduler-operator"

type DeschedulerClient struct {
	SharedInformer cache.SharedIndexInformer
	OperatorClient operatorconfigclientv1beta1.KubedeschedulersV1beta1Interface
}

func (c *DeschedulerClient) Informer() cache.SharedIndexInformer {
	return c.SharedInformer
}

func (c *DeschedulerClient) GetOperatorState() (spec *operatorv1.OperatorSpec, status *operatorv1.OperatorStatus, resourceVersion string, err error) {
	instance, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Get("cluster", metav1.GetOptions{})
	if err != nil {
		return nil, nil, "", err
	}
	return &instance.Spec.OperatorSpec, &instance.Status.OperatorStatus, instance.ResourceVersion, nil
}

func (c *DeschedulerClient) UpdateOperatorSpec(resourceVersion string, spec *operatorv1.OperatorSpec) (out *operatorv1.OperatorSpec, newResourceVersion string, err error) {
	original, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Get("cluster", metav1.GetOptions{})
	if err != nil {
		return nil, "", err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Spec.OperatorSpec = *spec

	ret, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Update(copy)
	if err != nil {
		return nil, "", err
	}

	return &ret.Spec.OperatorSpec, ret.ResourceVersion, nil
}

func (c *DeschedulerClient) UpdateOperatorStatus(resourceVersion string, status *operatorv1.OperatorStatus) (out *operatorv1.OperatorStatus, err error) {
	original, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Get("cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Status.OperatorStatus = *status

	ret, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).UpdateStatus(copy)
	if err != nil {
		return nil, err
	}

	return &ret.Status.OperatorStatus, nil
}
