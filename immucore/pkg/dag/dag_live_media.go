package dag

import (
	cnst "github.com/kairos-io/immucore/internal/constants"
	"github.com/kairos-io/immucore/pkg/state"
	"github.com/spectrocloud-labs/herd"
)

// RegisterLiveMedia registers the dag for booting from live media/netboot
// This mainly sets the sentinel, mounts oem if it can (failure is not fatal), runs rootfs and initramfs stages
// And thats it.
// There is a wait for sysroot to be there, just in case. Not waiting for it, can result in a race condition in which
// sysroot is not ready when we try to mount oem and run stages
// We let the actual init system deal with the mounts itself as we like hwo it setup cdrom mounts and such automatically.
func RegisterLiveMedia(s *state.State, g *herd.Graph) error {
	// Maybe LogIfErrorAndPanic ? If no sentinel, a lot of config files are not going to run
	err := s.LogIfErrorAndReturn(s.WriteSentinelDagStep(g), "write sentinel")

	// Waits for sysroot to be there, just in case
	s.LogIfError(s.WaitForSysrootDagStep(g), "Waiting for sysroot")
	// Run rootfs

	// Try to mount oem ONLY if we are on recovery squash
	// The check to see if its enabled its on the DAG step itself
	s.LogIfError(s.MountOemDagStep(g, herd.WithDeps(cnst.OpWaitForSysroot)), "oem mount")

	s.LogIfError(s.RootfsStageDagStep(g, herd.WithDeps(cnst.OpSentinel, cnst.OpWaitForSysroot), herd.WithWeakDeps(cnst.OpMountOEM)), "rootfs stage")
	// Run initramfs inside the /sysroot chroot!
	s.LogIfError(s.InitramfsStageDagStep(g, herd.WithDeps(cnst.OpSentinel, cnst.OpWaitForSysroot, cnst.OpRootfsHook), herd.WithWeakDeps(cnst.OpMountOEM)), "initramfs stage")
	return err
}
