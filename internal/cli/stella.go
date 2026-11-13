package cli

// The verbs Stella's Cloud Boxes and AgentBox drive devvm with, headless:
// devvm create (make or start a folder's VM, never a shell), devvm start,
// devvm exec, devvm forward and devvm snapshot, and --vm NAME on them and on
// stop and rm, which address a VM by its name instead of the current folder.
// See README.md, "Stella fork".

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/devvmhq/devvm/internal/backend"
	"github.com/devvmhq/devvm/internal/backend/lima"
	"github.com/devvmhq/devvm/internal/daemon"
	"github.com/devvmhq/devvm/internal/msg"
	"github.com/devvmhq/devvm/internal/platform"
	"github.com/devvmhq/devvm/internal/shell"
	"github.com/devvmhq/devvm/internal/sshconfig"
	"github.com/devvmhq/devvm/internal/state"
	"github.com/devvmhq/devvm/internal/vm"
	"github.com/devvmhq/devvm/internal/vmssh"
)

// vmFlag adds --vm NAME to cmd, which can't go with --here or --parent.
func vmFlag(cmd *cobra.Command, name *string) {
	cmd.Flags().StringVar(name, "vm", "", "the `NAME` of the VM to act on, as devvm ls lists it, instead of this folder's")
	if cmd.Flags().Lookup("here") != nil {
		exclusive(cmd, "vm", "here", "parent")
	}
}

// namedState is the state of the VM --vm names (vm.NamedVM), else of the
// folder's VM (vm.Env.FolderVM).
func namedState(ctx context.Context, a *app, name string) (*state.VM, error) {
	d, err := a.dirs()
	if err != nil {
		return nil, err
	}
	if name == "" {
		env, err := a.vmEnv()
		if err != nil {
			return nil, err
		}
		if name, err = env.FolderVM(ctx); err != nil {
			return nil, err
		}
	}
	return vm.NamedVM(d, name)
}

// toFolder makes the command act on st's VM from its folder, as its own
// (--here), for what starts it: one whose folder is gone is refused, as
// starting it would make the folder again. It runs before the run's VM
// environment is made, which reads the folder rule once.
func toFolder(a *app, st *state.VM) error {
	if vm.FolderOf(st) != vm.FolderThere {
		return msg.VMFolderGoneStart(st.Dir, st.Name())
	}
	if err := os.Chdir(st.Dir); err != nil {
		return err
	}
	a.folder.here = true
	return nil
}

// createOutput is devvm create --json.
type createOutput struct {
	VM createdVM `json:"vm"`
}

type createdVM struct {
	Name   string     `json:"name"`
	Folder string     `json:"folder"`
	Status string     `json:"status"`
	SSH    createdSSH `json:"ssh"`
}

type createdSSH struct {
	Config string `json:"config"`
	Host   string `json:"host"`
}

// newCreateCmd is devvm create: devvm without the shell.
func newCreateCmd(a *app) *cobra.Command {
	uf := new(upFlags)
	var asJSON bool
	var snapshot string
	cmd := &cobra.Command{
		Use: "create",
		// For Stella (README.md, "Stella fork"), not in devvm's help.
		Hidden: true,
		Short:  "Create this folder's VM, or start it, without opening a shell",
		Long: "Create this folder's VM as devvm does, or start it when it exists, then return instead of opening a shell, for scripts. " +
			"A new VM forwards no ports unless you give --ports: devvm forward forwards one. " +
			"--from-snapshot makes a new VM from a snapshot that devvm snapshot save made.",
		Example: examples(
			"devvm create --no-input --tools none --agents none --layers none --network internet --json", "create the VM headless and print it",
		),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o := uf.options()
			o.NoShell, o.NewPorts, o.Snapshot = true, "none", snapshot
			var made string
			err := a.env(func(env *vm.Env) error {
				if err := (vm.Launch{Env: env}).Up(cmd.Context(), o); err != nil || !asJSON {
					return err
				}
				var err error
				made, err = env.FolderVM(cmd.Context())
				return err
			}, msg.UpFailed)
			if err != nil || !asJSON {
				return err
			}
			out, err := createdOf(cmd, a, made)
			if err != nil {
				return err
			}
			return printJSON(a.u, createOutput{VM: out})
		},
	}
	addNewVMFlags(cmd.Flags(), uf, "the `PORTS` a new VM forwards: all, none or a list like 5173,8000-8010 (default: none)")
	cmd.Flags().StringVar(&snapshot, "from-snapshot", "", "make a new VM from the snapshot `ID` instead of its image")
	jsonFlag(cmd, &asJSON)
	addFolderFlags(cmd, a)
	return cmd
}

// createdOf is the VM name, as devvm ls lists it, for devvm create --json.
func createdOf(cmd *cobra.Command, a *app, name string) (createdVM, error) {
	ls, err := listVMs(cmd, a)
	if err != nil {
		return createdVM{}, err
	}
	d, err := a.dirs()
	if err != nil {
		return createdVM{}, err
	}
	reg, err := a.registry()
	if err != nil {
		return createdVM{}, err
	}
	i := slices.IndexFunc(ls, func(l vm.Listed) bool { return l.Name == name })
	if i < 0 {
		return createdVM{}, msg.RmUnknownVM(name)
	}
	l := ls[i]
	return createdVM{
		Name: l.Name, Folder: l.Folder, Status: l.Status,
		SSH: createdSSH{Config: d.SSHConfig(), Host: sshconfig.New(d, reg).Host(l.Name)},
	}, nil
}

// newStartCmd is devvm start: the VM started and set up as before its
// shell, never asking to edit ~/.ssh/config.
func newStartCmd(a *app) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use: "start",
		// For Stella (README.md, "Stella fork"), not in devvm's help.
		Hidden: true,
		Short:  "Start this folder's VM, or the one --vm names, without opening a shell",
		Long: "Start the VM and set it up as devvm does before it opens a shell, then return. It never asks to edit ~/.ssh/config. " +
			"A VM whose folder is gone isn't started, as starting it would make the folder again.",
		Example: examples(
			"devvm start --vm devvm-app-0123456789abcd", "start the VM named",
		),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name != "" {
				d, err := a.dirs()
				if err != nil {
					return err
				}
				st, err := vm.NamedVM(d, name)
				if err != nil {
					return err
				}
				if err := toFolder(a, st); err != nil {
					return err
				}
			}
			return a.env(func(env *vm.Env) error {
				return vm.Launch{Env: env}.Up(cmd.Context(), vm.Options{NoShell: true})
			}, msg.UpFailed)
		},
	}
	vmFlag(cmd, &name)
	return cmd
}

// sshTo is ssh to the VM name over devvm's own ssh config, never asking:
// the program, and its arguments without it, extra before the host.
func sshTo(a *app, name string, extra ...string) (string, []string, error) {
	d, err := a.dirs()
	if err != nil {
		return "", nil, err
	}
	reg, err := a.registry()
	if err != nil {
		return "", nil, err
	}
	path, err := exec.LookPath("ssh")
	if err != nil {
		return "", nil, err
	}
	args := append([]string{"-F", d.SSHConfig(), "-o", "BatchMode=yes"}, extra...)
	return path, append(args, sshconfig.New(d, reg).Host(name)), nil
}

// newExecCmd is devvm exec [--vm NAME] [-i] [--root] -- ARGV...
func newExecCmd(a *app) *cobra.Command {
	var name string
	var stdin, root bool
	cmd := &cobra.Command{
		Use: "exec ARGV...",
		// For Stella (README.md, "Stella fork"), not in devvm's help.
		Hidden: true,
		Short:  "Run a command in a running VM over devvm's own ssh, with its exit code",
		Long:   "Run ARGV in the running VM over devvm's own ssh config, never asking and never editing ~/.ssh/config. Its output and exit code are this command's.",
		Example: examples(
			"devvm exec --vm devvm-app-0123456789abcd --root -- cat /etc/os-release", "run a command as root in the VM named",
		),
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := namedState(cmd.Context(), a, name)
			if err != nil {
				return err
			}
			var extra []string
			if !stdin {
				extra = append(extra, "-n") // its input is the null device
			}
			path, sshArgs, err := sshTo(a, st.Name(), extra...)
			if err != nil {
				return failed(err, msg.ExecFailed)
			}
			argv := slices.Concat([]string{"ssh"}, sshArgs, []string{"--", execLine(args, root)})
			// In devvm's place: the command's input, output and exit code are
			// this one's.
			return failed(platform.Exec(path, argv, os.Environ()), msg.ExecFailed)
		},
	}
	vmFlag(cmd, &name)
	cmd.Flags().BoolVarP(&stdin, "interactive", "i", false, "pass this command's input to the command in the VM")
	cmd.Flags().BoolVar(&root, "root", false, "run it as root (sudo -n -H)")
	return cmd
}

// execLine is the remote command line ssh runs: argv quoted for the VM's
// shell, behind sudo for --root.
func execLine(argv []string, root bool) string {
	if root {
		argv = append([]string{"sudo", "-n", "-H", "--"}, argv...)
	}
	return shell.QuoteJoin(argv)
}

// forwardOutput is devvm forward --json.
type forwardOutput struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// newForwardCmd is devvm forward PORT, with --vm, --json and --close.
func newForwardCmd(a *app) *cobra.Command {
	var name string
	var asJSON, closeIt bool
	cmd := &cobra.Command{
		Use: "forward PORT",
		// For Stella (README.md, "Stella fork"), not in devvm's help.
		Hidden: true,
		Short:  "Forward a guest port of a running VM to a loopback port the computer chooses",
		Long: "Forward the VM's guest loopback PORT to 127.0.0.1 on a free port the computer chooses, so two VMs can forward the same guest port. " +
			"The forward runs in the background until the VM stops or is deleted; running it again returns the same port while it works. --close stops it.",
		Example: examples(
			"devvm forward --vm devvm-app-0123456789abcd 58790 --json", "forward the VM's port 58790 and print the host port",
		),
		Args: cobra.MatchAll(exactArgs(1), func(_ *cobra.Command, args []string) error {
			_, err := vm.ParseLinkPort(args[0])
			return err
		}),
		RunE: func(cmd *cobra.Command, args []string) error {
			guest, _ := strconv.Atoi(args[0]) // checked in Args
			st, err := namedState(cmd.Context(), a, name)
			if err != nil {
				return err
			}
			d, err := a.dirs()
			if err != nil {
				return err
			}
			pf := vmssh.NewPortForwards(d)
			if closeIt {
				return pf.Close(cmd.Context(), st.Name(), guest)
			}
			path, sshArgs, err := sshTo(a, st.Name(), "-N", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3", "-L", "")
			if err != nil {
				return failed(err, msg.ExecFailed)
			}
			host := sshArgs[len(sshArgs)-1]
			port, err := pf.Open(cmd.Context(), st.Name(), host, guest, func(local string) daemon.Cmd {
				args := slices.Clone(sshArgs)
				args[len(args)-2] = local // -L's value
				return daemon.Cmd{Path: path, Args: args}
			})
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(a.u, forwardOutput{Host: vmssh.Loopback, Port: port})
			}
			_, err = fmt.Fprintf(a.u.Stdout(), "%s:%d\n", vmssh.Loopback, port)
			return err
		},
	}
	vmFlag(cmd, &name)
	cmd.Flags().BoolVar(&closeIt, "close", false, "stop the forward instead")
	jsonFlag(cmd, &asJSON)
	return cmd
}

// snapshotOutput is devvm snapshot --json.
type snapshotOutput struct {
	Snapshots []snapshotEntry `json:"snapshots"`
}

type snapshotEntry struct {
	ID string `json:"id"`
}

// snapshotIDArg checks a snapshot's ID, the command's one argument.
func snapshotIDArg(_ *cobra.Command, args []string) error { return lima.CheckSnapshotID(args[0]) }

// snapshotter is the backend of st's VM as a backend.Snapshotter (a new
// VM's for none), refused when it can't save snapshots.
func snapshotter(a *app, st *state.VM) (backend.Backend, backend.Snapshotter, error) {
	reg, err := a.registry()
	if err != nil {
		return nil, nil, err
	}
	c, err := reg.For(st)
	if err != nil {
		return nil, nil, err
	}
	s, ok := backend.As[backend.Snapshotter](c.Backend)
	if !ok {
		return nil, nil, msg.SnapshotsUnsupported(c.Backend.Name())
	}
	return c.Backend, s, nil
}

// newSnapshotCmd is devvm snapshot: the snapshots, bare; save and rm.
func newSnapshotCmd(a *app) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use: "snapshot",
		// For Stella (README.md, "Stella fork"), not in devvm's help.
		Hidden: true,
		Short:  "List the snapshots new VMs can start from",
		Long:   "List the snapshots devvm snapshot save made, by ID. devvm create --from-snapshot makes a new VM from one.",
		Example: examples(
			"devvm snapshot --json", "list the snapshots as JSON",
		),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, s, err := snapshotter(a, new(state.VM))
			if err != nil {
				return err
			}
			ids, err := s.Snapshots(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				out := snapshotOutput{Snapshots: []snapshotEntry{}}
				for _, id := range ids {
					out.Snapshots = append(out.Snapshots, snapshotEntry{ID: id})
				}
				return printJSON(a.u, out)
			}
			for _, id := range ids {
				if _, err := fmt.Fprintln(a.u.Stdout(), id); err != nil {
					return err
				}
			}
			return nil
		},
	}
	jsonFlag(cmd, &asJSON)
	cmd.AddCommand(newSnapshotSaveCmd(a), newSnapshotRmCmd(a))
	return cmd
}

func newSnapshotSaveCmd(a *app) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use: "save ID",
		// For Stella (README.md, "Stella fork"), not in devvm's help.
		Hidden: true,
		Short:  "Save the VM as the snapshot ID, stopping it first and starting it again after",
		Long:   "Save the VM as the snapshot ID: Lima clones only a stopped VM, so a running one is stopped first and started again after. An ID that exists is refused.",
		Example: examples(
			"devvm snapshot save --vm devvm-app-0123456789abcd s1", "save the VM named as the snapshot s1",
		),
		Args: cobra.MatchAll(exactArgs(1), snapshotIDArg),
		RunE: func(cmd *cobra.Command, args []string) error {
			return saveSnapshot(cmd.Context(), a, name, args[0])
		},
	}
	vmFlag(cmd, &name)
	return cmd
}

func newSnapshotRmCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "rm ID",
		// For Stella (README.md, "Stella fork"), not in devvm's help.
		Hidden: true,
		Short:  "Delete the snapshot ID; one that isn't there is nothing to do",
		Long:   "Delete the snapshot ID. One that isn't there is nothing to do.",
		Example: examples(
			"devvm snapshot rm s1", "delete the snapshot s1",
		),
		Args: cobra.MatchAll(exactArgs(1), snapshotIDArg),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := snapshotter(a, new(state.VM))
			if err != nil {
				return err
			}
			return s.DeleteSnapshot(cmd.Context(), args[0])
		},
	}
}

// saveSnapshot saves the VM name names (the folder's for "") as the
// snapshot id: one that runs is stopped first (Lima clones only a stopped
// instance) and started again after, from its folder, which must be
// there. An id that exists is refused before anything stops.
func saveSnapshot(ctx context.Context, a *app, name, id string) error {
	st, err := namedState(ctx, a, name)
	if err != nil {
		return err
	}
	b, s, err := snapshotter(a, st)
	if err != nil {
		return err
	}
	ids, err := s.Snapshots(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(ids, id) {
		return msg.SnapshotExists(id)
	}
	in, err := b.Inspect(ctx, st.Name())
	if err != nil {
		return err
	}
	running := in.Status == backend.StatusRunning
	if running && name != "" {
		if err := toFolder(a, st); err != nil {
			return err
		}
	} else if running && vm.FolderOf(st) != vm.FolderThere {
		return msg.VMFolderGoneStart(st.Dir, st.Name())
	}
	if running {
		if err := a.env(func(env *vm.Env) error { return env.StopVM(ctx, st.Name()) }, msg.StopVMFailed); err != nil {
			return err
		}
	}
	saveErr := s.SaveSnapshot(ctx, st.Name(), id)
	if running {
		if err := a.env(func(env *vm.Env) error {
			return vm.Launch{Env: env}.Up(ctx, vm.Options{NoShell: true})
		}, msg.UpFailed); err != nil && saveErr == nil {
			return err
		}
	}
	return saveErr
}
