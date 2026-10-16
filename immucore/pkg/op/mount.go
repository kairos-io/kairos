package op

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/internal/mount"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/schema"
	"github.com/kairos-io/kairos/v4/sdk/retry"
)

// mountRetryInterval is how long MountOPWithFstab waits between two mount
// attempts. Nothing waits before the first one. A var, not a const, so the
// tests can shorten or lengthen it instead of measuring the real one.
var mountRetryInterval = 250 * time.Millisecond

// MountOPWithFstab creates and executes a mount operation.
// returns the fstab entries created and an error if any.
func MountOPWithFstab(what, where, t string, options []string, timeout time.Duration) (schema.FsTabs, error) {
	var fstab schema.FsTabs
	l := internalUtils.KLog.With().Str("what", what).Str("where", where).Str("type", t).Strs("options", options).Logger().Level(internalUtils.KLog.GetLevel())

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := retry.Do(func() error {
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
			return err
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

		// only retry if it's an error and not an already mounted error
		if err != nil && !errors.Is(err, constants.ErrAlreadyMounted) {
			l.Warn().Err(err).Send()
			return err
		}
		l.Info().Msg("mount done")
		return nil
	},
		// Zero wait for the first attempt: the device is usually already
		// there, and every caller pays this wait otherwise. Bounded only by
		// ctx's timeout, not by an attempt count.
		retry.Config{
			Delay: retry.Fixed(mountRetryInterval),
			Ctx:   ctx,
		},
	)

	if err != nil {
		e := fmt.Errorf("timeout exhausted")
		l.Err(e).Msg("Mount timeout")
		return fstab, e
	}
	return fstab, nil
}
