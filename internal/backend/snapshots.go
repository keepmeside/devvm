package backend

import "context"

// Snapshotter is a backend that saves a stopped VM as a snapshot, which
// a new VM can be made from (VMSpec.Snapshot): devvm snapshot, the Stella
// fork's. It's an optional role, found with As; a snapshot is never one
// of the backend's VMs (Machines.List) nor an image.
type Snapshotter interface {
	// Snapshots lists the saved snapshots' IDs, sorted.
	Snapshots(ctx context.Context) ([]string, error)
	// SaveSnapshot saves vm, which must be stopped, as the snapshot id,
	// refusing an id that exists.
	SaveSnapshot(ctx context.Context, vm, id string) error
	// DeleteSnapshot deletes the snapshot id; one that isn't there is
	// nothing to do.
	DeleteSnapshot(ctx context.Context, id string) error
}
