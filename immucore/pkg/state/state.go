package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/deniswernert/go-fstab"
	internalUtils "github.com/kairos-io/immucore/internal/utils"
	"github.com/spectrocloud-labs/herd"
)

type State struct {
	Rootdir       string // where to mount the root partition e.g. /sysroot inside initrd with pivot, / with nopivot
	TargetImage   string // image from the state partition to mount as loop device e.g. /cOS/active.img
	TargetDevice  string // e.g. /dev/disk/by-label/COS_ACTIVE
	RootMountMode string // How to mount the root partition e.g. ro or rw
	InRAM         bool   // running the kairos.ram workflow: rootfs is a tmpfs staged by dracut's rd.live.ram, OEM+persistent still on disk

	// /run/cos-layout.env (different!)
	OverlayDirs  []string          // e.g. /var
	BindMounts   []string          // e.g. /etc/kubernetes
	CustomMounts map[string]string // e.g. diskid : mountpoint
	OverlayBase  string            // Overlay config, defaults to tmpfs:20%
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
				if _, err := f.WriteString(fmt.Sprintf("%s\n", fst.String())); err != nil {
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
