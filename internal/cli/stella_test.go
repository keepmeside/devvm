package cli

import (
	"testing"
)

func TestForwardsGiveTwoVMsDistinctHostPorts(t *testing.T) {
	next := 47000
	started := map[int]bool{}
	f := forwards{
		dir:       t.TempDir(),
		alive:     func(pid int) bool { return pid > 0 },
		listening: func(port int) bool { return started[port] },
		free:      func() (int, error) { next++; return next, nil },
		start:     func(port int) (int, error) { started[port] = true; return port, nil },
	}
	one, err := f.open("devvm-box1-91e3e4ca0febc0", 58790)
	if err != nil {
		t.Fatal(err)
	}
	two, err := f.open("devvm-box2-0123456789abcd", 58790)
	if err != nil {
		t.Fatal(err)
	}
	if one == two || one == 58790 || two == 58790 {
		t.Fatalf("ports %d and %d: want two distinct ports other than the guest's", one, two)
	}
	// Asked again while it works: the same port, no new forward.
	again, err := f.open("devvm-box1-91e3e4ca0febc0", 58790)
	if err != nil || again != one {
		t.Fatalf("again = %d, %v; want %d", again, err, one)
	}
	// Its forward gone (the VM stopped): a new one.
	delete(started, one)
	fresh, err := f.open("devvm-box1-91e3e4ca0febc0", 58790)
	if err != nil || fresh == one {
		t.Fatalf("after the forward ended = %d, %v; want a new port", fresh, err)
	}
	if err := f.close("devvm-box1-91e3e4ca0febc0", 58790); err != nil {
		t.Fatal(err)
	}
	if err := f.close("devvm-box1-91e3e4ca0febc0", 58790); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
}

func TestExecLineQuotesAndRunsAsRoot(t *testing.T) {
	got := execLine([]string{"sh", "-c", "mkdir -p /opt/stella && tar -xf - -C /opt/stella"}, true)
	want := "'sudo' '-n' '-H' '--' 'sh' '-c' 'mkdir -p /opt/stella && tar -xf - -C /opt/stella'"
	if got != want {
		t.Fatalf("execLine = %q, want %q", got, want)
	}
	if got := execLine([]string{"cat", "/root/it's"}, false); got != `'cat' '/root/it'\''s'` {
		t.Fatalf("execLine = %q", got)
	}
}
