package lima

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/devvmhq/devvm/internal/paths"
)

// SnapshotPrefix starts the name of every snapshot's instance:
// devvm-snap-<id>. It is neither an image (ImagePrefix) nor a project VM
// (vmname.IsVM), so images' housekeeping never counts it as one of theirs.
const SnapshotPrefix = "devvm-snap-"

// snapshotID is what a snapshot's ID may be: lowercase letters, digits and
// dashes, 1 to 40 of them, starting with a letter or digit.
var snapshotID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// ErrSnapshotExists is SaveSnapshot's refusal to overwrite an ID.
var ErrSnapshotExists = errors.New("snapshot ID exists")

// CheckSnapshotID refuses an ID devvm doesn't take.
func CheckSnapshotID(id string) error {
	if !snapshotID.MatchString(id) {
		return fmt.Errorf("%q isn't a snapshot ID: use 1 to 40 lowercase letters, digits and dashes", id)
	}
	return nil
}

// Snapshot is one saved snapshot.
type Snapshot struct {
	ID string `json:"id"`
}

// limactlOn is limactl with args on devvm's Lima home, in devvm's data
// folder, without the host's SSH agent.
func limactlOn(ctx context.Context, d paths.Dirs, args ...string) (*exec.Cmd, error) {
	path, err := exec.LookPath("limactl")
	if err != nil {
		return nil, limaMissing()
	}
	if err := os.MkdirAll(d.Data, 0o700); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // devvm's own arguments
	cmd.Env = limactlEnv(os.Environ(), d)
	cmd.Dir = d.Data
	return cmd, nil
}

func runLimactl(ctx context.Context, d paths.Dirs, args ...string) ([]byte, error) {
	cmd, err := limactlOn(ctx, d, args...)
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("limactl %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// limaNames lists the instances in devvm's Lima home, by name, with
// their status.
func limaNames(ctx context.Context, d paths.Dirs) (map[string]string, error) {
	out, err := runLimactl(ctx, d, "list", "--json")
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var in struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		if err := dec.Decode(&in); errors.Is(err, io.EOF) {
			return names, nil
		} else if err != nil {
			return nil, fmt.Errorf("limactl list: %w", err)
		}
		names[in.Name] = in.Status
	}
}

// SaveSnapshot clones vm, which must be stopped, into the snapshot id.
// An id that exists is refused, never overwritten.
func SaveSnapshot(ctx context.Context, d paths.Dirs, vm, id string) error {
	if err := CheckSnapshotID(id); err != nil {
		return err
	}
	names, err := limaNames(ctx, d)
	if err != nil {
		return err
	}
	if _, ok := names[SnapshotPrefix+id]; ok {
		return ErrSnapshotExists
	}
	if _, ok := names[vm]; !ok {
		return fmt.Errorf("no VM %s", vm)
	}
	_, err = runLimactl(ctx, d, "clone", "--tty=false", vm, SnapshotPrefix+id)
	return err
}

// DeleteSnapshot deletes the snapshot id; one that isn't there is
// nothing to do.
func DeleteSnapshot(ctx context.Context, d paths.Dirs, id string) error {
	if err := CheckSnapshotID(id); err != nil {
		return err
	}
	names, err := limaNames(ctx, d)
	if err != nil {
		return err
	}
	if _, ok := names[SnapshotPrefix+id]; !ok {
		return nil
	}
	_, err = runLimactl(ctx, d, "delete", "--force", SnapshotPrefix+id)
	return err
}

// Snapshots lists the saved snapshots, by ID.
func Snapshots(ctx context.Context, d paths.Dirs) ([]Snapshot, error) {
	names, err := limaNames(ctx, d)
	if err != nil {
		return nil, err
	}
	return snapshotsIn(names), nil
}

func snapshotsIn(names map[string]string) []Snapshot {
	out := []Snapshot{}
	for name := range names {
		if id, ok := strings.CutPrefix(name, SnapshotPrefix); ok && snapshotID.MatchString(id) {
			out = append(out, Snapshot{ID: id})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
