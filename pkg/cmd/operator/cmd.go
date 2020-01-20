package operator

import (
	"github.com/spf13/cobra"

	"github.com/openshift/cluster-kube-descheduler-operator/pkg/operator"
	"github.com/openshift/cluster-kube-descheduler-operator/pkg/version"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
)

func NewOperator() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("openshift-cluster-kube-descheduler-operator", version.Get(), operator.RunOperator).
		NewCommand()
	cmd.Use = "operator"
	cmd.Short = "Start the Cluster kube-descheduler Operator"

	return cmd
}
