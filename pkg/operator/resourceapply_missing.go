package operator

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	admissionregistrationclientv1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
)

// TODO: delete once available in github.com/openshift/library-go
// see: https://github.com/openshift/library-go/pull/1954

func DeleteValidatingAdmissionPolicyV1(ctx context.Context, client admissionregistrationclientv1.ValidatingAdmissionPoliciesGetter, recorder events.Recorder, required *admissionregistrationv1.ValidatingAdmissionPolicy) (*admissionregistrationv1.ValidatingAdmissionPolicy, bool, error) {
	err := client.ValidatingAdmissionPolicies().Delete(ctx, required.Name, metav1.DeleteOptions{})
	if err != nil && apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	resourcehelper.ReportDeleteEvent(recorder, required, err)
	return nil, true, nil
}

func DeleteValidatingAdmissionPolicyBindingV1(ctx context.Context, client admissionregistrationclientv1.ValidatingAdmissionPolicyBindingsGetter, recorder events.Recorder, required *admissionregistrationv1.ValidatingAdmissionPolicyBinding) (*admissionregistrationv1.ValidatingAdmissionPolicyBinding, bool, error) {
	err := client.ValidatingAdmissionPolicyBindings().Delete(ctx, required.Name, metav1.DeleteOptions{})
	if err != nil && apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	resourcehelper.ReportDeleteEvent(recorder, required, err)
	return nil, true, nil
}
