package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/deniswernert/go-fstab"
	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/spectrocloud-labs/herd"
)

type State struct {
	Rootdir       string // where to mount the root partition e.g. /sysroot inside initrd with pivot, / with nopivot
	TargetImage   string // image from the state partition to mount as loop device e.g. /cOS/active.img
	TargetDevice  string // e.g. /dev/disk/by-label/COS_ACTIVE
	RootMountMode string // How to mount the root partition e.g. ro or rw
	InRAM         bool   // running the kairos.ram workflow: rootfs is a tmpfs staged by dracut's rd.live.ram, OEM+persistent still on disk
	// WriteProtected means the media is write-protected, so the persistent
	// partition is mounted through a copy-on-write snapshot held in RAM rather
	// than directly. Resolved once in pkg/cmd/root.go
	// from internalUtils.WriteProtected(); nothing re-reads the cmdline per step.
	WriteProtected bool

	// /run/cos-layout.env (different!)
	OverlayDirs  []string          // e.g. /var
	BindMounts   []string          // e.g. /etc/kubernetes
	CustomMounts map[string]string // e.g. diskid : mountpoint
	OverlayBase  string            // Overlay config, defaults to tmpfs:20%
	CowBase      string            // tmpfs spec for the snapshot's copy-on-write store on read-only media
	StateDir     string            // e.g. "/usr/local/.state"
	fstabs       []*fstab.Mount
}

// SortedBindMounts returns the nodes with less depth first and in alphabetical order.
func (s *State) SortedBindMounts() []string {
	bindMountsCopy := s.BindMounts
	sort.Slice(bindMountsCopy, func(i, j int) bool {
		iAry := strings.Split(bindMountsCopy[i], "/")
		jAry := strings.Split(bindMountsCopy[j], "/")
		iSize := len(iAry)
		jSize := len(jAry)
		if iSize == jSize {
			return strings.Compare(iAry[len(iAry)-1], jAry[len(jAry)-1]) == -1
		}
		return iSize < jSize
	})
	return bindMountsCopy
}

func (s *State) path(p ...string) string {
	return filepath.Join(append([]string{s.Rootdir}, p...)...)
}

// isPersistentVolume reports whether a custom mount backs the persistent state
// target. With the defaults that is the volume mounted at /usr/local, which
// backs /usr/local/.state.
//
// Two signals, either of which is enough. The mountpoint: the exact parent of
// the state target, or the target itself. The mountpoint survives every way of
// declaring a VOLUMES entry, which the device does not: internalUtils.ParseMount
// rewrites UUID=<uuid>:/usr/local into /dev/disk/by-uuid/<uuid>, which carries no
// label, and VOLUMES=/dev/sda5:/usr/local never had one. Matching on the device
// string would miss both and apply the read-only layout to only half the mounts.
//
// The label as well: a by-label device carrying the persistent label is the
// persistent volume wherever it is mounted, which keeps LABEL=COS_PERSISTENT:/data
// read-write as it always was. An ancestor further up than the parent is not
// enough: a VOLUMES entry mounted at /usr is not the persistent volume.
func (s *State) isPersistentVolume(what, where string) bool {
	target := s.StateDir
	if target == "" {
		// Nothing has read cos-layout.env yet. Fall back to the same default
		// LoadEnvLayoutDagStep would have applied, so a caller that runs early
		// cannot get a quietly wrong answer.
		target = cnst.PersistentStateTarget
	}
	target = filepath.Clean(target)
	w := filepath.Clean(where)
	if w == target || w == filepath.Dir(target) {
		return true
	}

	switch labelFromByLabelPath(what) {
	case sdkConstants.PersistentLabel, sdkConstants.PersistentLUKSLabel:
		return true
	}
	return false
}

// persistentVolume returns the device and mountpoint of the custom mount that
// backs the persistent state target, so that every step deciding something about
// it agrees by construction rather than by a second copy of the predicate.
func (s *State) persistentVolume() (what, where string, ok bool) {
	for what, where := range s.CustomMounts {
		if s.isPersistentVolume(what, where) {
			return what, where, true
		}
	}
	return "", "", false
}

// customMountPlan decides where a VOLUMES entry is mounted and with which
// options. Returned as a function of the filesystem type, because the type is
// only known for certain inside the mount retry loop.
//
// Ordinary volumes are read-only and the persistent volume is read-write, on
// write-protected media too: by the time this runs OpPersistentSnapshot has
// replaced the persistent device in s.CustomMounts with its copy-on-write
// snapshot, which accepts writes, so the mount is the same as on any disk.
func (s *State) customMountPlan(what, where string) (target string, options func(fstype string) []string) {
	if s.isPersistentVolume(what, where) {
		// TODO: Are custom mounts always rw?ro?depends? Clarify.
		// Persistent needs to be RW
		return s.path(where), func(string) []string { return []string{"rw"} }
	}
	return s.path(where), func(string) []string { return []string{"ro"} }
}

// oemMountOptions are the options /oem is mounted with, given its filesystem.
//
// OEM is the operator's input: the userdata, the grub environment, the kcrypt
// configuration. On write-protected media it is mounted read-only rather than
// given an overlay of its own, so that a write which cannot be persisted fails
// instead of appearing to succeed and vanishing on the next boot. /oem/grubenv
// is the clearest case: GRUB reads it before Linux exists, so a write landing in
// RAM could never affect the next boot anyway.
func (s *State) oemMountOptions(fstype string) []string {
	mode := []string{"rw"}
	if s.WriteProtected {
		mode = internalUtils.ReadOnlyMountOptions(fstype)
	}
	return append(mode, "suid", "dev", "exec", "async")
}

// readOnlyOrMode returns the mount options for a device that is normally mounted
// with RootMountMode.
//
// On write-protected media RootMountMode is already "ro", but "ro" on its own
// still lets the kernel replay a dirty ext4 journal, and that replay is a write.
// So the read-only branch asks for the filesystem's no-recovery option too. This
// covers COS_STATE and the root image, which are mounted before anything knows
// about the persistent partition.
func (s *State) readOnlyOrMode(fstype string) []string {
	if s.WriteProtected {
		return internalUtils.ReadOnlyMountOptions(fstype)
	}
	return []string{s.RootMountMode}
}

func (s *State) WriteFstab() func(context.Context) error {
	return func(ctx context.Context) error {
		// Create the file first, override if something is there, we don't care, we are on initramfs
		fstabFile := s.path("/etc/fstab")
		f, err := os.Create(fstabFile)
		if err != nil {
			return err
		}
		_ = f.Close()
		for _, fst := range s.fstabs {
			internalUtils.KLog.Logger.Debug().Str("what", fst.String()).Msg("Adding line to fstab")
			select {
			case <-ctx.Done():
			default:
				f, err := os.OpenFile(fstabFile,
					os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintf(f, "%s\n", fst.String()); err != nil {
					_ = f.Close()
					return err
				}
				_ = f.Close()
			}
		}
		return nil
	}
}

// WriteDAG writes the dag.
func (s *State) WriteDAG(g *herd.Graph) (out string) {
	for i, layer := range g.Analyze() {
		out += fmt.Sprintf("%d.\n", i+1)
		for _, op := range layer {
			if op.Error != nil {
				out += fmt.Sprintf(" <%s> (error: %s) (background: %t) (weak: %t) (run: %t)\n", op.Name, op.Error.Error(), op.Background, op.WeakDeps, op.Executed)
			} else {
				out += fmt.Sprintf(" <%s> (background: %t) (weak: %t) (run: %t)\n", op.Name, op.Background, op.WeakDeps, op.Executed)
			}
		}
	}
	return
}

// FailureReason scans the graph after a run and returns a human-readable string
// listing the operations that errored (op.Error is only set once an op has run),
// for use in the emergency-shell failure summary. Returns a generic message when
// no specific op reported an error.
func (s *State) FailureReason(g *herd.Graph) string {
	var failed []string
	for _, layer := range g.Analyze() {
		for _, op := range layer {
			if op.Error != nil {
				failed = append(failed, fmt.Sprintf("%s: %s", op.Name, op.Error.Error()))
			}
		}
	}
	if len(failed) == 0 {
		return "boot failed (no specific operation reported an error)"
	}
	return "failed operations: " + strings.Join(failed, "; ")
}

// LogIfError will log if there is an error with the given context as message
// Context can be empty.
func (s *State) LogIfError(e error, msgContext string) {
	if e != nil {
		internalUtils.KLog.Logger.Err(e).Msg(msgContext)
	}
}

// LogIfErrorAndReturn will log if there is an error with the given context as message
// Context can be empty
// Will also return the error.
func (s *State) LogIfErrorAndReturn(e error, msgContext string) error {
	if e != nil {
		internalUtils.KLog.Logger.Err(e).Msg(msgContext)
	}
	return e
}

// LogIfErrorAndPanic will log if there is an error with the given context as message
// Context can be empty
// Will also panic.
func (s *State) LogIfErrorAndPanic(e error, msgContext string) {
	if e != nil {
		internalUtils.KLog.Logger.Err(e).Msg(msgContext)
		internalUtils.KLog.Logger.Fatal().Msg(e.Error())
	}
}

// AddToFstab will try to add an entry to the fstab list
// Will check if the entry exists before adding it to avoid duplicates.
func (s *State) AddToFstab(tmpFstab *fstab.Mount) {
	found := false
	for _, f := range s.fstabs {
		if f.Spec == tmpFstab.Spec {
			internalUtils.KLog.Logger.Debug().Interface("existing", f).Interface("duplicated", tmpFstab).Msg("Duplicated fstab entry found, not adding")
			found = true
		}
	}
	if !found {
		s.fstabs = append(s.fstabs, tmpFstab)
	}
}
