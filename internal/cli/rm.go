package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/devvmhq/devvm/internal/msg"
	"github.com/devvmhq/devvm/internal/vm"
)

// newRmCmd is devvm rm [VM] [--force] [--no-export] [--no-clone-check].
func newRmCmd(a *app) *cobra.Command {
	var o vm.RmOptions
	var name string
	cmd := &cobra.Command{
		Use:   "rm [VM]",
		Short: "Delete this folder's VM, or a VM whose folder is gone",
		Long: "Delete this folder's VM and what devvm keeps for it, or the VM you name when its folder is gone, after asking in a terminal without --no-input, which --force never answers when its folder is on a disk that isn't connected. " +
			"When its image has an agent, devvm first exports the agents' history, unless you give --no-export. " +
			"A --clone VM's work that this repo hasn't fetched is offered for fetching first, unless you give --no-clone-check.",
		Example: examples(
			"devvm rm", "delete this folder's VM, after asking",
			"devvm rm --force", "delete it without asking, as a script must",
			"devvm rm --no-export", "delete it without exporting the agents' history",
			"devvm rm --no-clone-check", "delete a --clone VM without checking its clone, losing its unfetched work",
			"devvm rm devvm-web-71a99b1076d92c", "delete a VM whose folder is gone, by its name as devvm ls lists it",
		),
		Args: cobra.MatchAll(maxArgs(1), noFolderFlagsWithVM, noVMFlagWithVM),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --vm (the Stella fork's) deletes the VM named: from its folder,
			// as its own, while the folder is there (the VM alone, never the
			// folder), else as devvm rm VM does.
			if name != "" {
				d, err := a.dirs()
				if err != nil {
					return err
				}
				st, err := vm.NamedVM(d, name)
				if err != nil {
					return err
				}
				if vm.FolderOf(st) == vm.FolderThere {
					if err := os.Chdir(st.Dir); err != nil {
						return err
					}
					a.folder.here = true
				} else {
					args = []string{st.Name()}
				}
			}
			if len(args) == 1 {
				name := args[0]
				return a.env(func(env *vm.Env) error { return vm.Lifecycle{Env: env}.RmVM(cmd.Context(), name, o) },
					func(reason string) msg.M { return msg.RmVMFailed(reason, name) })
			}
			return a.env(func(env *vm.Env) error { return vm.Lifecycle{Env: env}.Rm(cmd.Context(), o) }, msg.RmFailed)
		},
	}
	addForceFlag(cmd.Flags(), &o.Force)
	addNoExportFlag(cmd.Flags(), &o.NoExport)
	addNoCloneCheckFlag(cmd.Flags(), &o.NoCloneCheck)
	addFolderFlags(cmd, a)
	vmFlag(cmd, &name)
	return cmd
}

// noFolderFlagsWithVM refuses --here and --parent with a VM's name: they
// choose this folder's VM, and devvm rm VM deletes the one it names.
func noFolderFlagsWithVM(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	for _, name := range []string{"here", "parent"} {
		if cmd.Flags().Changed(name) {
			return msg.RmVMFolderFlag("--" + name)
		}
	}
	return nil
}

// noVMFlagWithVM refuses --vm with a VM's name: each names the VM.
func noVMFlagWithVM(cmd *cobra.Command, args []string) error {
	if len(args) == 1 && cmd.Flags().Changed("vm") {
		return msg.RmVMFolderFlag("--vm")
	}
	return nil
}
