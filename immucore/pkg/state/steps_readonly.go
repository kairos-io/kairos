package state

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	"github.com/spectrocloud-labs/herd"
)

// lowerIsMounted is a seam for the tests. Nothing else reassigns it.
var lowerIsMounted = internalUtils.IsMounted

// MountPersistentROOverlayDagStep puts a writable view of the persistent tree
// back where everything expects it, on write-protected media.
//
// MountCustomMountsDagStep has by then mounted the persistent filesystem
// read-only at cnst.PersistentROMount instead of at its configured mountpoint.
// This step stacks an overlay on that mountpoint whose lower layer is that
// mount and whose upper layer is on the tmpfs at /run/overlay. Reads fall
// through to whatever provisioning wrote to the partition, writes land in RAM
// and are gone on reboot. The copy-on-write itself is the kernel's overlayfs;
// this step only assembles the layers and issues the one mount.
//
// Everything downstream then needs no change at all. op.MountBind builds its
// state directories as <root>/<StateDir>/<path>.bind, which now resolves through
// the merged overlay, so every PERSISTENT_STATE_PATHS entry is created, synced
// and bound exactly as it is on a writable disk.
//
// Registered only when s.HardwareRO, so a writable install keeps the graph it
// has always had. Registered as herd.FatalOp: see mountOverlayOn for why.
func (s *State) MountPersistentROOverlayDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpPersistentROOverlay,
		append(opts,
			herd.WithDeps(cnst.OpLoadConfig, cnst.OpCustomMounts, cnst.OpMountBaseOverlay),
			herd.FatalOp,
			TimedCallback(cnst.OpPersistentROOverlay, func(_ context.Context) error {
				_, where, ok := s.persistentVolume()
				if !ok {
					// No VOLUMES entry named the persistent mountpoint, so there
					// is no persistent filesystem to read through to. Overlay the
					// state target on the image's own copy of it, which gives a
					// fully ephemeral machine rather than a boot that dies in
					// OpMountBind when the first bind tries to create its state
					// directory on the read-only rootfs.
					where = s.StateDir
					if where == "" {
						where = cnst.PersistentStateTarget
					}
					// The mountpoint that backs a state target is its parent:
					// /usr/local/.state is backed by /usr/local.
					where = filepath.Dir(where)
					internalUtils.KLog.Logger.Warn().Str("where", where).
						Msg("No persistent volume is configured: overlaying the state target on tmpfs alone, so nothing under it survives a reboot")
					return s.mountOverlayOn(where, s.path(where))
				}

				// herd skips this op outright when OpCustomMounts errored, so a
				// failed persistent mount never reaches here. What this catches
				// is OpCustomMounts succeeding without the persistent filesystem
				// being mounted where we expect it: a second VOLUMES entry that
				// also matched the predicate and got ErrAlreadyMounted, or the
				// mount having gone away. Cheap, and the alternative is stacking
				// the overlay on an empty directory and booting a node that looks
				// healthy with none of its pre-seeded content.
				if !lowerIsMounted(cnst.PersistentROMount) {
					return fmt.Errorf("the persistent filesystem is not mounted at %s, so there is nothing for the writable overlay on %s to read through to", cnst.PersistentROMount, where)
				}

				return s.mountOverlayOn(where, cnst.PersistentROMount)
			}),
		)...)
}

// ReadOnlyOverlayBindDeps returns the extra dependencies MountCustomBindsDagStep
// needs on write-protected media: the binds must wait for the overlay that
// makes their target writable. Empty on a writable install, so that graph is
// untouched.
func (s *State) ReadOnlyOverlayBindDeps() []herd.OpOption {
	if !s.HardwareRO {
		return nil
	}
	return []herd.OpOption{herd.WithDeps(cnst.OpPersistentROOverlay)}
}

// ReadOnlyOverlayWeakDep names OpPersistentROOverlay as a weak dependency, but
// only when that step was actually registered.
//
// Naming an op that was never registered does not fail, which is the trap. herd
// hands the name to depgraph.DependOn, which creates the node rather than
// complaining, and the node then has no entry in the ops map. The first
// Analyze() walks the layers doing g.ops[name].Lock() on every one of them, so
// the nil *OpState dereferences and immucore panics before it has mounted
// anything. Hence a no-op option on a writable install rather than the name.
func (s *State) ReadOnlyOverlayWeakDep() herd.OpOption {
	if s.HardwareRO {
		return herd.WithWeakDeps(cnst.OpPersistentROOverlay)
	}
	return func(_ string, _ *herd.OpState, _ *herd.Graph) error { return nil }
}

// buildOverlayOn constructs, without running, the overlay operation for where
// on the given lower layer. Split from mountOverlayOn so the fstab entry it
// produces can be asserted as data.
func (s *State) buildOverlayOn(where, lower string) op.MountOperation {
	operation := op.MountOverlayWithLower(where, s.Rootdir, cnst.OverlayBaseDir, lower)

	// systemd-fstab-generator derives RequiresMountsFor from the device field,
	// and an overlay's device field is the literal string "overlay": it has no
	// way to know that lowerdir= names a mount it has to order behind. In
	// practice both are mounted before systemd takes over and it simply marks
	// them active, so this is insurance rather than load-bearing. It goes on the
	// fstab entry only, never on the mount options, or mount(2) would be handed
	// an option the filesystem does not know. op.BaseOverlay already reaches
	// into MntOps the same way.
	if lower == cnst.PersistentROMount {
		operation.FstabEntry.MntOps["x-systemd.requires"] = lower
	}
	return operation
}

// mountOverlayOn stacks the overlay for where on the given lower layer.
func (s *State) mountOverlayOn(where, lower string) error {
	operation := s.buildOverlayOn(where, lower)

	err := operation.Run()
	switch {
	case err == nil:
		s.fstabs = append(s.fstabs, &operation.FstabEntry)
		return nil
	case errors.Is(err, cnst.ErrAlreadyMounted):
		return nil
	}

	// This error has to be seen. Without the overlay every persistent bind
	// either fails on the read-only filesystem or lands on image content, and
	// /etc/systemd, /etc/ssh, /home and the rest come up pristine on a node
	// that otherwise looks healthy.
	//
	// Returning an error from an ordinary op does not achieve that: herd only
	// skips the dependents, Run() still returns nil, immucore exits 0 and
	// switch_root happens. The op is therefore registered with herd.FatalOp,
	// which makes Run() return this error, so pkg/cmd/root.go paints the boot
	// failure summary on the console and writes /run/immucore/boot_failure.log.
	// The boot still continues into the degraded system afterwards, the same
	// as every other failed mount in this tree; halting a remote unit into a
	// loop was judged worse than a visible degraded boot.
	return fmt.Errorf("stacking the writable overlay for %s on %s: %w", where, lower, err)
}
