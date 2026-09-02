package op

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/containerd/containerd/mount"
	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/schema"
)

// mountRetryInterval is how long MountOPWithFstab waits between two mount
// attempts. Nothing waits before the first one.
const mountRetryInterval = 1 * time.Second

// MountOPWithFstab creates and executes a mount operation.
// returns the fstab entries created and an error if any.
func MountOPWithFstab(what, where, t string, options []string, timeout time.Duration) (schema.FsTabs, error) {
	var fstab schema.FsTabs
	l := internalUtils.KLog.With().Str("what", what).Str("where", where).Str("type", t).Strs("options", options).Logger().Level(internalUtils.KLog.GetLevel())
	c := context.Background()
	cc := time.After(timeout)
	// Zero for the first attempt: the device is usually already there, and
	// every caller pays this wait otherwise. Retries wait mountRetryInterval,
	// and the wait is a select case so the timeout can cut it short.
	wait := time.Duration(0)
	for {
		select {
		case <-time.After(wait):
			wait = mountRetryInterval
			// check fs type just-in-time before running the OP
			if t != "tmpfs" {
				fsType := internalUtils.DiskFSType(what)
				// If not empty and it does not match
				if fsType != "" && t != fsType {
					t = fsType
				}
			}

			err := internalUtils.CreateIfNotExists(where)
			if err != nil {
				l.Err(err).Msg("Creating dir")
				continue
			}
			mountPoint := mount.Mount{
				Type:    t,
				Source:  what,
				Options: options,
			}
			tmpFstab := internalUtils.MountToFstab(mountPoint)
			tmpFstab.File = internalUtils.CleanSysrootForFstab(where)
			op := MountOperation{
				MountOption: mountPoint,
				FstabEntry:  *tmpFstab,
				Target:      where,
				PrepareCallback: func() error {
					_ = internalUtils.Fsck(what)
					return nil
				},
			}

			err = op.Run()

			// If no error on mounting or error is already mounted, as that affects the sysroot
			// for some reason it reports that its already mounted (systemd is mounting it behind our back!).
			if err == nil || err != nil && errors.Is(err, constants.ErrAlreadyMounted) {
				fstab = append(fstab, tmpFstab)
			} else {
				l.Debug().Err(err).Msg("Mount not added to fstab")
			}

			// only continue the loop if it's an error and not an already mounted error
			if err != nil && !errors.Is(err, constants.ErrAlreadyMounted) {
				l.Warn().Err(err).Send()
				continue
			}
			l.Info().Msg("mount done")
			return fstab, nil
		case <-c.Done():
			e := fmt.Errorf("context canceled")
			l.Err(e).Msg("mount canceled")
			return fstab, e
		case <-cc:
			e := fmt.Errorf("timeout exhausted")
			l.Err(e).Msg("Mount timeout")
			return fstab, e
		}
	}
}
