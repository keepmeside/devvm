package cli

import (
	"github.com/spf13/cobra"

	"github.com/devvmhq/devvm/internal/msg"
	"github.com/devvmhq/devvm/internal/vm"
)

// newStopCmd is devvm stop.
func newStopCmd(a *app) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop this folder's VM",
		Long: "Stop this folder's VM and its proxy. " +
			"devvm, devvm restart or a command that needs the VM running starts it again.",
		Example: examples(
			"devvm stop", "stop this folder's VM",
		),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := toVM(a, name); err != nil {
				return err
			}
			return a.env(func(env *vm.Env) error { return vm.Lifecycle{Env: env}.Stop(cmd.Context()) }, msg.StopVMFailed)
		},
	}
	addFolderFlags(cmd, a)
	vmFlag(cmd, &name)
	return cmd
}
