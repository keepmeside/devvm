package cli

// The verbs Stella's Cloud Boxes and AgentBox drive devvm with, headless:
// devvm create (make or start a folder's VM, never a shell), devvm start,
// devvm exec, devvm forward and devvm snapshot, and --vm NAME on them and on
// stop and rm, which address a VM by its name instead of the current folder.
// See README.md, "Stella fork".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/devvmhq/devvm/internal/backend/lima"
	"github.com/devvmhq/devvm/internal/daemon"
	"github.com/devvmhq/devvm/internal/msg"
	"github.com/devvmhq/devvm/internal/paths"
	"github.com/devvmhq/devvm/internal/platform"
	"github.com/devvmhq/devvm/internal/shell"
	"github.com/devvmhq/devvm/internal/sshconfig"
	"github.com/devvmhq/devvm/internal/state"
	"github.com/devvmhq/devvm/internal/vm"
)

// vmFlag adds --vm NAME to cmd.
func vmFlag(cmd *cobra.Command, name *string) {
	cmd.Flags().StringVar(name, "vm", "", "the `NAME` of the VM to act on, as devvm ls lists it, instead of this folder's")
}

// toVM makes the command act on the VM named name: it runs in that VM's
// folder, on the folder's own VM (--here). An empty name leaves the folder
// as it is.
func toVM(a *app, name string) error {
	if name == "" {
		return nil
	}
	dir, err := vmDir(a, name)
	if err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("the folder of %s, %s: %w", name, dir, err)
	}
	a.folder.here = true
	return nil
}

// vmDir is the folder of the VM named name.
func vmDir(a *app, name string) (string, error) {
	d, err := a.dirs()
	if err != nil {
		return "", err
	}
	inv, err := vm.ReadStates(d)
	if err != nil {
		return "", err
	}
	for _, st := range inv.States() {
		if st.Name() == name {
			return st.Dir, nil
		}
	}
	return "", fmt.Errorf("no VM %s", name)
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
			"--from-snapshot makes a new VM from a snapshot that devvm snapshot save made.",
		Example: examples(
			"devvm create --no-input --tools none --agents none --layers none --network internet --json", "create the VM headless and print it",
		),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o := uf.options()
			o.NoShell = true
			if snapshot != "" {
				if err := lima.CheckSnapshotID(snapshot); err != nil {
					return err
				}
				o.Snapshot = snapshot
			}
			err := a.env(func(env *vm.Env) error {
				return vm.Launch{Env: env}.Up(cmd.Context(), o)
			}, msg.UpFailed)
			if err != nil {
				return err
			}
			if !asJSON {
				return nil
			}
			out, err := createdHere(cmd, a)
			if err != nil {
				return err
			}
			return printJSON(a.u, createOutput{VM: out})
		},
	}
	addNewVMFlags(cmd.Flags(), uf)
	cmd.Flags().StringVar(&snapshot, "from-snapshot", "", "make a new VM from the snapshot `ID` instead of its image")
	jsonFlag(cmd, &asJSON)
	addFolderFlags(cmd, a)
	return cmd
}

// createdHere is the VM of the current folder, as devvm ls sees it.
func createdHere(cmd *cobra.Command, a *app) (createdVM, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return createdVM{}, err
	}
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
	for _, l := range ls {
		if samePath(l.Folder, cwd) {
			return createdVM{
				Name: l.Name, Folder: l.Folder, Status: l.Status,
				SSH: createdSSH{Config: d.SSHConfig(), Host: sshconfig.New(d, reg).Host(l.Name)},
			}, nil
		}
	}
	return createdVM{}, fmt.Errorf("no VM for %s", cwd)
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
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
		Long:   "Start the VM and set it up as devvm does before it opens a shell, then return. It never asks to edit ~/.ssh/config.",
		Example: examples(
			"devvm start --vm devvm-app-0123456789abcd", "start the VM named",
		),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := toVM(a, name); err != nil {
				return err
			}
			return a.env(func(env *vm.Env) error {
				return vm.Launch{Env: env}.Up(cmd.Context(), vm.Options{NoShell: true})
			}, msg.UpFailed)
		},
	}
	vmFlag(cmd, &name)
	return cmd
}

// sshTo is ssh to the running VM name over devvm's own ssh config, never
// asking: argv without the program.
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
	args := []string{"-F", d.SSHConfig(), "-o", "BatchMode=yes"}
	args = append(args, extra...)
	return path, append(args, sshconfig.New(d, reg).Host(name)), nil
}

// namedVM is the VM --vm names, else the current folder's.
func namedVM(cmd *cobra.Command, a *app, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	here, err := createdHere(cmd, a)
	return here.Name, err
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
			n, err := namedVM(cmd, a, name)
			if err != nil {
				return err
			}
			var extra []string
			if !stdin {
				extra = append(extra, "-n") // its input is the null device
			}
			path, sshArgs, err := sshTo(a, n, extra...)
			if err != nil {
				return err
			}
			argv := append([]string{"ssh"}, sshArgs...)
			argv = append(argv, "--", execLine(args, root))
			// In devvm's place: the command's input, output and exit code are
			// this one's.
			return platform.Exec(path, argv, os.Environ())
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

// forwardRecord is what devvm forward keeps for one VM's guest port.
type forwardRecord struct {
	Port int `json:"port"`
	PID  int `json:"pid"`
}

// forwards keeps the records of devvm forward, one file each.
type forwards struct {
	dir string
	// alive and listening tell a record that still works; start opens a
	// forward of guest on port and returns its pid.
	alive     func(pid int) bool
	listening func(port int) bool
	start     func(port int) (int, error)
	free      func() (int, error)
}

func (f forwards) path(vmName string, guest int) string {
	return filepath.Join(f.dir, vmName+"-"+strconv.Itoa(guest)+".json")
}

// open returns the host port of the VM's forward of guest, opening one on
// a port the OS chooses when there's none that works.
func (f forwards) open(vmName string, guest int) (int, error) {
	if rec, ok := f.read(vmName, guest); ok && f.alive(rec.PID) && f.listening(rec.Port) {
		return rec.Port, nil
	}
	port, err := f.free()
	if err != nil {
		return 0, err
	}
	pid, err := f.start(port)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return 0, err
	}
	body, err := json.Marshal(forwardRecord{Port: port, PID: pid})
	if err != nil {
		return 0, err
	}
	return port, state.WriteFile(f.path(vmName, guest), body)
}

// close stops the VM's forward of guest; none is nothing to do.
func (f forwards) close(vmName string, guest int) error {
	rec, ok := f.read(vmName, guest)
	if !ok {
		return nil
	}
	if f.alive(rec.PID) {
		_ = daemon.Signal(rec.PID, 15)
	}
	err := os.Remove(f.path(vmName, guest))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (f forwards) read(vmName string, guest int) (forwardRecord, bool) {
	body, err := os.ReadFile(f.path(vmName, guest))
	if err != nil {
		return forwardRecord{}, false
	}
	var rec forwardRecord
	if json.Unmarshal(body, &rec) != nil || rec.Port <= 0 {
		return forwardRecord{}, false
	}
	return rec, true
}

// freeLoopbackPort is a loopback port the OS chooses.
func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func listening(port int) bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
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
			"The forward runs in the background and ends with the VM; running it again returns the same port while it works. --close stops it.",
		Example: examples(
			"devvm forward --vm devvm-app-0123456789abcd 58790 --json", "forward the VM's port 58790 and print the host port",
		),
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			guest, err := strconv.Atoi(args[0])
			if err != nil || guest < 1 || guest > 65535 {
				return fmt.Errorf("%q isn't a port from 1 to 65535", args[0])
			}
			n, err := namedVM(cmd, a, name)
			if err != nil {
				return err
			}
			d, err := a.dirs()
			if err != nil {
				return err
			}
			f := forwards{dir: forwardsDir(d), alive: daemon.Alive, listening: listening, free: freeLoopbackPort}
			if closeIt {
				return f.close(n, guest)
			}
			f.start = func(port int) (int, error) { return startForward(a, n, port, guest) }
			port, err := f.open(n, guest)
			if err != nil {
				return fmt.Errorf("%s isn't running, or its port %d can't be forwarded: %w", n, guest, err)
			}
			if asJSON {
				return printJSON(a.u, forwardOutput{Host: "127.0.0.1", Port: port})
			}
			fmt.Fprintf(a.u.Stdout(), "127.0.0.1:%d\n", port)
			return nil
		},
	}
	vmFlag(cmd, &name)
	cmd.Flags().BoolVar(&closeIt, "close", false, "stop the forward instead")
	jsonFlag(cmd, &asJSON)
	return cmd
}

func forwardsDir(d paths.Dirs) string { return paths.StellaForwards(d) }

// startForward starts ssh -N -L in the background and waits until port
// answers.
func startForward(a *app, vmName string, port, guest int) (int, error) {
	path, args, err := sshTo(a, vmName,
		"-N", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3",
		"-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", port, guest))
	if err != nil {
		return 0, err
	}
	pid, err := daemon.Detach(daemon.Cmd{Path: path, Args: args})
	if err != nil {
		return 0, err
	}
	for range 100 {
		if listening(port) {
			return pid, nil
		}
		if !daemon.Alive(pid) {
			return 0, errors.New("ssh ended before the forward was up")
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = daemon.Signal(pid, 15)
	return 0, errors.New("the forward didn't come up within 10 seconds")
}

// snapshotOutput is devvm snapshot --json.
type snapshotOutput struct {
	Snapshots []lima.Snapshot `json:"snapshots"`
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
			d, err := a.dirs()
			if err != nil {
				return err
			}
			all, err := lima.Snapshots(cmd.Context(), d)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(a.u, snapshotOutput{Snapshots: all})
			}
			for _, s := range all {
				fmt.Fprintln(a.u.Stdout(), s.ID)
			}
			return nil
		},
	}
	jsonFlag(cmd, &asJSON)
	var name string
	save := &cobra.Command{
		Use: "save ID",
		// For Stella (README.md, "Stella fork"), not in devvm's help.
		Hidden: true,
		Short:  "Save the VM as the snapshot ID, stopping it first and starting it again after",
		Long:   "Save the VM as the snapshot ID: Lima clones only a stopped VM, so a running one is stopped first and started again after. An ID that exists is refused.",
		Example: examples(
			"devvm snapshot save --vm devvm-app-0123456789abcd s1", "save the VM named as the snapshot s1",
		),
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := lima.CheckSnapshotID(id); err != nil {
				return err
			}
			if err := toVM(a, name); err != nil {
				return err
			}
			n, err := namedVM(cmd, a, name)
			if err != nil {
				return err
			}
			return saveSnapshot(cmd.Context(), a, n, id)
		},
	}
	vmFlag(save, &name)
	rm := &cobra.Command{
		Use: "rm ID",
		// For Stella (README.md, "Stella fork"), not in devvm's help.
		Hidden: true,
		Short:  "Delete the snapshot ID; one that isn't there is nothing to do",
		Long:   "Delete the snapshot ID. One that isn't there is nothing to do.",
		Example: examples(
			"devvm snapshot rm s1", "delete the snapshot s1",
		),
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := a.dirs()
			if err != nil {
				return err
			}
			return lima.DeleteSnapshot(cmd.Context(), d, args[0])
		},
	}
	cmd.AddCommand(save, rm)
	return cmd
}

// saveSnapshot stops the VM when it runs (Lima clones only a stopped
// instance), clones it, and starts it again if it was running.
func saveSnapshot(ctx context.Context, a *app, vmName, id string) error {
	d, err := a.dirs()
	if err != nil {
		return err
	}
	if err := lima.CheckSnapshotID(id); err != nil {
		return err
	}
	// An ID that exists is refused before the VM is stopped.
	existing, err := lima.Snapshots(ctx, d)
	if err != nil {
		return err
	}
	for _, s := range existing {
		if s.ID == id {
			return lima.ErrSnapshotExists
		}
	}
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	ls, err := listVMs(cmd, a)
	if err != nil {
		return err
	}
	running := false
	for _, l := range ls {
		if l.Name == vmName {
			running = l.Status == "Running"
		}
	}
	if running {
		if err := a.env(func(env *vm.Env) error { return vm.Lifecycle{Env: env}.Stop(ctx) }, msg.StopVMFailed); err != nil {
			return err
		}
	}
	saveErr := lima.SaveSnapshot(ctx, d, vmName, id)
	if running {
		if err := a.env(func(env *vm.Env) error {
			return vm.Launch{Env: env}.Up(ctx, vm.Options{NoShell: true})
		}, msg.UpFailed); err != nil && saveErr == nil {
			return err
		}
	}
	return saveErr
}
