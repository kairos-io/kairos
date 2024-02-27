// nolint
package mos_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

var _ = Describe("kairos custom partitioning install", Label("custom-partitioning"), func() {
	var vm VM

	BeforeEach(func() {
		stateDir, err := os.MkdirTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		fmt.Printf("State dir: %s\n", stateDir)

		opts := defaultVMOptsNoDrives(stateDir)
		opts = append(opts, types.WithDriveSize("40000"))
		opts = append(opts, types.WithDriveSize("30000"))

		m, err := machine.New(opts...)
		Expect(err).ToNot(HaveOccurred())
		vm = NewVM(m, stateDir)
		_, err = vm.Start(context.Background())
		Expect(err).ToNot(HaveOccurred())

		DeferCleanup(func() {
			vm.Destroy(nil)
		})

		By("waiting for VM to be up for the first time")
		vm.EventuallyConnects(1200)

		By("creating a configuration for custom partitioning")
		configFile, err := os.CreateTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		defer os.Remove(configFile.Name())

		err = os.WriteFile(configFile.Name(), []byte(customPartitionConfig()), 0744)
		Expect(err).ToNot(HaveOccurred())

		By("copying the configuration to the vm")
		err = vm.Scp(configFile.Name(), "/tmp/config.yaml", "0744")
		Expect(err).ToNot(HaveOccurred())

		By("manually installing")
		installationOutput, installError = vm.Sudo("kairos-agent --debug manual-install --device auto /tmp/config.yaml")
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}
	})

	It("installs on the pre-configured disks", func() {
		Expect(installError).ToNot(HaveOccurred(), installationOutput)
		By("Rebooting")
		vm.Reboot()
		vm.EventuallyConnects(1200)

		By("Checking the partition")
		out, err := vm.Sudo("blkid")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(MatchRegexp("/dev/vdb2.*LABEL=\"COS_OEM\""), out)
		Expect(out).To(MatchRegexp("/dev/vdb3.*LABEL=\"COS_RECOVERY\""), out)
		Expect(out).To(MatchRegexp("/dev/vdb4.*LABEL=\"COS_STATE\""), out)
		Expect(out).To(MatchRegexp("/dev/vdb5.*LABEL=\"COS_PERSISTENT\""), out)
	})
})

func customPartitionConfig() string {
	return `#cloud-config

strict: true
debug: true

install:
  no-format: true
  auto: true
  poweroff: false
  reboot: false
  grub_options:
    extra_cmdline: "rd.immucore.debug"

users:
  - name: "kairos"
    passwd: "kairos"

stages:
  kairos-install.pre.before:
  - if:  '[ -e "/dev/vdb" ]'
    name: "Create partitions"
    commands:
      - |
        parted --script --machine -- "/dev/vdb" mklabel gpt
        parted --script "/dev/vdb" mkpart primary Data 0 1MB
    layout:
      add_partitions:
        - fsLabel: COS_OEM
          size: 64
          pLabel: oem
        - fsLabel: COS_RECOVERY
          size: 8500
          pLabel: recovery
        - fsLabel: COS_STATE
          size: 18000
          pLabel: state
        - fsLabel: COS_PERSISTENT
          pLabel: persistent
          size: 0
          filesystem: "ext4"
  boot:
    - systemd_firstboot:
      keymap: us
`
}
