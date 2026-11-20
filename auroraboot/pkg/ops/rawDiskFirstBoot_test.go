package ops

import (
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	yipSchema "github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// legacyResetConfig is the reset config previously rendered with fmt.Sprintf, kept to guard the typed migration.
const legacyResetConfig = `name: Expand disk layout and autoreset
stages:
    after-reset:
        - commands:
            - rm /oem/01_reset.yaml
          if: '[ -f "/oem/01_reset.yaml" ]'
          name: Auto remove this file
    network:
        - commands:
            - udevadm trigger
          name: Trigger udevadm
        - commands:
            - kairos-agent --debug reset --unattended --reboot
          if: '[ -f "/run/cos/recovery_mode" ] && [ ! -f "/oem/.autoreset.skip" ]'
          name: Run auto reset
    rootfs.before:
        - name: Add state partition
          layout:
            device:
                label: COS_RECOVERY
            add_partitions:
                - fsLabel: COS_STATE
                  size: 4242
                  pLabel: state
                  filesystem: ext2
                - fsLabel: COS_PERSISTENT
                  pLabel: persistent
                  filesystem: ext4
`

func parseYip(content string) yipSchema.YipConfig {
	var cfg yipSchema.YipConfig
	Expect(yaml.Unmarshal([]byte(content), &cfg)).To(Succeed())
	return cfg
}

var _ = Describe("Raw disk first boot config", Label("raw"), func() {
	It("renders the reset config identical to the legacy template", func() {
		got := parseYip(resetFirstBootConfig(4242).ToString())
		Expect(got).To(Equal(parseYip(legacyResetConfig)))
	})

	It("creates persistent with the partition filesystem and keeps state on the GRUB-safe one", func() {
		got := parseYip(resetFirstBootConfig(4242).ToString())
		fs := map[string]string{}
		for _, stage := range got.Stages[stageRootfsBefore] {
			for _, part := range stage.Layout.Parts {
				fs[part.FSLabel] = part.FileSystem
			}
		}
		Expect(fs).To(Equal(map[string]string{
			sdkConstants.PersistentLabel: sdkConstants.LinuxFs,
			sdkConstants.StateLabel:      sdkConstants.LinuxImgFs,
		}))
	})

	It("creates the cluster plugin config dir before agent.boot runs in initramfs", func() {
		got := parseYip(bootActiveFirstBootConfig().ToString())
		Expect(got.Stages).To(HaveKey(stageInitramfsBefore))
		stages := got.Stages[stageInitramfsBefore]
		Expect(stages).To(HaveLen(1))
		Expect(stages[0].If).To(ContainSubstring(recoveryModeSentry))
		Expect(stages[0].Directories).To(Equal([]yipSchema.Directory{{
			Path:        clusterProviderConfigDir,
			Permissions: clusterProviderConfigDirPerm,
		}}))
	})

	It("only adds the persistent partition for boot-active images", func() {
		got := parseYip(bootActiveFirstBootConfig().ToString())
		Expect(got.Stages).To(HaveLen(2))
		Expect(got.Stages).To(HaveKey(stageRootfsBefore))
		stages := got.Stages[stageRootfsBefore]
		Expect(stages).To(HaveLen(1))
		Expect(stages[0].Commands).To(BeEmpty())
		Expect(stages[0].Layout.Device.Label).To(Equal(sdkConstants.StateLabel))
		Expect(stages[0].Layout.Parts).To(Equal([]yipSchema.Partition{{
			FSLabel:    sdkConstants.PersistentLabel,
			PLabel:     sdkConstants.PersistentPartName,
			FileSystem: sdkConstants.LinuxFs,
		}}))
	})

	It("picks the config and EFI chainload by boot mode", func() {
		r := NewEFIRawImage(RawImageParams{BootActive: true})
		file, cfg, err := r.firstBootConfig("")
		Expect(err).ToNot(HaveOccurred())
		Expect(file).To(Equal(layoutCloudInitFile))
		Expect(cfg.Stages).ToNot(HaveKey(stageNetwork))
		Expect(r.efiGrubCfg()).To(ContainSubstring(sdkConstants.StateLabel))

		r = NewEFIRawImage(RawImageParams{StateSize: 1000})
		file, cfg, err = r.firstBootConfig("")
		Expect(err).ToNot(HaveOccurred())
		Expect(file).To(Equal(resetCloudInitFile))
		Expect(cfg.Stages).To(HaveKey(stageNetwork))
		Expect(r.efiGrubCfg()).To(ContainSubstring(sdkConstants.RecoveryLabel))
	})
})
