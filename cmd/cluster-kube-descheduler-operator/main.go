package main

import (
	"os"

	"github.com/spf13/cobra"

	"k8s.io/component-base/cli"

	"github.com/openshift/cluster-kube-descheduler-operator/pkg/cmd/operator"
)

func main() {
	command := NewDeschedulerOperatorCommand()
	code := cli.Run(command)
	os.Exit(code)
}

func NewDeschedulerOperatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster-kube-descheduler-operator",
		Short: "OpenShift cluster kube-descheduler operator",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
			os.Exit(1)
		},
	}

	cmd.AddCommand(operator.NewOperator())
	return cmd
}
