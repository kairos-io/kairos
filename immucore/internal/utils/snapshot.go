package utils

import (
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/blockdev"
	"golang.org/x/sys/unix"
)

// snapshotChunkSectors is the copy-on-write granularity: 8 sectors is 4 KiB,
// the same as an ext4 block, so a 4 KiB write copies 4 KiB into RAM and not a
// larger chunk around it.
const snapshotChunkSectors = 8

// cowStoreShare is how much of the copy-on-write tmpfs the store may claim.
// The store's own accounting has to reach "full" before the tmpfs underneath
// it does: a store that fills reports Overflow and keeps serving reads, while
// a store whose backing file hits ENOSPC is invalidated and serves nothing.
// The margin is what keeps the first limit in front of the second.
const cowStoreShare = 95

// SnapshotTable is the device-mapper table for a copy-on-write view of origin
// whose changed chunks go to cow. PO rather than N: a persistent-format store
// gains the overflow behaviour, and on a tmpfs it is just as gone at power-off.
func SnapshotTable(originSectors uint64, origin, cow string) string {
	return fmt.Sprintf("0 %d snapshot %s %s PO %d", originSectors, origin, cow, snapshotChunkSectors)
}

// CowStoreSize is the size to give the sparse file that backs the store, for
// a tmpfs of the given size: the share above, rounded down to whole chunks.
func CowStoreSize(tmpfsBytes uint64) uint64 {
	size := tmpfsBytes / 100 * cowStoreShare
	chunk := uint64(snapshotChunkSectors * 512)
	return size - size%chunk
}

// TmpfsSizeBytes is the size of the filesystem mounted at path.
func TmpfsSizeBytes(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Blocks) * uint64(st.Bsize), nil //nolint:unconvert // Bsize is int64 on linux and uint32 elsewhere
}

// CreateSparseFile makes an empty file of the given logical size. Nothing is
// allocated until something writes to it, which is what makes it a suitable
// backing for a store that is sized larger than it is expected to fill.
func CreateSparseFile(path string, size uint64) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Truncate(int64(size))
}

// CreateSnapshot builds the device-mapper snapshot name over origin with cow as
// its store, so that /dev/mapper/<name> is a writable view of a device that is
// not. Every write lands in cow; reads of untouched chunks come from origin.
//
// dm loads a target's module on demand, but modprobe is asked first so that a
// missing dm-snapshot fails with its own message rather than as an opaque
// table load error.
func CreateSnapshot(name, origin, cow string) error {
	if out, err := CommandWithPath("modprobe dm-snapshot"); err != nil {
		KLog.Logger.Warn().Err(err).Str("out", strings.TrimSpace(out)).
			Msg("modprobe dm-snapshot failed; the snapshot target may still be built in or load on demand")
	}
	sectors, err := blockdev.SizeSectors(origin)
	if err != nil {
		return fmt.Errorf("sizing %s: %w", origin, err)
	}
	table := SnapshotTable(sectors, origin, cow)
	out, err := CommandWithPath(fmt.Sprintf("dmsetup create %s --table %q", name, table))
	if err != nil {
		return fmt.Errorf("dmsetup create %s: %s: %w", name, strings.TrimSpace(out), err)
	}
	KLog.Logger.Info().Str("name", name).Str("origin", origin).Str("cow", cow).Uint64("origin_sectors", sectors).
		Msg("Created the copy-on-write snapshot of the persistent partition")
	return nil
}
