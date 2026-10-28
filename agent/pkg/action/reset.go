/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package action

import (
	"time"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	"github.com/kairos-io/kairos/v4/agent/pkg/state"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

func (r *ResetAction) resetHook(hook string, chroot bool) error {
	if chroot {
		extraMounts := map[string]string{}
		persistent := r.spec.Partitions.Persistent
		if persistent != nil && persistent.MountPoint != "" {
			extraMounts[persistent.MountPoint] = cnst.UsrLocalPath
		}
		oem := r.spec.Partitions.OEM
		if oem != nil && oem.MountPoint != "" {
			extraMounts[oem.MountPoint] = cnst.OEMPath
		}
		return ChrootHook(r.cfg, hook, r.spec.Active.MountPoint, extraMounts)
	}
	return Hook(r.cfg, hook)
}

type ResetAction struct {
	cfg  *sdkConfig.Config
	spec *v1.ResetSpec
}

func NewResetAction(cfg *sdkConfig.Config, spec *v1.ResetSpec) *ResetAction {
	return &ResetAction{cfg: cfg, spec: spec}
}

// Run will reset the cos system to by following several steps
func (r ResetAction) Run() (err error) {
	e := elemental.NewElemental(r.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()

	// Reformat state partition
	// We should expose this under a flag, to reformat state before starting
	// In case state fs is broken somehow
	// We used to format it blindly but if something happens between this format and the new image copy down below
	// You end up with a broken system with no active or passive and reset may not even work at that point
	// TODO: Either create a flag to do this or ignore it and never format the partition
	/*
		if r.spec.FormatState {
			state := r.spec.Partitions.State
			if state != nil {
				err = e.UnmountPartition(state)
				if err != nil {
					return err
				}
				err = e.FormatPartition(state)
				if err != nil {
					return err
				}
				// Mount it back
				err = e.MountPartition(state)
				if err != nil {
					return err
				}
			}
		}
	*/

	// Check every partition this reset formats before formatting any of
	// them, so a reset that cannot end in the state the configuration
	// demands stops while nothing has been destroyed.
	err = PreflightResetFormats(r.cfg, r.spec.Partitions.Persistent, r.spec.Partitions.OEM, r.spec.FormatPersistent, r.spec.FormatOEM)
	if err != nil {
		return err
	}

	// Reformat persistent partition
	if r.spec.FormatPersistent {
		persistent := r.spec.Partitions.Persistent
		if persistent != nil {
			// The audit trail and the state file are the things on the
			// persistent partition a reset is not meant to destroy, so carry
			// them over the format.
			restore := r.preservePersistent(persistent)

			err = e.UnmountPartition(persistent)
			if err != nil {
				return err
			}
			err = e.FormatPartition(persistent)
			if err != nil {
				return err
			}

			// The format above leaves persistent plaintext. When the
			// configuration lists it as encrypted, encrypt it again now,
			// while it is empty by construction, so a reset ends in the
			// same state an install ends in (kairos-io/kairos#4556). Fail
			// closed: a node whose configuration demands encryption must
			// not come back from a reset plaintext. This must run before
			// the audit trail is restored: encrypting LUKS-formats the
			// partition, so anything written earlier is destroyed. After
			// it the spec points at the unlocked mapper, which is where
			// the restore lands.
			err = ResetEncryptFn(r.cfg, persistent)
			if err != nil {
				return err
			}

			restore()
		}
	}

	// Reformat OEM
	if r.spec.FormatOEM {
		oem := r.spec.Partitions.OEM
		if oem != nil {
			// Try to umount
			err = e.UnmountPartition(oem)
			if err != nil {
				return err
			}
			err = e.FormatPartition(oem)
			if err != nil {
				return err
			}

			// Same as persistent above: the format leaves OEM plaintext
			// and empty, so when the configuration lists it as encrypted
			// (explicitly, or through the UKI default), encrypt it again
			// now. On a node encrypted at install the format hit the
			// unlocked mapper, the container survived, and this is a
			// no-op.
			err = ResetEncryptFn(r.cfg, oem)
			if err != nil {
				return err
			}

			// Mount it back, as oem is mounted during recovery, keep everything as is
			err = e.MountPartition(oem)
			if err != nil {
				return err
			}
		}
	}

	// Before reset hook happens once partitions are already and before deploying the OS image
	err = r.resetHook(cnst.BeforeResetHook, false)
	if err != nil {
		return err
	}

	// Mount COS_STATE so we can write the new images
	err = e.MountPartition(r.spec.Partitions.State)
	if err != nil {
		return err
	}
	cleanup.Push(func() error { return e.UnmountPartition(r.spec.Partitions.State) })

	// Deploy active image
	_, err = e.DeployImage(&r.spec.Active, true)
	if err != nil {
		return err
	}
	cleanup.Push(func() error { return e.UnmountImage(&r.spec.Active) })

	// Label the image as boot_t: it was just created and does not carry a
	// label yet, so boot-time components can access it.
	e.LabelStateImage(r.spec.Active.File)

	// Create extra dirs in rootfs as afterwards this will be impossible due to RO system
	createExtraDirsInRootfs(r.cfg, r.spec.ExtraDirsRootfs, r.spec.Active.MountPoint)

	// Mount EFI partition before installing grub as under EFI this copies stuff in there
	if r.spec.Efi {
		err = e.MountPartition(r.spec.Partitions.EFI)
		if err != nil {
			return err
		}
		cleanup.Push(func() error { return e.UnmountPartition(r.spec.Partitions.EFI) })
	}
	//TODO: does bios needs to be mounted here?

	// install grub
	grub := utils.NewGrub(r.cfg)
	err = grub.Install(
		r.spec.Target,
		r.spec.Active.MountPoint,
		r.spec.Partitions.State.MountPoint,
		r.spec.GrubConf,
		r.spec.Tty,
		r.spec.Efi,
		r.spec.Partitions.State.FilesystemLabel,
	)
	if err != nil {
		return err
	}

	// Relabel SELinux
	// TODO probably relabelling persistent volumes should be an opt in feature, it could
	// have undesired effects in case of failures
	binds := map[string]string{}
	if mnt, _ := utils.IsMounted(r.cfg, r.spec.Partitions.Persistent); mnt {
		binds[r.spec.Partitions.Persistent.MountPoint] = cnst.UsrLocalPath
	}
	if mnt, _ := utils.IsMounted(r.cfg, r.spec.Partitions.OEM); mnt {
		binds[r.spec.Partitions.OEM.MountPoint] = cnst.OEMPath
	}
	err = utils.ChrootedCallback(
		r.cfg, r.spec.Active.MountPoint, binds,
		func() error { return e.SelinuxRelabel("/", true) },
	)
	if err != nil {
		return err
	}

	err = r.resetHook(cnst.AfterResetChrootHook, true)
	if err != nil {
		return err
	}

	// installation rebrand (only grub for now)
	err = e.SetDefaultGrubEntry(
		r.spec.Partitions.State.MountPoint,
		r.spec.Active.MountPoint,
		r.spec.GrubDefEntry,
	)
	if err != nil {
		return err
	}

	// Unmount active image
	err = e.UnmountImage(&r.spec.Active)
	if err != nil {
		return err
	}

	// Install Passive
	_, err = e.DeployImage(&r.spec.Passive, false)
	if err != nil {
		return err
	}
	// passive.img is created after SelinuxRelabel runs, so label it here
	e.LabelStateImage(r.spec.Passive.File)

	err = r.resetHook(cnst.AfterResetHook, false)
	if err != nil {
		return err
	}

	r.recordState()

	// Do not reboot/poweroff on cleanup errors
	err = cleanup.Cleanup(err)
	if err != nil {
		return err
	}

	return err
}

// preservePersistent stashes the audit trail and the state file off the
// persistent partition and returns the function that puts them back after the
// format. Failing to preserve either does not fail the reset: that is the
// behaviour we have today, while a reset that stops halfway leaves an
// unbootable machine.
func (r *ResetAction) preservePersistent(persistent *sdkPartitions.Partition) func() {
	auditStash, err := StashAuditLog(r.cfg, persistent)
	if err != nil {
		r.cfg.Logger.Warnf("could not preserve %s across the reset: %s", cnst.AuditLogPath, err)
	}
	stateStash, err := StashStateFile(r.cfg, persistent)
	if err != nil {
		r.cfg.Logger.Warnf("could not preserve %s across the reset: %s", state.FileName, err)
	}

	return func() {
		if err := RestoreAuditLog(r.cfg, persistent, auditStash); err != nil {
			r.cfg.Logger.Warnf("could not restore %s after the reset: %s", cnst.AuditLogPath, err)
		}
		if err := RestoreStateFile(r.cfg, persistent, stateStash); err != nil {
			r.cfg.Logger.Warnf("could not restore %s after the reset: %s", state.FileName, err)
		}
	}
}

func (r *ResetAction) recordState() {
	source := r.spec.Active.Source.String()
	persistent := r.spec.Partitions.Persistent
	if persistent == nil {
		if err := state.RecordReset(r.cfg.Fs, cnst.PersistentDir, source, time.Now); err != nil {
			r.cfg.Logger.Warnf("failed to update kairos state file: %s", err)
		}
		return
	}

	// Recovery boots without persistent mounted and usually without a mount
	// point for it, so mount it for the write. Otherwise the state file lands
	// on the recovery rootfs, which is discarded on reboot.
	err := withPersistentMounted(r.cfg, persistent, func(root string) error {
		return state.RecordReset(r.cfg.Fs, root, source, time.Now)
	})
	if err != nil {
		r.cfg.Logger.Warnf("failed to update kairos state file: %s", err)
	}
}
