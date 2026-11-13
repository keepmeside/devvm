package vmssh

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/devvmhq/devvm/internal/daemon"
	"github.com/devvmhq/devvm/internal/paths"
)

// testPF is PortForwards whose ssh processes are the test's
// (sleep), each listening on its port once started (listening), on ports
// counted from 47000; it counts the starts. A process it didn't start
// has arguments that aren't ssh's.
type testPF struct {
	PortForwards
	mu        sync.Mutex
	procs     map[int][]string
	listening map[int]bool
	starts    int
	next      int
}

func newTestPF(t *testing.T) *testPF {
	t.Helper()
	tp := &testPF{procs: map[int][]string{}, listening: map[int]bool{}, next: 47000}
	tp.PortForwards = PortForwards{
		Dirs: paths.Dirs{Data: t.TempDir()}, Tick: time.Millisecond, Tries: 20,
		Detach: func(c daemon.Cmd) (int, error) {
			cmd := exec.CommandContext(t.Context(), "sleep", "30")
			if err := cmd.Start(); err != nil {
				return 0, err
			}
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
			time.Sleep(20 * time.Millisecond) // a start that takes a while, as ssh's
			tp.mu.Lock()
			defer tp.mu.Unlock()
			tp.starts++
			args := append([]string{c.Path}, c.Args...)
			tp.procs[cmd.Process.Pid] = args
			tp.listening[portOf(args)] = true
			return cmd.Process.Pid, nil
		},
		Args: func(pid int) ([]string, error) {
			tp.mu.Lock()
			defer tp.mu.Unlock()
			if a, ok := tp.procs[pid]; ok {
				return a, nil
			}
			return []string{"sleep", "30"}, nil
		},
		Listening: func(_ context.Context, port int) bool {
			tp.mu.Lock()
			defer tp.mu.Unlock()
			return tp.listening[port]
		},
		Free: func(context.Context) (int, error) {
			tp.mu.Lock()
			defer tp.mu.Unlock()
			tp.next++
			return tp.next, nil
		},
	}
	return tp
}

// portOf is the host port in a test ssh's -L.
func portOf(args []string) int {
	for i, a := range args {
		if a == "-L" && i+1 < len(args) {
			for p := 47000; p < 48000; p++ {
				if args[i+1] == LocalForward(p, 58790) {
					return p
				}
			}
		}
	}
	return 0
}

func sshFor(host string) SSHForward {
	return func(local string) daemon.Cmd {
		return daemon.Cmd{Path: "/usr/bin/ssh", Args: []string{"-N", "-L", local, host}}
	}
}

const (
	pfVM1 = "devvm-box1-91e3e4ca0febc0"
	pfVM2 = "devvm-box2-0123456789abcd"
)

// Two VMs' forwards of one guest port get two host ports, neither the
// guest's; asked again while it works, the same port; its ssh gone, a new
// one; closed, it's stopped, and closing twice is nothing.
func TestPortForwardsOpenAndClose(t *testing.T) {
	tp := newTestPF(t)
	ctx := t.Context()
	one, err := tp.Open(ctx, pfVM1, "devvm-box1", 58790, sshFor("devvm-box1"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := tp.Open(ctx, pfVM2, "devvm-box2", 58790, sshFor("devvm-box2"))
	if err != nil {
		t.Fatal(err)
	}
	if one == two || one == 58790 || two == 58790 {
		t.Fatalf("ports %d and %d: want two distinct ports other than the guest's", one, two)
	}
	again, err := tp.Open(ctx, pfVM1, "devvm-box1", 58790, sshFor("devvm-box1"))
	if err != nil || again != one || tp.starts != 2 {
		t.Fatalf("again = %d, %v (%d starts); want %d, no new start", again, err, tp.starts, one)
	}
	tp.mu.Lock()
	delete(tp.listening, one)
	tp.mu.Unlock()
	fresh, err := tp.Open(ctx, pfVM1, "devvm-box1", 58790, sshFor("devvm-box1"))
	if err != nil || fresh == one {
		t.Fatalf("after the forward ended = %d, %v; want a new port", fresh, err)
	}
	rec, _ := tp.read(pfVM1, 58790)
	pid, ok := tp.proc(pfVM1, rec).PID()
	if !ok {
		t.Fatal("no forward runs")
	}
	if err := tp.Close(ctx, pfVM1, 58790); err != nil {
		t.Fatal(err)
	}
	if daemon.Alive(pid) {
		t.Fatal("the forward still runs after Close")
	}
	if _, err := os.Stat(tp.file(pfVM1, 58790, ".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("its record: %v", err)
	}
	if err := tp.Close(ctx, pfVM1, 58790); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
	// StopAll, as the VM stops: box2's goes too.
	rec2, _ := tp.read(pfVM2, 58790)
	pid2, _ := tp.proc(pfVM2, rec2).PID()
	tp.StopAll(pfVM2)
	if daemon.Alive(pid2) {
		t.Fatal("box2's forward still runs after StopAll")
	}
}

// Two at once start one forward and get the same port.
func TestPortForwardsConcurrentOpen(t *testing.T) {
	tp := newTestPF(t)
	var wg sync.WaitGroup
	got := make([]int, 2)
	for i := range got {
		wg.Go(func() {
			p, err := tp.Open(t.Context(), pfVM1, "devvm-box1", 58790, sshFor("devvm-box1"))
			if err != nil {
				t.Error(err)
			}
			got[i] = p
		})
	}
	wg.Wait()
	if got[0] != got[1] || tp.starts != 1 {
		t.Fatalf("ports %v, %d starts: want one forward", got, tp.starts)
	}
	tp.StopAll(pfVM1)
}

// A record whose pid is now another process's (not ssh's) is never
// signalled: Close leaves the process, Open starts a new forward.
func TestPortForwardsStaleRecord(t *testing.T) {
	tp := newTestPF(t)
	other := exec.CommandContext(t.Context(), "sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = other.Wait(); close(done) }()
	t.Cleanup(func() { _ = other.Process.Kill(); <-done })
	rec := portRecord{Guest: 58790, Port: 47500, Host: "devvm-box1"}
	if err := os.MkdirAll(tp.Dirs.VM(pfVM1).Run(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := tp.write(pfVM1, rec); err != nil {
		t.Fatal(err)
	}
	pidFile := tp.file(pfVM1, 58790, ".pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(other.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tp.mu.Lock()
	tp.listening[47500] = true // another program has the port now
	tp.mu.Unlock()
	port, err := tp.Open(t.Context(), pfVM1, "devvm-box1", 58790, sshFor("devvm-box1"))
	if err != nil || port == 47500 || tp.starts != 1 {
		t.Fatalf("Open = %d, %v (%d starts): want a new forward", port, err, tp.starts)
	}
	if err := tp.Close(t.Context(), pfVM1, 58790); err != nil {
		t.Fatal(err)
	}
	if !daemon.Alive(other.Process.Pid) {
		t.Fatal("an unrelated process was signalled")
	}
}

// A forward whose port never listens is stopped, and Open fails naming
// its log.
func TestPortForwardsNotUp(t *testing.T) {
	tp := newTestPF(t)
	tp.Listening = func(context.Context, int) bool { return false }
	if _, err := tp.Open(t.Context(), pfVM1, "devvm-box1", 58790, sshFor("devvm-box1")); err == nil {
		t.Fatal("Open succeeded")
	}
	if _, ok := tp.read(pfVM1, 58790); ok {
		t.Fatal("a record stays")
	}
}
