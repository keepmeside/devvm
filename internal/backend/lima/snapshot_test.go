package lima

import "testing"

func TestSnapshotIDs(t *testing.T) {
	for _, ok := range []string{"stella-abcdefghijkl", "s1", "0", "a-b-c"} {
		if err := CheckSnapshotID(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	// Stella's own ids are stella_<12 base32>: the server passes them lowercased with a dash.
	for _, bad := range []string{"", "stella_abcdefghijkl", "Snap", "-x", "a/b", "abcdefghijabcdefghijabcdefghijabcdefghijX"} {
		if err := CheckSnapshotID(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSnapshotsInListsOnlySnapshots(t *testing.T) {
	got := snapshotsIn(map[string]string{
		"devvm-snap-b":              "Stopped",
		"devvm-snap-a":              "Stopped",
		"devvm-img-base":            "Stopped",
		"devvm-box1-91e3e4ca0febc0": "Running",
		"devvm-snap-Bad_ID":         "Stopped",
	})
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("snapshotsIn = %+v", got)
	}
}
