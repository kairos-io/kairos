package e2e_test

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// As this tests all use loop devices, they should be run serially so they dont hit each other while acquiring the loop device number
var _ = Describe("Disk image generation", Label("raw-disks", "e2e"), Serial, Ordered, func() {
	var tempDir string
	var err error
	var aurora *Auroraboot

	BeforeEach(func() {
		out, err := exec.Command("losetup").CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))
		fmt.Println("losetup output: ", string(out))
		tempDir, err = os.MkdirTemp("", "auroraboot-test-")
		Expect(err).ToNot(HaveOccurred())

		err = WriteConfig("test", tempDir)
		Expect(err).ToNot(HaveOccurred())

		aurora = NewAuroraboot()
		// Map the config.yaml file to the container and the temp dir to the state dir
		aurora.ManualDirs = map[string]string{
			fmt.Sprintf("%s/config.yaml", tempDir): "/config.yaml",
			tempDir:                                "/tmp/auroraboot",
		}
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	Context("source is a container image", func() {
		var config string

		BeforeEach(func() {
			// Overwrite the config.yaml file with a cloud-config
			config = `#cloud-config

hostname: kairos-{{ trunc 4 .MachineID }}

# Automated install block
install:
  # Device for automated installs
  device: "auto"
  # Reboot after installation
  reboot: false
  # Power off after installation
  poweroff: true
  # Set to true to enable automated installations
  auto: true

## Login
users:
- name: "kairos"
  groups:
    - "admin"
  lock_passwd: true
  ssh_authorized_keys:
  - github:mudler

stages:
  boot:
  - name: "Repart image"
    layout:
      device:
        label: COS_PERSISTENT
      expand_partition:
        size: 0 # all space
    commands:
      # grow filesystem if not used 100%
      - |
         [[ "$(echo "$(df -h | grep COS_PERSISTENT)" | awk '{print $5}' | tr -d '%')" -ne 100 ]] && resize2fs /dev/disk/by-label/COS_PERSISTENT`
			err = WriteConfig(config, tempDir)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			os.RemoveAll(tempDir)
		})
		Describe("EFI", Label("efi"), func() {
			It("generate a raw disk file", func() {
				image := "quay.io/kairos/opensuse:tumbleweed-core-amd64-generic-v3.2.1"
				_, err := PullImage(image)
				Expect(err).ToNot(HaveOccurred())

				out, err := aurora.Run("--debug",
					"--set", "disable_http_server=true",
					"--set", "disable_netboot=true",
					"--set", "container_image=oci://"+image,
					"--set", "state_dir=/tmp/auroraboot",
					"--set", "disk.efi=true",
					"--cloud-config", "/config.yaml",
				)

				Expect(out).To(ContainSubstring("Generating raw disk"), out)
				Expect(out).To(ContainSubstring(constants.OpGenEFIRawDisk), out)
				Expect(out).To(ContainSubstring(constants.OpDumpSource), out)
				Expect(err).ToNot(HaveOccurred(), out)
				_, err = os.Stat(filepath.Join(tempDir, "kairos-opensuse-tumbleweed-core-amd64-generic-v3.2.1.raw"))
				Expect(err).ToNot(HaveOccurred(), out)
			})
			It("generates a boot-active raw disk with a state partition", func() {
				image := "quay.io/kairos/opensuse:tumbleweed-core-amd64-generic-v3.2.1"
				_, err := PullImage(image)
				Expect(err).ToNot(HaveOccurred())

				out, err := aurora.Run("--debug",
					"--set", "disable_http_server=true",
					"--set", "disable_netboot=true",
					"--set", "container_image=oci://"+image,
					"--set", "state_dir=/tmp/auroraboot",
					"--set", "disk.efi=true",
					"--set", "disk.boot_active=true",
					"--cloud-config", "/config.yaml",
				)
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("Created STATE image"), out)
				rawDisk := filepath.Join(tempDir, "kairos-opensuse-tumbleweed-core-amd64-generic-v3.2.1.raw")
				_, err = os.Stat(rawDisk)
				Expect(err).ToNot(HaveOccurred(), out)

				disk, err := diskfs.Open(rawDisk, diskfs.WithOpenMode(diskfs.ReadOnly))
				Expect(err).ToNot(HaveOccurred())
				defer disk.Close()
				table, err := disk.GetPartitionTable()
				Expect(err).ToNot(HaveOccurred())
				gptTable, ok := table.(*gpt.Table)
				Expect(ok).To(BeTrue())
				Expect(gptTable.Partitions).To(HaveLen(4))
				Expect(gptTable.Partitions[3].Name).To(Equal("state"))
			})
			It("generates a single-image boot-active raw disk without recovery", func() {
				image := "quay.io/kairos/opensuse:tumbleweed-core-amd64-generic-v3.2.1"
				_, err := PullImage(image)
				Expect(err).ToNot(HaveOccurred())

				out, err := aurora.Run("--debug",
					"--set", "disable_http_server=true",
					"--set", "disable_netboot=true",
					"--set", "container_image=oci://"+image,
					"--set", "state_dir=/tmp/auroraboot",
					"--set", "disk.efi=true",
					"--set", "disk.boot_active=true",
					"--set", "disk.no_recovery=true",
					"--set", "disk.state_slots=1",
					"--cloud-config", "/config.yaml",
				)
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("Skipping RECOVERY image"), out)
				Expect(out).To(ContainSubstring("Created STATE image"), out)
				rawDisk := filepath.Join(tempDir, "kairos-opensuse-tumbleweed-core-amd64-generic-v3.2.1.raw")

				disk, err := diskfs.Open(rawDisk, diskfs.WithOpenMode(diskfs.ReadOnly))
				Expect(err).ToNot(HaveOccurred())
				defer disk.Close()
				table, err := disk.GetPartitionTable()
				Expect(err).ToNot(HaveOccurred())
				gptTable, ok := table.(*gpt.Table)
				Expect(ok).To(BeTrue())
				Expect(gptTable.Partitions).To(HaveLen(3))
				Expect(gptTable.Partitions[1].Name).To(Equal("oem"))
				Expect(gptTable.Partitions[2].Name).To(Equal("state"))
			})
			It("generates a gce image", func() {
				image := "quay.io/kairos/opensuse:tumbleweed-core-amd64-generic-v3.2.1"
				_, err := PullImage(image)
				Expect(err).ToNot(HaveOccurred())

				out, err := aurora.Run("--debug",
					"--set", "disable_http_server=true",
					"--set", "disable_netboot=true",
					"--set", "container_image=oci://"+image,
					"--set", "state_dir=/tmp/auroraboot",
					"--set", "disk.gce=true",
					"--cloud-config", "/config.yaml",
				)
				Expect(out).To(ContainSubstring("Generating raw disk"), out)
				Expect(out).To(ContainSubstring(constants.OpGenEFIRawDisk), out)
				Expect(out).To(ContainSubstring(constants.OpConvertGCE), out)
				Expect(out).To(ContainSubstring(constants.OpDumpSource), out)
				Expect(err).ToNot(HaveOccurred(), out)
				_, err = os.Stat(filepath.Join(tempDir, "kairos-opensuse-tumbleweed-core-amd64-generic-v3.2.1.raw.gce.tar.gz"))
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(err).ToNot(HaveOccurred(), out)
				// Open the file and check that there is a disk.raw file inside and check that its rounded to a GB
				file, err := os.Open(filepath.Join(tempDir, "kairos-opensuse-tumbleweed-core-amd64-generic-v3.2.1.raw.gce.tar.gz"))
				Expect(err).ToNot(HaveOccurred(), out)
				defer file.Close()
				// Create a gzip reader
				gzr, err := gzip.NewReader(file)
				Expect(err).ToNot(HaveOccurred(), out)
				defer gzr.Close()

				tr := tar.NewReader(gzr)
				found := false
				for {
					hdr, err := tr.Next()
					if err != nil {
						break
					}
					if hdr.Name == "disk.raw" {
						found = true
						Expect(hdr.Size).To(BeNumerically(">", 1<<30), out)
					}
				}
				Expect(found).To(BeTrue(), out)
				Expect(err).ToNot(HaveOccurred(), out)
			})
			It("generates a vhd image", func() {
				image := "quay.io/kairos/opensuse:tumbleweed-core-amd64-generic-v3.2.1"
				_, err := PullImage(image)
				Expect(err).ToNot(HaveOccurred())

				out, err := aurora.Run("--debug",
					"--set", "disable_http_server=true",
					"--set", "disable_netboot=true",
					"--set", "container_image=oci://"+image,
					"--set", "state_dir=/tmp/auroraboot",
					"--set", "disk.vhd=true",
					"--cloud-config", "/config.yaml",
				)
				Expect(out).To(ContainSubstring("Generating raw disk"), out)
				Expect(out).To(ContainSubstring(constants.OpGenEFIRawDisk), out)
				Expect(out).To(ContainSubstring(constants.OpConvertVHD), out)
				Expect(out).To(ContainSubstring(constants.OpDumpSource), out)
				Expect(err).ToNot(HaveOccurred(), out)
				_, err = os.Stat(filepath.Join(tempDir, "kairos-opensuse-tumbleweed-core-amd64-generic-v3.2.1.raw.vhd"))
				Expect(err).ToNot(HaveOccurred(), out)
			})
		})
		Describe("Partition images", Label("partitions"), func() {
			It("emits individual partition image files instead of a merged disk", func() {
				image := "quay.io/kairos/opensuse:tumbleweed-core-amd64-generic-v3.2.1"
				_, err := PullImage(image)
				Expect(err).ToNot(HaveOccurred())

				out, err := aurora.Run("--debug",
					"--set", "disable_http_server=true",
					"--set", "disable_netboot=true",
					"--set", "container_image=oci://"+image,
					"--set", "state_dir=/tmp/auroraboot",
					"--set", "disk.partitions=true",
					"--cloud-config", "/config.yaml",
				)

				Expect(out).To(ContainSubstring(constants.OpGenEFIRawDisk), out)
				Expect(out).To(ContainSubstring(constants.OpDumpSource), out)
				Expect(out).To(ContainSubstring("Emitting individual partition images"), out)
				Expect(err).ToNot(HaveOccurred(), out)

				// The three partition images are produced with the script-matching names.
				for _, name := range []string{"efi.img", "oem.img", "recovery_partition.img"} {
					info, statErr := os.Stat(filepath.Join(tempDir, name))
					Expect(statErr).ToNot(HaveOccurred(), out)
					Expect(info.Size()).To(BeNumerically(">", 0), name)
				}

				// No merged .raw disk is produced in partitions mode.
				matches, globErr := filepath.Glob(filepath.Join(tempDir, "kairos-*.raw"))
				Expect(globErr).ToNot(HaveOccurred())
				Expect(matches).To(BeEmpty(), out)
			})
			It("rejects disk.partitions combined with disk.gce before building", func() {
				out, err := aurora.Run("--debug",
					"--set", "disable_http_server=true",
					"--set", "disable_netboot=true",
					"--set", "container_image=oci://quay.io/kairos/opensuse:tumbleweed-core-amd64-generic-v3.2.1",
					"--set", "state_dir=/tmp/auroraboot",
					"--set", "disk.partitions=true",
					"--set", "disk.gce=true",
					"--cloud-config", "/config.yaml",
				)
				Expect(err).To(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("disk.partitions cannot be combined with disk.gce"), out)
				// Fail fast: never reaches the build/dump step.
				Expect(out).ToNot(ContainSubstring("Emitting individual partition images"), out)
			})
		})
		Describe("MBR", Label("mbr"), func() {
			It("generates a raw image", func() {
				image := "quay.io/kairos/opensuse:tumbleweed-core-amd64-generic-v3.2.1"
				_, err := PullImage(image)
				Expect(err).ToNot(HaveOccurred())

				out, err := aurora.Run("--debug",
					"--set", "disable_http_server=true",
					"--set", "disable_netboot=true",
					"--set", "container_image=oci://"+image,
					"--set", "state_dir=/tmp/auroraboot",
					"--set", "disk.bios=true",
					"--cloud-config", "/config.yaml",
				)
				Expect(out).To(ContainSubstring("Generating raw disk"), out)
				Expect(out).To(ContainSubstring(constants.OpGenBIOSRawDisk), out)
				Expect(out).To(ContainSubstring(constants.OpDumpSource), out)
				Expect(err).ToNot(HaveOccurred(), out)
				_, err = os.Stat(filepath.Join(tempDir, "kairos-opensuse-tumbleweed-core-amd64-generic-v3.2.1.raw"))
				Expect(err).ToNot(HaveOccurred(), out)
			})
		})
	})
})
