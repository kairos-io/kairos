package state

import (
	"context"
	"fmt"
	"path/filepath"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	"github.com/kairos-io/kairos/v4/immucore/pkg/schema"
	"github.com/kairos-io/kairos/v4/sdk/blockdev"
	"github.com/kairos-io/kairos/v4/sdk/loop"
	"github.com/spectrocloud-labs/herd"
)

// Seams for the tests, which have no tmpfs to mount, no block device to snapshot
// and no loop device to attach. Nothing else reassigns them.
var (
	mountCowStore = func(spec string) (op.MountOperation, error) {
		operation, err := op.BaseOverlay(schema.Overlay{Base: cnst.PersistentCowDir, BackingBase: spec})
		if err != nil {
			return operation, err
		}
		return operation, operation.Run()
	}
	cowTmpfsSize   = internalUtils.TmpfsSizeBytes
	createSparse   = internalUtils.CreateSparseFile
	attachLoop     = func(path string) (string, error) { return loop.Loop{Logger: internalUtils.KLog}.Attach(path) }
	createSnapshot = internalUtils.CreateSnapshot
	snapshotStatus = blockdev.QuerySnapshotStatus
)

// MountPersistentSnapshotDagStep makes the persistent partition writable on
// write-protected media, by putting a device-mapper snapshot over it.
//
// The snapshot reads unchanged chunks from the partition and keeps changed
// chunks in a copy-on-write store on a tmpfs, so what gets mounted at the
// persistent mountpoint is an ordinary read-write ext4 with everything that
// was pre-seeded on the disk visible through it, and everything written since
// boot living in RAM until power-off. That is the same mechanism dracut's
// dmsquash-live uses for the root of every live Kairos boot.
//
// A filesystem rather than a file-level overlay because of what runs on top:
// a container runtime's snapshotter is itself overlayfs, and the kernel refuses
// an overlayfs whose upper layer is on another overlayfs. On a real ext4 it
// works as it does on any disk, and the pre-seeded image layers are usable.
//
// The step runs before OpCustomMounts and swaps the persistent device in
// s.CustomMounts for the snapshot, so the ordinary read-write mount does the
// rest and nothing downstream, the binds included, knows the difference. It is
// herd.FatalOp: without it the persistent mount fails against the frozen
// device and every bind is skipped, and that has to reach the console.
//
// Registered only when s.WriteProtected, so a writable install keeps the graph it
// has always had.
func (s *State) MountPersistentSnapshotDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpPersistentSnapshot,
		append(opts,
			herd.WithDeps(cnst.OpLoadConfig),
			herd.FatalOp,
			TimedCallback(cnst.OpPersistentSnapshot, func(_ context.Context) error {
				what, where, ok := s.persistentVolume()
				if !ok {
					// No VOLUMES entry names the persistent mountpoint, so there
					// is nothing to snapshot and the layout is whatever it would
					// be on a writable disk with the same configuration.
					internalUtils.KLog.Logger.Warn().
						Msg("No persistent volume is configured, so there is nothing to snapshot on read-only media")
					return nil
				}

				// The same resolution the custom mount would do: a by-label path
				// races udev on installs whose LUKS outer and inner ext4 share a
				// label (kairos-io/kairos#4403).
				origin := what
				if label := labelFromByLabelPath(what); label != "" {
					resolved, err := resolveMountSource(label)
					if err != nil {
						return fmt.Errorf("resolving the persistent volume %s: %w", what, err)
					}
					origin = resolved
				}

				store, err := mountCowStore(s.CowBase)
				if err != nil {
					return fmt.Errorf("mounting the copy-on-write store %s (%s): %w", cnst.PersistentCowDir, s.CowBase, err)
				}
				s.fstabs = append(s.fstabs, &store.FstabEntry)

				tmpfsBytes, err := cowTmpfsSize(cnst.PersistentCowDir)
				if err != nil {
					return fmt.Errorf("sizing the copy-on-write store: %w", err)
				}
				size := internalUtils.CowStoreSize(tmpfsBytes)
				if err := createSparse(cnst.PersistentCowFile, size); err != nil {
					return fmt.Errorf("creating the copy-on-write file: %w", err)
				}
				cow, err := attachLoop(cnst.PersistentCowFile)
				if err != nil {
					return fmt.Errorf("attaching the copy-on-write file: %w", err)
				}

				if err := createSnapshot(cnst.PersistentSnapshotName, origin, cow); err != nil {
					return err
				}

				// From here on the persistent volume is the snapshot. The custom
				// mount step finds it under the same mountpoint and mounts it
				// read-write like any other disk.
				mapper := filepath.Join("/dev/mapper", cnst.PersistentSnapshotName)
				delete(s.CustomMounts, what)
				s.CustomMounts[mapper] = where

				if st, err := snapshotStatus(cnst.PersistentSnapshotName); err == nil {
					internalUtils.KLog.Logger.Info().
						Uint64("store_total_sectors", st.TotalSectors).Uint64("store_used_sectors", st.UsedSectors).
						Str("state", st.State).
						Msg("Persistent partition is a copy-on-write snapshot; its store only grows until the next boot, and a full store refuses writes")
				}
				return nil
			}),
		)...)
}

// WriteProtectedSnapshotDeps returns the extra dependency MountCustomMountsDagStep
// needs on write-protected media: the persistent device it is about to mount
// is the snapshot, which has to exist first. Empty on a writable install, so
// that graph is untouched and no dependency names an op that was never
// registered, which herd does not check and which panics in Analyze().
func (s *State) WriteProtectedSnapshotDeps() []herd.OpOption {
	if !s.WriteProtected {
		return nil
	}
	return []herd.OpOption{herd.WithDeps(cnst.OpPersistentSnapshot)}
}
