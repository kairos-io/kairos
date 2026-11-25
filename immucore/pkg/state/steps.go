package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	"github.com/kairos-io/kairos/v4/sdk/loop"
	"github.com/kairos-io/kairos/v4/sdk/retry"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	"github.com/spectrocloud-labs/herd"
	"github.com/twpayne/go-vfs/v5"
)

// MountTmpfsDagStep adds the step to mount /tmp .
func (s *State) MountTmpfsDagStep(g *herd.Graph) error {
	return g.Add(cnst.OpMountTmpfs, TimedCallback(cnst.OpMountTmpfs,
		func(_ context.Context) error {
			fstab, err := op.MountOPWithFstab("tmpfs", "/tmp", "tmpfs", cnst.TmpMountOptions, 10*time.Second)
			for _, f := range fstab {
				s.appendFstab(f)
			}
			if err != nil {
				return err
			}
			// Not fatal: a /dev/shm that keeps exec is a CIS finding, not a
			// reason to stop the boot.
			if e := syscall.Mount("", "/dev/shm", "", cnst.DevShmRemountFlags, ""); e != nil {
				internalUtils.KLog.Logger.Warn().Err(e).Msg("remounting /dev/shm noexec")
			}
			return nil
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
				stateDevice := internalUtils.GetState()
				fstab, err := op.MountOPWithFstabFn(
					stateDevice,
					s.path("/run/initramfs/cos-state"),
					internalUtils.DiskFSType(stateDevice),
					s.readOnlyOrMode, 60*time.Second)
				for _, f := range fstab {
					s.appendFstab(f)
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
				// Check the sha256 kairos-agent recorded next to the image. Before fsck, which may write to it.
				if err := s.verifyImage(image); err != nil {
					return err
				}
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
				fstab, err := op.MountOPWithFstabFn(
					s.TargetDevice,
					s.Rootdir,
					"ext4", // TODO: Get this just in time? Currently if using DiskFSType is run immediately which is bad because its not mounted
					func(fstype string) []string {
						return append(s.readOnlyOrMode(fstype), "suid", "dev", "exec", "async")
					}, 10*time.Second)
				for _, f := range fstab {
					s.appendFstab(f)
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
// the sysroot. Dracut stages /sysroot within a second of immucore starting, so
// an interval measured in seconds ends the wait at the next round multiple of
// itself rather than when the sysroot is ready. It is a var so the specs can
// stretch it.
var sysrootPollInterval = 100 * time.Millisecond

// verifyImage checks the image about to be loop-mounted against the sha256
// kairos-agent stored next to it, according to the image checksum settings
// (see internalUtils.ResolveVerifySettings).
func (s *State) verifyImage(image string) error {
	return verifyImageFile(internalUtils.ResolveVerifySettings(filepath.Dir(image)), image, s.RootMountMode == "rw")
}

// verifyImageFile is the logic behind verifyImage, split out so it can be tested
// without a cmdline. rw is true when the state partition is mounted read-write
// (rd.immucore.debugrw): the image may change then, and a rw fsck rewrites its
// superblock.
//
// What each action does with each outcome:
//
//	outcome                   none   warn          fail
//	no checksum / stale       -      log warning   log warning (error screen with require)
//	malformed / unreadable    -      log error     error screen
//	mismatch                  -      log error     error screen
//	match                     -      log info      log info
//
// A problem in warn mode is logged and the boot goes on. In fail mode the boot is
// stopped with the failure screen, which names the image, the expected (stored)
// sha256 and the actual (computed) one.
func verifyImageFile(settings internalUtils.VerifySettings, image string, rw bool) error {
	log := internalUtils.KLog.Logger
	if rw {
		// The recorded checksum is about to go stale. Drop it: no checksum means
		// "not verified", a stale one would trip the next normal boot.
		if err := os.Remove(imagesum.SidecarPath(image)); err == nil {
			log.Warn().Str("image", image).Msg("State mounted rw (debugrw), the image may change: removed its checksum file, it is not verified until the next upgrade or reset")
		}
		return nil
	}
	if settings.Action != imagesum.Warn && settings.Action != imagesum.Fail {
		log.Debug().Str("image", image).Msg("Image checksum verification disabled")
		return nil
	}

	start := time.Now()
	report, err := imagesum.Check(vfs.OSFS, image)
	took := time.Since(start).Round(time.Millisecond).String()

	var problem *internalUtils.ImageChecksumProblem
	switch {
	case err != nil:
		problem = &internalUtils.ImageChecksumProblem{
			Image: image, Kind: "the checksum could not be checked", Detail: "  " + err.Error(),
			Expected: report.Expected, Actual: report.Actual,
		}
	case report.Result == imagesum.OK:
		log.Info().Str("image", image).Str("sha256", report.Actual).Str("took", took).Msg("Image checksum verified")
		return nil
	case report.Result == imagesum.Mismatch:
		problem = &internalUtils.ImageChecksumProblem{
			Image: image, Kind: "checksum mismatch: the image is not what was written",
			Detail:   "  The image has the size and modification time it was written with but\n  different content: the storage corrupted it.",
			Expected: report.Expected, Actual: report.Actual,
		}
	case report.Result == imagesum.Stale:
		if !settings.Require {
			log.Warn().Str("image", image).Str("expected", report.Expected).Msg("Image changed since its checksum was recorded (replaced by an agent that does not update checksums?), checksum ignored")
			return nil
		}
		problem = &internalUtils.ImageChecksumProblem{
			Image: image, Kind: "stale checksum: the image changed since it was recorded",
			Detail:   "  The checksum file describes an older image (size or modification time\n  differ), so it cannot vouch for this one. The image was not read.",
			Expected: report.Expected,
		}
	default: // Missing
		if !settings.Require {
			log.Warn().Str("image", image).Msg("No checksum recorded for the image, not verifying it (it predates checksums, or image-checksum.create was off when it was written)")
			return nil
		}
		problem = &internalUtils.ImageChecksumProblem{
			Image: image, Kind: "no checksum recorded for this image",
			Detail: "  " + imagesum.SidecarPath(image) + " does not exist.",
		}
	}

	log.Error().Str("image", image).Str("problem", problem.Kind).Str("expected", problem.Expected).Str("actual", problem.Actual).
		Str("action", string(settings.Action)).Msg("OS image checksum check failed")
	if settings.Action == imagesum.Warn {
		return nil
	}
	failErr := fmt.Errorf("image checksum check failed for %s: %s (expected sha256 %s, actual %s)",
		image, problem.Kind, orNotAvailable(problem.Expected), orNotAvailable(problem.Actual))
	haltWithBannerFn(internalUtils.RenderImageChecksumFailedMessage(*problem, settings), "OS image checksum check failed", failErr)
	return failErr
}

func orNotAvailable(s string) string {
	if s == "" {
		return "n/a"
	}
	return s
}

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

	err := retry.PollUntil(ctx, sysrootPollInterval, timeout, true, func() (bool, error) {
		missing := s.missingSysrootPath()
		if missing != "" {
			internalUtils.KLog.Logger.Debug().Str("what", missing).Msg("Checking path existence")
			return false, nil
		}
		return true, nil
	})
	switch {
	case errors.Is(err, retry.ErrTimeout):
		// Look once more before giving up. The sysroot may have been
		// staged during the interval we were waiting out, and a
		// rd.immucore.sysrootwait= shorter than sysrootPollInterval
		// would otherwise never get a second check at all.
		if s.missingSysrootPath() == "" {
			return nil
		}
		e := fmt.Errorf("timeout exhausted")
		internalUtils.KLog.Logger.Err(e).Str("what", s.Rootdir).Msg("filepath check timeout")
		return e
	case err != nil:
		e := fmt.Errorf("context canceled")
		internalUtils.KLog.Logger.Err(e).Str("what", s.Rootdir).Msg("filepath check canceled")
		return e
	}
	return nil
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

// upgradeKcryptPartitions is a seam for the tests. Nothing else reassigns it.
var upgradeKcryptPartitions = internalUtils.UpgradeKcryptPartitions

// RunKcryptUpgrade will upgrade encrypted partitions created with 1.x to the new 2.x format, where
// we inspect the uuid of the partition directly to know which label to use for the key
// As those old installs have an old agent the only way to do it is during the first boot after the upgrade to the newest immucore.
func (s *State) RunKcryptUpgrade(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpKcryptUpgrade, append(opts, TimedCallback(cnst.OpKcryptUpgrade, func(_ context.Context) error {
		if s.WriteProtected {
			// The upgrade rewrites the LUKS header (cryptsetup luksUUID --uuid),
			// which is a write to the very device the media refuses writes to.
			// It cannot succeed, so it is skipped rather than allowed to fail the
			// step. A partition still on the old format on write-protected media
			// stays on the old format; the unlock path handles both.
			internalUtils.KLog.Logger.Info().
				Msg("Skipping the kcrypt partition upgrade: it rewrites LUKS headers, and the media is write-protected")
			return nil
		}
		return upgradeKcryptPartitions()
	}))...)
}
