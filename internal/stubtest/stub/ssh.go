package main

import (
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// masterLife is how long a master connection waits to be stopped.
const masterLife = 300 * time.Second

// listenOnce listens on the unix socket path with a backlog of one and
// never accepts: the socket takes the first connection or two, which
// wait, and refuses the rest, as no master answers on it. devvm's check
// for a killed session's leftover socket (vm.LeftoverSockets) then finds
// it stale once something has tried it.
func listenOnce(path string) (int, error) {
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return -1, err
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	if err := syscall.Listen(fd, 1); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

// ssh stands in for OpenSSH's ssh, reaching the guest (guest.go). It logs
// every call to $STUB_LOG. A master connection (-M, a VM's port
// forwarder's) reads the script it's fed, answers it with
// $STUB_DIR/guest-ports and ".", then waits to be stopped. A link
// session's master (-M -N -S CONTROL, devvm _links's) listens on its
// control socket until it's stopped, and its -O forward and -O cancel
// work, but for a forward whose ADDRESS:PORT is a line of
// $STUB_DIR/link-taken, which the VM refuses. Either master outlives the
// process that started it, as ssh's does, for 5 minutes (TestScript
// stops its sandbox's). Host git pushing to and fetching from a --clone
// VM runs its git-receive-pack or git-upload-pack on the guest's files.
// devvm forward's ssh (-N -L 127.0.0.1:PORT:…, no master) listens on
// PORT until it's stopped, as ssh's does, unless $STUB_DIR/forward-fails
// is there; devvm exec's (a command after --) prints the remote command
// line, then its input unless -n, and exits with $STUB_DIR/ssh-exit's
// number (0 without it). With $STUB_DIR/ssh-fails, or for anything else,
// it fails as ssh does (255).
func ssh(c *call) int {
	c.log()
	if c.on("ssh-fails") {
		return 255
	}
	g := &guest{call: c}
	switch {
	case c.has("-M") && c.has("-N"):
		fd, err := listenOnce(c.after("-S"))
		if err != nil {
			c.say("ssh stub: " + err.Error())
			return 255
		}
		defer func() { _ = syscall.Close(fd) }()
		c.sleep(masterLife)
		return 0
	case c.has("-O"):
		addr, rest, _ := strings.Cut(c.after("-R"), ":")
		port, _, _ := strings.Cut(rest, ":")
		taken, _ := c.text("link-taken")
		for line := range strings.Lines(taken) {
			if strings.TrimSuffix(line, "\n") == addr+":"+port {
				c.say("Error: remote port forwarding failed for listen port " + port)
				return 255
			}
		}
		return 0
	case c.has("-M"):
		return g.ports()
	case c.has("-N") && c.has("-L"):
		return c.localForward()
	case c.has("--"):
		return c.remote()
	}
	if cmd := c.last(); strings.HasPrefix(cmd, "git-receive-pack ") || strings.HasPrefix(cmd, "git-upload-pack ") {
		return g.pack(cmd)
	}
	return 255
}

// localForward is ssh -N -L: it listens on -L's local port, as ssh does,
// for masterLife, or fails as ssh does when it can't or with
// $STUB_DIR/forward-fails.
func (c *call) localForward() int {
	local := c.after("-L")
	host, rest, _ := strings.Cut(local, ":")
	port, _, _ := strings.Cut(rest, ":")
	if c.on("forward-fails") {
		c.say("bind [" + host + "]:" + port + ": Address already in use")
		return 255
	}
	var lc net.ListenConfig
	l, err := lc.Listen(c.ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		c.say("ssh stub: " + err.Error())
		return 255
	}
	defer func() { _ = l.Close() }()
	c.sleep(masterLife)
	return 0
}

// remote is ssh HOST -- LINE: it prints LINE, then its input unless -n,
// and exits with $STUB_DIR/ssh-exit's number.
func (c *call) remote() int {
	c.print(c.last() + "\n")
	if !c.has("-n") {
		_, _ = io.Copy(c.stdout, c.stdin)
	}
	code, err := strconv.Atoi(c.textOr("ssh-exit", "0"))
	if err != nil {
		return 255
	}
	return code
}
