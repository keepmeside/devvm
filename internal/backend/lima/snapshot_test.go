package lima

import (
	"testing"

	"github.com/devvmhq/devvm/internal/backend"
)

func TestSnapshotIDs(t *testing.T) {
	for _, ok := range []string{"stella-abcdefghijkl", "s1", "0", "a-b-c", "a-0123456789abc", "a-0123456789abcde"} {
		if err := CheckSnapshotID(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	// Stella's own ids are stella_<12 base32>: the server passes them
	// lowercased with a dash. One ending in a dash and 14 hex digits would
	// make devvm-snap-<id> a project VM's name.
	for _, bad := range []string{"", "stella_abcdefghijkl", "Snap", "-x", "a/b", "abcdefghijabcdefghijabcdefghijabcdefghijX", "a-0123456789abcd"} {
		if err := CheckSnapshotID(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSnapshotsAreNeitherVMsNorImages(t *testing.T) {
	for name, want := range map[string]bool{
		"devvm-snap-a": true, "devvm-snap-stella-x": true,
		"devvm-img-base": false, "devvm-box1-91e3e4ca0febc0": false, "devvm-snapper-91e3e4ca0febc0": false,
		"devvm-snap-x-91e3e4ca0febc0": false, // a folder snap-x's VM
	} {
		if isSnapshot(name) != want {
			t.Errorf("isSnapshot(%q) = %v", name, !want)
		}
	}
	var _ backend.Snapshotter = (*Lima)(nil)
}
