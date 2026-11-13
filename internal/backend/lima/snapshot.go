package lima

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/devvmhq/devvm/internal/backend"
	"github.com/devvmhq/devvm/internal/msg"
	"github.com/devvmhq/devvm/internal/vmname"
)

// SnapshotPrefix starts the name of every snapshot's instance:
// devvm-snap-<id>. List leaves them out, as it does images, so no listing
// of VMs, nor housekeeping's strays, takes one for a VM; CheckSnapshotID
// refuses an ID that would make the name a project VM's (vmname.IsVM).
const SnapshotPrefix = "devvm-snap-"

// snapshotID is what a snapshot's ID may be: lowercase letters, digits and
// dashes, 1 to 40 of them, starting with a letter or digit.
var snapshotID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// CheckSnapshotID refuses an ID devvm doesn't take: one snapshotID
// refuses, or one whose instance's name would be a project VM's
// (devvm-snap-…-<14 hex>).
func CheckSnapshotID(id string) error {
	if !snapshotID.MatchString(id) || vmname.IsVM(SnapshotPrefix+id) {
		return msg.SnapshotIDInvalid(id)
	}
	return nil
}

// isSnapshot reports whether an instance's name is a snapshot's: never a
// project VM's, such as a folder snap-x's devvm-snap-x-<hash>.
func isSnapshot(name string) bool {
	return strings.HasPrefix(name, SnapshotPrefix) && !vmname.IsVM(name)
}

// snapshots is Lima's backend.Snapshotter: each snapshot is a stopped
// instance, devvm-snap-<id>, cloned from a stopped VM, which CreateVM
// clones again for a VM made from it.
type snapshots struct{ *Lima }

var _ backend.Snapshotter = (*Lima)(nil)

// Snapshots lists the saved snapshots' IDs, sorted.
func (l *snapshots) Snapshots(ctx context.Context) ([]string, error) {
	ins, err := l.list(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, in := range ins {
		if id, ok := strings.CutPrefix(in.Name, SnapshotPrefix); ok && CheckSnapshotID(id) == nil {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// SaveSnapshot clones vm, which must be stopped, into the snapshot id. An
// id that exists is refused, never overwritten.
func (l *snapshots) SaveSnapshot(ctx context.Context, vm, id string) error {
	if err := CheckSnapshotID(id); err != nil {
		return err
	}
	switch _, err := l.inspect(ctx, SnapshotPrefix+id); {
	case err == nil:
		return msg.SnapshotExists(id)
	case !errors.Is(err, backend.ErrNotFound):
		return err
	}
	in, err := l.inspect(ctx, vm)
	if err != nil {
		return err
	}
	if backend.Status(in.Status) != backend.StatusStopped {
		return msg.VMNotStopped(vm)
	}
	if err := l.Runner.Run(ctx, msg.SavingSnapshot(id, vm), l.limactl(ctx, "clone", "--tty=false", vm, SnapshotPrefix+id)); err != nil {
		return backend.Logged(msg.SnapshotSaveFailed(id, vm))
	}
	return nil
}

// DeleteSnapshot deletes the snapshot id (Lima.delete); one that isn't
// there is nothing to do.
func (l *snapshots) DeleteSnapshot(ctx context.Context, id string) error {
	if err := CheckSnapshotID(id); err != nil {
		return err
	}
	if _, err := l.inspect(ctx, SnapshotPrefix+id); err != nil {
		if errors.Is(err, backend.ErrNotFound) {
			return nil
		}
		return err
	}
	return l.delete(ctx, SnapshotPrefix+id)
}
