package ops

import (
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	yipSchema "github.com/mudler/yip/pkg/schema"
)

const (
	// resetCloudInitFile is the OEM file that expands the layout and autoresets on first boot, then removes itself.
	resetCloudInitFile = "01_reset.yaml"
	// layoutCloudInitFile is the OEM file that adds COS_PERSISTENT on first boot of a boot-active image.
	layoutCloudInitFile = "01_layout.yaml"

	stageRootfsBefore    = "rootfs.before"
	stageInitramfsBefore = "initramfs.before"
	stageNetwork         = "network"
	stageAfterReset      = "after-reset"

	oemMountDir         = "/oem/"
	autoresetSkipFile   = "/oem/.autoreset.skip"
	recoveryModeSentry  = "/run/cos/recovery_mode"
	udevadmTriggerCmd   = "udevadm trigger"
	autoresetCmd        = "kairos-agent --debug reset --unattended --reboot"
	resetConfigName     = "Expand disk layout and autoreset"
	layoutConfigName    = "Expand disk layout"
	grubNextEntryKey    = "next_entry"
	grubRecoveryEntry   = "recovery"
	grubOEMEnvFile      = "grubenv"
	customCloudInitFile = "90_custom.yaml"

	// clusterProviderConfigDir is where kairos cluster plugins write their config on agent.boot, in the initramfs stage.
	clusterProviderConfigDir = "/usr/local/cloud-config"
	// clusterProviderConfigDirPerm matches the mode 00_rootfs.yaml uses when it creates the dir in fs.after.
	clusterProviderConfigDirPerm = 0o600
)

// resetFirstBootConfig adds COS_STATE and COS_PERSISTENT on first boot and runs an unattended reset from recovery.
func resetFirstBootConfig(stateSizeMB int64) *yipSchema.YipConfig {
	return &yipSchema.YipConfig{
		Name: resetConfigName,
		Stages: map[string][]yipSchema.Stage{
			stageAfterReset: {{
				Name:     "Auto remove this file",
				Commands: []string{"rm " + oemMountDir + resetCloudInitFile},
				If:       `[ -f "` + oemMountDir + resetCloudInitFile + `" ]`,
			}},
			stageNetwork: {
				{
					Name:     "Trigger udevadm",
					Commands: []string{udevadmTriggerCmd},
				},
				{
					Name:     "Run auto reset",
					Commands: []string{autoresetCmd},
					If:       `[ -f "` + recoveryModeSentry + `" ] && [ ! -f "` + autoresetSkipFile + `" ]`,
				},
			},
			stageRootfsBefore: {{
				Name: "Add state partition",
				Layout: yipSchema.Layout{
					Device: &yipSchema.Device{Label: sdkConstants.RecoveryLabel},
					Parts: []yipSchema.Partition{
						// COS_STATE stays ext2 for now. GRUB reads it on every boot, and yip's
						// mkfs cannot pass -O ^metadata_csum_seed yet, so e2fsprogs 1.47+ would
						// make it unreadable for GRUB < 2.12 without the backport
						// (kairos-io/kairos#5476).
						{
							FSLabel:    sdkConstants.StateLabel,
							Size:       uint64(stateSizeMB),
							PLabel:     sdkConstants.StatePartName,
							FileSystem: sdkConstants.LinuxImgFs,
						},
						// COS_PERSISTENT holds live data (k3s/etcd, containerd, kubelet) and
						// GRUB never reads it, so it gets ext4 like an install does.
						{
							FSLabel:    sdkConstants.PersistentLabel,
							PLabel:     sdkConstants.PersistentPartName,
							FileSystem: sdkConstants.LinuxFs,
						},
					},
				},
			}},
		},
	}
}

// bootActiveFirstBootConfig adds COS_PERSISTENT over the rest of the disk; yip skips it once the partition exists.
// It also creates the cluster plugin config dir on the fresh persistent before agent.boot fires in the initramfs stage.
func bootActiveFirstBootConfig() *yipSchema.YipConfig {
	return &yipSchema.YipConfig{
		Name: layoutConfigName,
		Stages: map[string][]yipSchema.Stage{
			stageInitramfsBefore: {{
				Name: "Ensure cluster provider config dir",
				If:   `[ ! -f "` + recoveryModeSentry + `" ]`,
				Directories: []yipSchema.Directory{{
					Path:        clusterProviderConfigDir,
					Permissions: clusterProviderConfigDirPerm,
				}},
			}},
			stageRootfsBefore: {{
				Name: "Add persistent partition",
				Layout: yipSchema.Layout{
					// COS_STATE is on every boot-active disk, COS_RECOVERY may be skipped
					Device: &yipSchema.Device{Label: sdkConstants.StateLabel},
					Parts: []yipSchema.Partition{{
						FSLabel:    sdkConstants.PersistentLabel,
						PLabel:     sdkConstants.PersistentPartName,
						FileSystem: sdkConstants.LinuxFs,
					}},
				},
			}},
		},
	}
}
