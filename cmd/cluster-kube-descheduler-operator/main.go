package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/openshift/cluster-kube-descheduler-operator/pkg/cmd/operator"
)

func main() {
	command := NewDeschedulerOperatorCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
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
