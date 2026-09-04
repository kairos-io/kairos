package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	"github.com/kairos-io/kairos/v4/sdk/loop"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	"github.com/spectrocloud-labs/herd"
)

// MountTmpfsDagStep adds the step to mount /tmp .
func (s *State) MountTmpfsDagStep(g *herd.Graph) error {
	return g.Add(cnst.OpMountTmpfs, TimedCallback(cnst.OpMountTmpfs,
		func(_ context.Context) error {
			fstab, err := op.MountOPWithFstab("tmpfs", "/tmp", "tmpfs", []string{"rw"}, 10*time.Second)
			for _, f := range fstab {
				s.fstabs = append(s.fstabs, f)
			}
			return err
		},
	))
}

// MountRootDagStep will add the step to mount the Rootdir for the system
// 1 - mount the state partition to find the images (active/passive/recovery)
// 2 - mount the image as a loop device
// 3 - Mount the labels as /sysroot .
func (s *State) MountRootDagStep(g *herd.Graph) error {
	var err error

	// 1 - mount the state partition to find the images (active/passive/recovery)
	err = g.Add(cnst.OpMountState,
		TimedCallback(cnst.OpMountState,
			func(_ context.Context) error {
				fstab, err := op.MountOPWithFstab(
					internalUtils.GetState(),
					s.path("/run/initramfs/cos-state"),
					internalUtils.DiskFSType(internalUtils.GetState()),
					[]string{
						s.RootMountMode,
					}, 60*time.Second)
				for _, f := range fstab {
					s.fstabs = append(s.fstabs, f)
				}
				return err
			},
		),
	)
	if err != nil {
		internalUtils.KLog.Logger.Err(err).Send()
	}

	// 2 - mount the image as a loop device
	err = g.Add(cnst.OpDiscoverState,
		herd.WithDeps(cnst.OpMountState),
		TimedCallback(cnst.OpDiscoverState,
			func(_ context.Context) error {
				// Check if loop device is mounted already
				if internalUtils.IsMounted(s.TargetDevice) {
					internalUtils.KLog.Logger.Debug().Str("targetImage", s.TargetImage).Str("path", s.Rootdir).Str("TargetDevice", s.TargetDevice).Msg("Not mounting loop, already mounted")
					return nil
				}
				image := s.path("/run/initramfs/cos-state", s.TargetImage)
				_ = internalUtils.Fsck(image)
				// PartScan is left off to match "losetup -f": the state
				// image holds a bare filesystem, not a partition table.
				device, err := loop.Loop{Logger: internalUtils.KLog}.Attach(image)
				s.LogIfError(err, "attaching the state image to a loop device")
				if err == nil {
					internalUtils.KLog.Logger.Debug().Str("device", device).Str("image", image).Msg("loop attached")
				}
				// Trigger udevadm
				// On some systems the COS_ACTIVE/PASSIVE label is automatically shown as soon as we mount the device
				// But on other it seems like it won't trigger which causes the sysroot to not be mounted as we cant find
				// the block device by the target label. Make sure we run this after mounting so we refresh the devices.
				sh, _ := utils.SH("udevadm trigger")
				internalUtils.KLog.Logger.Debug().Str("output", sh).Msg("udevadm trigger")
				internalUtils.KLog.Logger.Debug().Str("targetImage", s.TargetImage).Str("path", s.Rootdir).Str("TargetDevice", s.TargetDevice).Msg("mount done")
				return err
			},
		))
	if err != nil {
		internalUtils.KLog.Logger.Err(err).Send()
	}

	// 3 - Mount the labels as Rootdir
	err = g.Add(cnst.OpMountRoot,
		herd.WithDeps(cnst.OpDiscoverState),
		TimedCallback(cnst.OpMountRoot,
			func(_ context.Context) error {
				fstab, err := op.MountOPWithFstab(
					s.TargetDevice,
					s.Rootdir,
					"ext4", // TODO: Get this just in time? Currently if using DiskFSType is run immediately which is bad because its not mounted
					[]string{
						s.RootMountMode,
						"suid",
						"dev",
						"exec",
						"async",
					}, 10*time.Second)
				for _, f := range fstab {
					s.fstabs = append(s.fstabs, f)
				}
				return err
			},
		),
	)
	if err != nil {
		internalUtils.KLog.Logger.Err(err).Send()
	}
	return err
}

// sysrootPollInterval is how long waitForSysroot waits between two checks for
// the sysroot. It is a var so the specs do not have to wait whole seconds.
var sysrootPollInterval = 2 * time.Second

// WaitForSysrootDagStep waits for the s.Rootdir and s.Rootdir/system paths to be there
// Useful for livecd/netboot as we want to run steps after s.Rootdir is ready but we don't mount it ourselves.
func (s *State) WaitForSysrootDagStep(g *herd.Graph) error {
	return g.Add(cnst.OpWaitForSysroot,
		TimedCallback(cnst.OpWaitForSysroot, s.waitForSysroot))
}

// waitForSysroot returns as soon as both paths are there, and keeps looking
// every sysrootPollInterval until the deadline runs out. The deadline is 120
// seconds, or whatever rd.immucore.sysrootwait= asks for.
func (s *State) waitForSysroot(ctx context.Context) error {
	var timeout = 120 * time.Second
	timeoutArg := internalUtils.CleanupSlice(internalUtils.ReadCMDLineArg("rd.immucore.sysrootwait="))
	if len(timeoutArg) > 0 {
		atoi, err := strconv.Atoi(timeoutArg[0])
		if err == nil && atoi > 0 {
			timeout = time.Duration(atoi) * time.Second
		}
	}

	internalUtils.KLog.Logger.Debug().Str("timeout", timeout.String()).Msg("Waiting for sysroot")

	cc := time.After(timeout)
	for {
		missing := s.missingSysrootPath()
		if missing == "" {
			return nil
		}
		internalUtils.KLog.Logger.Debug().Str("what", missing).Msg("Checking path existence")

		// Wait before looking again. This has to be a select rather than a
		// time.Sleep: a sleep holds this goroutine for the whole interval, so
		// both the cancel and the deadline below are seen that late.
		select {
		case <-time.After(sysrootPollInterval):
		case <-ctx.Done():
			e := fmt.Errorf("context canceled")
			internalUtils.KLog.Logger.Err(e).Str("what", s.Rootdir).Msg("filepath check canceled")
			return e
		case <-cc:
			e := fmt.Errorf("timeout exhausted")
			internalUtils.KLog.Logger.Err(e).Str("what", s.Rootdir).Msg("filepath check timeout")
			return e
		}
	}
}

// missingSysrootPath returns the first of the paths the sysroot needs that is
// not there yet, or the empty string once they all are.
func (s *State) missingSysrootPath() string {
	for _, path := range []string{s.Rootdir, filepath.Join(s.Rootdir, "system")} {
		if _, err := os.Stat(path); err != nil {
			return path
		}
	}

	return ""
}

// LVMActivation will try to activate lvm volumes/groups on the system.
func (s *State) LVMActivation(g *herd.Graph) error {
	return g.Add(cnst.OpLvmActivate, TimedCallback(cnst.OpLvmActivate, func(_ context.Context) error {
		return internalUtils.ActivateLVM()
	}))
}

// RunKcrypt unlocks encrypted partitions using the appropriate encryptor.
// It uses kcrypt.GetEncryptor() to automatically determine the encryption method
// (Remote KMS, TPM with PCR, or Local TPM NV) based on system configuration.
// This works for both UKI and non-UKI modes - the encryptor handles the differences.
func (s *State) RunKcrypt(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpKcryptUnlock, append(opts, TimedCallback(cnst.OpKcryptUnlock, func(_ context.Context) error {
		return unlockEncryptedPartitions()
	}))...)
}

// RunKcryptUpgrade will upgrade encrypted partitions created with 1.x to the new 2.x format, where
// we inspect the uuid of the partition directly to know which label to use for the key
// As those old installs have an old agent the only way to do it is during the first boot after the upgrade to the newest immucore.
func (s *State) RunKcryptUpgrade(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpKcryptUpgrade, append(opts, TimedCallback(cnst.OpKcryptUpgrade, func(_ context.Context) error {
		return internalUtils.UpgradeKcryptPartitions()
	}))...)
}
