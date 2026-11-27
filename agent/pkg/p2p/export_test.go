package p2p

import (
	"syscall"
	"time"
)

// SetStatfsForTest makes the disk look like avail bytes are free.
func SetStatfsForTest(avail int64) func() {
	old := statfsFn
	statfsFn = func(_ string, st *syscall.Statfs_t) error {
		st.Bsize = 4096
		st.Bavail = uint64(avail / 4096)
		return nil
	}
	return func() { statfsFn = old }
}

// SetGraceForTest shortens the time a download from one manifest gets before
// its speed is judged.
func SetGraceForTest(d time.Duration) func() {
	old := groupGrace
	groupGrace = d
	return func() { groupGrace = old }
}

// SetETAGraceForTest shortens the time before the projected duration is judged.
func SetETAGraceForTest(d time.Duration) func() {
	old := etaGrace
	etaGrace = d
	return func() { etaGrace = old }
}

// SetETAFloorForTest changes the least expected duration of a download.
func SetETAFloorForTest(d time.Duration) func() {
	old := etaFloor
	etaFloor = d
	return func() { etaFloor = old }
}

// SetManifestTimeoutForTest changes how long a peer gets to give its manifest.
func SetManifestTimeoutForTest(d time.Duration) func() {
	old := manifestTimeout
	manifestTimeout = d
	return func() { manifestTimeout = old }
}
