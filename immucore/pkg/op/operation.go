package op

import (
	"github.com/containerd/containerd/mount"
	"github.com/deniswernert/go-fstab"
	"github.com/kairos-io/immucore/internal/constants"
	internalUtils "github.com/kairos-io/immucore/internal/utils"
	"github.com/moby/sys/mountinfo"
)

type MountOperation struct {
	FstabEntry      fstab.Mount
	MountOption     mount.Mount
	Target          string
	PrepareCallback func() error
}

func (m MountOperation) Run() error {
	// call sync to make sure the data is written to disk
	defer internalUtils.Sync()

	// Add context to sublogger
	l := internalUtils.KLog.With().Str("what", m.MountOption.Source).Str("where", m.Target).Str("type", m.MountOption.Type).Strs("options", m.MountOption.Options).Logger()

	if m.PrepareCallback != nil {
		if err := m.PrepareCallback(); err != nil {
			l.Warn().Err(err).Msg("executing mount callback")
			return err
		}
	}
	//TODO: not only check if mounted but also if the type,options and source are the same?
	mounted, err := mountinfo.Mounted(m.Target)
	if err != nil {
		l.Warn().Err(err).Msg("checking mount status")
		return err
	}

	// In UKI mode we need to remount things from ephemeral to persistent if persistent exists so we need to skip the check for mount
	// only in UKI (/home basically)
	if mounted && !internalUtils.IsUKI() {
		l.Debug().Msg("Already mounted")
		return constants.ErrAlreadyMounted
	}
	l.Debug().Msg("mount ready")
	return mount.All([]mount.Mount{m.MountOption}, m.Target)
}
