package vm

import (
	"context"

	"github.com/devvmhq/devvm/internal/msg"
	"github.com/devvmhq/devvm/internal/paths"
	"github.com/devvmhq/devvm/internal/project"
	"github.com/devvmhq/devvm/internal/state"
)

// FolderVM is the name of the folder's VM, as every command on it finds
// it (e.Folder's rule), which its backend has: devvm exec and devvm
// forward without --vm, and the VM devvm create made or started (the
// Stella fork's). It neither asks nor moves a VM.
func (e *Env) FolderVM(ctx context.Context) (string, error) {
	t, err := e.finder.target(ctx, need{has: hasMachine, move: moveNone})
	if err != nil {
		return "", err
	}
	return t.f.Name, nil
}

// NamedVM is the state of the project VM name names, by its name or its
// ssh alias, as devvm rm VM finds it (--vm, the Stella fork's): one that
// isn't is rm_unknown_vm.
func NamedVM(d paths.Dirs, name string) (*state.VM, error) {
	inv, err := ReadStates(d)
	if err != nil {
		return nil, err
	}
	vm := project.Named(name, inv.Recorded())
	if vm == "" {
		return nil, msg.RmUnknownVM(name)
	}
	return state.ReadVM(d, vm)
}

// StopVM stops the VM that name names, by its name or its ssh alias, from
// any folder (devvm stop --vm), as Lifecycle.Stop does; .git is checked
// only when its folder is there.
func (e *Env) StopVM(ctx context.Context, name string) error {
	t, err := e.finder.target(ctx, need{name: name, has: hasMachine})
	if err != nil {
		return err
	}
	return stopTarget(ctx, Lifecycle{e}, t, FolderOf(t.st) == FolderThere)
}
