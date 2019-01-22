package e2e

import (
	goctx "context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/descheduler-operator/pkg/apis"
	operator "github.com/openshift/descheduler-operator/pkg/apis/descheduler/v1alpha1"
	"github.com/openshift/descheduler-operator/pkg/controller/descheduler"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Second * 60
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

func buildDeschedulerStrategies(strategyNames []string) []operator.Strategy {
	strategies := make([]operator.Strategy, 0)
	for _, strategyName := range strategyNames {
		strategies = append(strategies, operator.Strategy{strategyName, nil})
	}
	return strategies
}

func TestDescheduler(t *testing.T) {
	deschedulerList := &operator.DeschedulerList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Descheduler",
			APIVersion: "descheduler.io/v1alpha1",
		},
	}
	err := framework.AddToFrameworkScheme(apis.AddToScheme, deschedulerList)
	if err != nil {
		t.Fatalf("failed to add custom resource scheme to framework: %v", err)
	}
	// run subtests
	t.Run("descheduler-group", func(t *testing.T) {
		t.Run("Cluster", DeschedulerCluster)
	})
}

func deschedulerStrategiesTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) error {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}
	// create descheduler custom resource
	exampleDescheduler := &operator.Descheduler{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Descheduler",
			APIVersion: "descheduler.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-descheduler",
			Namespace: namespace,
		},
		Spec: operator.DeschedulerSpec{
			Strategies: buildDeschedulerStrategies([]string{"duplicates"}),
			Schedule:   "*/1 * * * ?",
		},
	}
	err = f.Client.Create(goctx.TODO(), exampleDescheduler, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}
	// wait for policyConfigMap to be created
	err = waitForPolicyConfigMap(t, f.KubeClient, namespace, "example-descheduler", exampleDescheduler.Spec.Strategies, retryInterval, timeout)
	if err != nil {
		return err
	}
	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: "example-descheduler", Namespace: namespace}, exampleDescheduler)
	if err != nil {
		return err
	}

	exampleDescheduler.Spec.Strategies = buildDeschedulerStrategies([]string{"duplicates", "interpodantiaffinity"})

	err = f.Client.Update(goctx.TODO(), exampleDescheduler)
	if err != nil {
		return err
	}

	// wait for policy configmap creation
	err = waitForPolicyConfigMap(t, f.KubeClient, namespace, "example-descheduler", exampleDescheduler.Spec.Strategies, retryInterval, timeout)
	if err != nil {
		return err
	}

	flagsBuilt, err := descheduler.ValidateFlags(exampleDescheduler.Spec.Flags)

	if err != nil {
		return err
	}

	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: "example-descheduler", Namespace: namespace}, exampleDescheduler)
	if err != nil {
		return err
	}
	exampleDescheduler.Spec.Schedule = "*/4 * * * ?"
	err = f.Client.Update(goctx.TODO(), exampleDescheduler)
	if err != nil {
		return err
	}

	flagsBuilt, err = descheduler.ValidateFlags(exampleDescheduler.Spec.Flags)

	if err != nil {
		return err
	}
	err = waitForCronJob(t, f.KubeClient, namespace, "example-descheduler", exampleDescheduler.Spec.Schedule, exampleDescheduler.Spec.Image, append(descheduler.DeschedulerCommand, flagsBuilt...), retryInterval, timeout)
	if err != nil {
		return err
	}

	// Update descheduler flags
	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: "example-descheduler", Namespace: namespace}, exampleDescheduler)
	if err != nil {
		return err
	}

	exampleDescheduler.Spec.Flags = []operator.Param{{Name: "descheduling-interval", Value: "10"}, {Name: "dry-run", Value: "20"}, {Name: "node-selector", Value: "abc"}}
	err = f.Client.Update(goctx.TODO(), exampleDescheduler)
	if err != nil {
		return err
	}
	flagsBuilt, err = descheduler.ValidateFlags(exampleDescheduler.Spec.Flags)

	if err != nil {
		return err
	}

	err = waitForCronJob(t, f.KubeClient, namespace, "example-descheduler", exampleDescheduler.Spec.Schedule, exampleDescheduler.Spec.Image, append(descheduler.DeschedulerCommand, flagsBuilt...), retryInterval, timeout)
	if err != nil {
		return err
	}
	// Update descheduler Image
	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: "example-descheduler", Namespace: namespace}, exampleDescheduler)
	if err != nil {
		return err
	}

	exampleDescheduler.Spec.Image = "quay.io/repository/openshift/origin-descheduler"
	err = f.Client.Update(goctx.TODO(), exampleDescheduler)
	if err != nil {
		return err
	}

	flagsBuilt, err = descheduler.ValidateFlags(exampleDescheduler.Spec.Flags)
	if err != nil {
		return err
	}

	// Cronjob creation
	return waitForCronJob(t, f.KubeClient, namespace, "example-descheduler", exampleDescheduler.Spec.Schedule, exampleDescheduler.Spec.Image, append(descheduler.DeschedulerCommand, flagsBuilt...), retryInterval, timeout)
}

// waitForPolicyConfigMap to be created.
func waitForPolicyConfigMap(t *testing.T, kubeclient kubernetes.Interface, namespace, name string, strategies []operator.Strategy, retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		configMap, err := kubeclient.CoreV1().ConfigMaps(namespace).Get(name, metav1.GetOptions{IncludeUninitialized: true})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s configmap\n", name)
				return false, nil
			}
			return false, err
		}

		if configMap.Data != nil && descheduler.CheckIfPropertyChanges(strategies, configMap.Data) {
			return true, nil
		}
		t.Logf("Waiting for creation of %s policy configmap\n", name)
		return false, nil
	})
	if err != nil {
		return err
	}
	t.Log("Configmap available with different strategies")
	return nil
}

// waitFoCronJob waits for cronjob to be created.
func waitForCronJob(t *testing.T, kubeclient kubernetes.Interface, namespace, name, schedule, image string, flags []string, retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		deschedulerCronJob, err := kubeclient.BatchV1beta1().CronJobs(namespace).Get(name, metav1.GetOptions{IncludeUninitialized: true})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s cronjob\n", deschedulerCronJob.Name)
				return false, nil
			}
			return false, err
		}
		if deschedulerCronJob.Spec.Schedule == schedule {
			if len(flags) == 0 {
				return true, err
			} else {
				t.Logf("%v found descheduler jobs %v", deschedulerCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Command, flags)
				if reflect.DeepEqual(deschedulerCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Command, flags) {
					return true, err
				}
			}
		}
		if deschedulerCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image == image {
			if len(flags) == 0 {
				return true, err
			} else {
				t.Logf("%v found descheduler jobs %v", deschedulerCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Command, flags)
				if reflect.DeepEqual(deschedulerCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Command, flags) {
					return true, err
				}
			}
		}
		t.Logf("Waiting for creation of cron job %s with desired schedule failed \n", name)
		return false, nil
	})
	if err != nil {
		return err
	}
	return nil
}

func DeschedulerCluster(t *testing.T) {
	t.Parallel()
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()
	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		t.Fatalf("failed to initialize cluster resources: %v", err)
	}
	t.Log("Initialized cluster resources")
	// get global framework variables
	f := framework.Global
	if err = deschedulerStrategiesTest(t, f, ctx); err != nil {
		t.Fatal(err)
	}
}
