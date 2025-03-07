package operatorclient

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"

	operatorv1 "github.com/openshift/api/operator/v1"
	applyconfiguration "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	"github.com/openshift/library-go/pkg/apiserver/jsonpatch"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	deschedulerapplyconfiguration "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/applyconfiguration/descheduler/v1"
	operatorconfigclientv1 "github.com/openshift/cluster-kube-descheduler-operator/pkg/generated/clientset/versioned/typed/descheduler/v1"
)

const OperatorNamespace = "openshift-kube-descheduler-operator"
const OperatorConfigName = "cluster"
const OperandName = "descheduler"
const SoftTainterOperandName = "softtainter"

type DeschedulerClient struct {
	Ctx            context.Context
	SharedInformer cache.SharedIndexInformer
	OperatorClient operatorconfigclientv1.KubedeschedulersV1Interface
}

func (c *DeschedulerClient) Informer() cache.SharedIndexInformer {
	return c.SharedInformer
}

func (c *DeschedulerClient) GetOperatorState() (spec *operatorv1.OperatorSpec, status *operatorv1.OperatorStatus, resourceVersion string, err error) {
	instance, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Get(c.Ctx, OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, "", err
	}
	return &instance.Spec.OperatorSpec, &instance.Status.OperatorStatus, instance.ResourceVersion, nil
}

func (c *DeschedulerClient) GetOperatorStateWithQuorum(ctx context.Context) (*operatorv1.OperatorSpec, *operatorv1.OperatorStatus, string, error) {
	return c.GetOperatorState()
}

func (c *DeschedulerClient) UpdateOperatorSpec(ctx context.Context, resourceVersion string, spec *operatorv1.OperatorSpec) (out *operatorv1.OperatorSpec, newResourceVersion string, err error) {
	original, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Get(ctx, OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, "", err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Spec.OperatorSpec = *spec

	ret, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Update(ctx, copy, v1.UpdateOptions{})
	if err != nil {
		return nil, "", err
	}

	return &ret.Spec.OperatorSpec, ret.ResourceVersion, nil
}

func (c *DeschedulerClient) UpdateOperatorStatus(ctx context.Context, resourceVersion string, status *operatorv1.OperatorStatus) (out *operatorv1.OperatorStatus, err error) {
	original, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Get(ctx, OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Status.OperatorStatus = *status

	ret, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).UpdateStatus(ctx, copy, v1.UpdateOptions{})
	if err != nil {
		return nil, err
	}

	return &ret.Status.OperatorStatus, nil
}

func (c *DeschedulerClient) GetObjectMeta() (meta *metav1.ObjectMeta, err error) {
	instance, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Get(c.Ctx, OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &instance.ObjectMeta, nil
}

func (c *DeschedulerClient) ApplyOperatorSpec(ctx context.Context, fieldManager string, desiredConfiguration *applyconfiguration.OperatorSpecApplyConfiguration) error {
	if desiredConfiguration == nil {
		return fmt.Errorf("applyConfiguration must have a value")
	}

	desiredSpec := &deschedulerapplyconfiguration.KubeDeschedulerSpecApplyConfiguration{
		OperatorSpecApplyConfiguration: *desiredConfiguration,
	}
	desired := deschedulerapplyconfiguration.KubeDescheduler(OperatorConfigName, OperatorNamespace)
	desired.WithSpec(desiredSpec)

	instance, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Get(ctx, OperatorConfigName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
	// do nothing and proceed with the apply
	case err != nil:
		return fmt.Errorf("unable to get operator configuration: %w", err)
	default:
		original, err := deschedulerapplyconfiguration.ExtractKubeDescheduler(instance, fieldManager)
		if err != nil {
			return fmt.Errorf("unable to extract operator configuration from spec: %w", err)
		}
		if equality.Semantic.DeepEqual(original, desired) {
			return nil
		}
	}

	_, err = c.OperatorClient.KubeDeschedulers(OperatorNamespace).Apply(ctx, desired, v1.ApplyOptions{
		Force:        true,
		FieldManager: fieldManager,
	})
	if err != nil {
		return fmt.Errorf("unable to Apply for operator using fieldManager %q: %w", fieldManager, err)
	}

	return nil
}

func (c *DeschedulerClient) ApplyOperatorStatus(ctx context.Context, fieldManager string, desiredConfiguration *applyconfiguration.OperatorStatusApplyConfiguration) error {
	if desiredConfiguration == nil {
		return fmt.Errorf("applyConfiguration must have a value")
	}

	desiredStatus := &deschedulerapplyconfiguration.KubeDeschedulerStatusApplyConfiguration{
		OperatorStatusApplyConfiguration: *desiredConfiguration,
	}
	desired := deschedulerapplyconfiguration.KubeDescheduler(OperatorConfigName, OperatorNamespace)
	desired.WithStatus(desiredStatus)

	instance, err := c.OperatorClient.KubeDeschedulers(OperatorNamespace).Get(ctx, OperatorConfigName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		// do nothing and proceed with the apply
		v1helpers.SetApplyConditionsLastTransitionTime(clock.RealClock{}, &desired.Status.Conditions, nil)
	case err != nil:
		return fmt.Errorf("unable to get operator configuration: %w", err)
	default:
		original, err := deschedulerapplyconfiguration.ExtractKubeDeschedulerStatus(instance, fieldManager)
		if err != nil {
			return fmt.Errorf("unable to extract operator configuration from status: %w", err)
		}
		if equality.Semantic.DeepEqual(original, desired) {
			return nil
		}
		if original.Status != nil {
			v1helpers.SetApplyConditionsLastTransitionTime(clock.RealClock{}, &desired.Status.Conditions, original.Status.Conditions)
		} else {
			v1helpers.SetApplyConditionsLastTransitionTime(clock.RealClock{}, &desired.Status.Conditions, nil)
		}
	}

	_, err = c.OperatorClient.KubeDeschedulers(OperatorNamespace).ApplyStatus(ctx, desired, v1.ApplyOptions{
		Force:        true,
		FieldManager: fieldManager,
	})
	if err != nil {
		return fmt.Errorf("unable to ApplyStatus for operator using fieldManager %q: %w", fieldManager, err)
	}

	return nil
}

func (c *DeschedulerClient) PatchOperatorStatus(ctx context.Context, jsonPatch *jsonpatch.PatchSet) (err error) {
	jsonPatchBytes, err := jsonPatch.Marshal()
	if err != nil {
		return err
	}
	_, err = c.OperatorClient.KubeDeschedulers(OperatorNamespace).Patch(ctx, OperatorConfigName, types.JSONPatchType, jsonPatchBytes, metav1.PatchOptions{}, "/status")
	return err
}
