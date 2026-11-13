package vmssh

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/devvmhq/devvm/internal/daemon"
	"github.com/devvmhq/devvm/internal/msg"
	"github.com/devvmhq/devvm/internal/paths"
	"github.com/devvmhq/devvm/internal/platform"
	"github.com/devvmhq/devvm/internal/ports"
	"github.com/devvmhq/devvm/internal/state"
)

// PortForwards is devvm forward's forwards (the Stella fork): each one ssh
// -N -L from a loopback port the system chose to a guest loopback port of
// a running VM, detached, so two VMs can forward the same guest port. Each
// is found by its pid file and its arguments (daemon.Proc), its record
// beside it, all in the VM's run folder (paths.VM.Forwarder, -<guest>),
// and a lock for the VM's guest port is held while one is looked for,
// started or closed, so two at once never start two. Every way a VM stops
// or goes through devvm stops them (Forwards.Stop); one whose VM stopped
// otherwise ends with its ssh connection.
type PortForwards struct {
	Dirs paths.Dirs
	// Detach starts a process that outlives devvm (daemon.Detach).
	Detach daemon.Starter
	// Args returns a running process's arguments (platform.ProcessArgs).
	Args func(pid int) ([]string, error)
	// Listening reports whether a host loopback port is had
	// (ports.Listening); Free returns one the system chose.
	Listening func(ctx context.Context, port int) bool
	Free      func(ctx context.Context) (int, error)
	// Tick is how often Open looks for a new forward's port, Tries times.
	Tick  time.Duration
	Tries int
}

// NewPortForwards returns the forwards in devvm's folders d.
func NewPortForwards(d paths.Dirs) PortForwards {
	return PortForwards{
		Dirs: d, Detach: daemon.Detach, Args: platform.ProcessArgs,
		Listening: ports.Listening, Free: systemPort, Tick: 100 * time.Millisecond, Tries: 100,
	}
}

// systemPort is a loopback port the system chooses: in its ephemeral
// range, above the ports devvm gives VMs (platform.ChoosablePorts).
func systemPort(ctx context.Context) (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", Loopback+":0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// portRecord is a forward's record: the guest port, the host port its ssh
// listens on, and the ssh host it reaches the VM by.
type portRecord struct {
	Guest int    `json:"guest"`
	Port  int    `json:"port"`
	Host  string `json:"host"`
}

// SSHForward makes the ssh command of a forward with local, -L's value
// (LocalForward), its last argument the record's host.
type SSHForward func(local string) daemon.Cmd

// LocalForward is ssh -L's value for a forward from the host's port to
// the guest's, both on loopback.
func LocalForward(port, guest int) string {
	return Loopback + ":" + strconv.Itoa(port) + ":" + Loopback + ":" + strconv.Itoa(guest)
}

func (p PortForwards) file(vm string, guest int, ext string) string {
	return p.Dirs.VM(vm).Forwarder("-" + strconv.Itoa(guest) + ext)
}

// proc is rec's forward as a background process: ssh, its last argument
// rec's host and -L rec's ports.
func (p PortForwards) proc(vm string, rec portRecord) daemon.Proc {
	local := LocalForward(rec.Port, rec.Guest)
	return daemon.Proc{
		PIDFile: p.file(vm, rec.Guest, ".pid"), Args: p.Args,
		Is: func(args []string) bool {
			i := slices.Index(args, "-L")
			return len(args) > 1 && filepath.Base(args[0]) == "ssh" && args[len(args)-1] == rec.Host &&
				i > 0 && i+1 < len(args) && args[i+1] == local
		},
	}
}

// Open returns the host port of the VM's forward of guest over host,
// starting one with ssh on a port the system chooses when there's none
// that works. One that doesn't come up is stopped, and its log named.
func (p PortForwards) Open(ctx context.Context, vm, host string, guest int, ssh SSHForward) (int, error) {
	unlock, err := p.lock(ctx, vm, guest)
	if err != nil {
		return 0, err
	}
	defer unlock()
	if rec, ok := p.read(vm, guest); ok {
		if rec.Host == host && p.proc(vm, rec).Running() && p.Listening(ctx, rec.Port) {
			return rec.Port, nil
		}
		p.stop(vm, rec)
	}
	port, err := p.Free(ctx)
	if err != nil {
		return 0, err
	}
	rec := portRecord{Guest: guest, Port: port, Host: host}
	if err := p.write(vm, rec); err != nil {
		return 0, err
	}
	if p.start(ctx, vm, rec, ssh) {
		return port, nil
	}
	p.stop(vm, rec)
	return 0, msg.ForwardFailed(p.file(vm, guest, ".log"), strconv.Itoa(guest), vm)
}

// start starts rec's forward with ssh, its output in its log, and reports
// whether it came up: its port listened while it ran.
func (p PortForwards) start(ctx context.Context, vm string, rec portRecord, ssh SSHForward) bool {
	out, err := os.OpenFile(p.file(vm, rec.Guest, ".log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:forbidigo // a log, streamed by ssh into its file in devvm's own run folder
	if err != nil {
		return false
	}
	defer func() { _ = out.Close() }()
	c := ssh(LocalForward(rec.Port, rec.Guest))
	c.Out = out
	pid, err := p.proc(vm, rec).Start(p.Detach, c)
	if err != nil {
		return false
	}
	for range p.Tries {
		if p.Listening(ctx, rec.Port) {
			return true
		}
		if !daemon.Alive(pid) || ctx.Err() != nil {
			return false
		}
		time.Sleep(p.Tick)
	}
	return false
}

// Close stops the VM's forward of guest; none is nothing to do.
func (p PortForwards) Close(ctx context.Context, vm string, guest int) error {
	unlock, err := p.lock(ctx, vm, guest)
	if err != nil {
		return err
	}
	defer unlock()
	if rec, ok := p.read(vm, guest); ok {
		p.stop(vm, rec)
	}
	return nil
}

// StopAll stops every forward of the VM's, without their locks: it runs
// as the VM stops or goes, and a forward started meanwhile ends with the
// VM's ssh.
func (p PortForwards) StopAll(vm string) {
	recs, _ := filepath.Glob(p.Dirs.VM(vm).Forwarder("-*.json")) // a bad pattern: none
	for _, f := range recs {
		if rec, ok := readRecord(f); ok {
			p.stop(vm, rec)
		}
	}
}

// stop stops rec's forward, if it runs, and removes its record.
func (p PortForwards) stop(vm string, rec portRecord) {
	p.proc(vm, rec).Stop(2 * time.Second)
	for _, ext := range []string{".pid", ".json"} {
		_ = os.Remove(p.file(vm, rec.Guest, ext))
	}
}

func (p PortForwards) lock(ctx context.Context, vm string, guest int) (func(), error) {
	if err := state.PrivateDir(p.Dirs.VM(vm).Run()); err != nil {
		return nil, err
	}
	return platform.LockFile(ctx, p.file(vm, guest, ".lock"))
}

func (p PortForwards) write(vm string, rec portRecord) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return state.WriteFile(p.file(vm, rec.Guest, ".json"), body)
}

func (p PortForwards) read(vm string, guest int) (portRecord, bool) {
	rec, ok := readRecord(p.file(vm, guest, ".json"))
	return rec, ok && rec.Guest == guest
}

// readRecord reads a forward's record; ok is false for none, or one that
// isn't one.
func readRecord(path string) (portRecord, bool) {
	body, err := os.ReadFile(path) //nolint:gosec // devvm's own run folder
	if err != nil {
		return portRecord{}, false
	}
	var rec portRecord
	if err := json.Unmarshal(body, &rec); err != nil || rec.Port <= 0 || rec.Guest <= 0 || rec.Host == "" {
		return portRecord{}, false
	}
	return rec, true
}
