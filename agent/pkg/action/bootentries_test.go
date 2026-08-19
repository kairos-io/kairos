package action

import (
	"bytes"
	"os"
	"path/filepath"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// TODO: Mock the syscall.StatFS to simulate and test RO/RW partitions and how it mounts it and unmounts it

// Keep a reference to the original version probe before any test overrides it
var origGetSystemdBootMajorVersion = getSystemdBootMajorVersion

var _ = Describe("Bootentries tests", Label("bootentry"), func() {
	var config *sdkConfig.Config
	var fs vfs.FS
	var logger sdkLogger.KairosLogger
	var runner *v1mock.FakeRunner
	var mounter *v1mock.ErrorMounter
	var syscallMock *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var extractor *v1mock.FakeImageExtractor
	var ghwTest ghwMock.GhwMock

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscallMock = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		extractor = v1mock.NewFakeImageExtractor(logger)
		logger.SetLevel("debug")
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		// Create proper dir structure for our EFI partition contentens
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/efi/loader/entries", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/efi/EFI/BOOT", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/efi/EFI/kairos", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/etc/cos/", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/run/initramfs/cos-state/grub/", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/etc/kairos/branding/", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/sys/firmware/efi/efivars/", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())

		cloudInit = &v1mock.FakeCloudInitRunner{}
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscallMock),
			agentConfig.WithClient(client),
			agentConfig.WithCloudInitRunner(cloudInit),
			agentConfig.WithImageExtractor(extractor),
		)
		config.Collector = collector.Config{}

		mainDisk := sdkPartitions.Disk{
			Name: "device",
			Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "COS_GRUB",
					FS:              "ext4",
					MountPoint:      "/efi",
				},
			},
		}
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()
	})

	AfterEach(func() {
		ghwTest.Clean()
		cleanup()
	})
	Context("Under Uki", func() {
		BeforeEach(func() {
			err := fs.Mkdir("/proc", os.ModeDir|os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/proc/cmdline", []byte("rd.immucore.uki"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			// Reset the version probe to the default (0 = unknown → 257+ behaviour)
			// so tests are isolated from each other.
			getSystemdBootMajorVersion = func(_ string) uint16 { return 0 }
		})
		Context("ListBootEntries", func() {
			It("fails to list the boot entries when there is no loader.conf", func() {
				err := ListBootEntries(config)
				Expect(err).To(HaveOccurred())
			})
			It("fails to list the boot entries when the EFI partition is missing", func() {
				ghwTest.Clean()
				err := ListBootEntries(config)
				Expect(err).To(HaveOccurred())
			})
			It("fails to list the boot entries when the EFI partition cannot be mounted", func() {
				mounter.ErrorOnMount = true
				err := ListBootEntries(config)
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("could not mount EFI partition"))
			})
			It("defaults to the EFI dir when the EFI partition has no mountpoint", func() {
				// Recreate the ghw mock with an EFI partition without a mountpoint
				ghwTest.Clean()
				ghwTest = ghwMock.GhwMock{}
				ghwTest.AddDisk(sdkPartitions.Disk{
					Name: "device",
					Partitions: []*sdkPartitions.Partition{
						{
							Name:            "device1",
							FilesystemLabel: "COS_GRUB",
							FS:              "ext4",
						},
					},
				})
				ghwTest.CreateDevices()
				// The prompt fails as there is no TTY, but the partition was mounted on the default dir
				err := ListBootEntries(config)
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Mounting partition COS_GRUB"))
				// The partition is unmounted again by the cleanup
				_, err = fs.Stat(cnst.EfiDir)
				Expect(err).ToNot(HaveOccurred())
			})
		})
		Context("ListSystemdEntries", func() {
			It("lists the boot entries if there is any", func() {
				err := fs.WriteFile("/efi/loader/loader.conf", []byte("timeout 5\ndefault kairos\nrecovery kairos2\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/active.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive.conf", []byte("title kairos (fallback)\nefi /EFI/kairos/passive.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery.conf", []byte("title kairos recovery\nefi /EFI/kairos/recovery.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset.conf", []byte("title kairos state reset (auto)\nefi /EFI/kairos/statereset.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				entries, err := listSystemdEntries(config, &sdkPartitions.Partition{MountPoint: "/efi"})
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(HaveLen(4))
				Expect(entries).To(ContainElement("cos"))
				Expect(entries).To(ContainElement("fallback"))
				Expect(entries).To(ContainElement("recovery"))
				Expect(entries).To(ContainElement("statereset"))

			})
			It("list empty boot entries if there is none", func() {
				entries, err := listSystemdEntries(config, &sdkPartitions.Partition{MountPoint: "/efi"})
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(HaveLen(0))
			})
		})
		Context("SelectBootEntry", func() {
			It("fails to select the boot entry if it doesnt exist", func() {
				err := SelectBootEntry(config, "kairos")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not exist"))
			})
			It("works without boot assessment", func() {
				err := fs.WriteFile("/efi/loader/entries/active.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte("default active.conf"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = SelectBootEntry(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active"))
				reader, err := utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active.conf"))
			})

			It("selects the boot entry in a default installation", func() {
				err := fs.WriteFile("/efi/loader/entries/active+2-1.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive+3.conf", []byte("title kairos (fallback)\nefi /EFI/kairos/passive.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery+1-2.conf", []byte("title kairos recovery\nefi /EFI/kairos/recovery.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset+2-1.conf", []byte("title kairos state reset (auto)\nefi /EFI/kairos/statereset.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte(""), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = SelectBootEntry(config, "fallback")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to fallback"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("passive.conf"))

				err = SelectBootEntry(config, "recovery")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to recovery"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("recovery.conf"))

				err = SelectBootEntry(config, "statereset")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to statereset"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("statereset.conf"))

				err = SelectBootEntry(config, "cos")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to cos"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("active.conf"))

				// also works using active (we want to get rid of the word cos later but this also needs to be applied in GRUB)
				err = SelectBootEntry(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("active.conf"))
			})

			It("selects the boot entry in a extend-cmdline installation with boot branding", func() {
				err := fs.WriteFile("/efi/loader/entries/active_install-mode_awesomeos.conf", []byte("title awesomeos\nefi /EFI/kairos/active_install-mode_awesomeos.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive_install-mode_awesomeos+3.conf", []byte("title awesomeos (fallback)\nefi /EFI/kairos/passive_install-mode_awesomeos.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery_install-mode_awesomeos.conf", []byte("title awesomeos recovery\nefi /EFI/kairos/recovery_install-mode_awesomeos.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset_install-mode_awesomeos.conf", []byte("title awesomeos state reset (auto)\nefi /EFI/kairos/statereset_install-mode_awesomeos.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte(""), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = SelectBootEntry(config, "fallback")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to fallback"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("passive_install-mode_awesomeos.conf"))

				err = SelectBootEntry(config, "recovery")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to recovery"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("recovery_install-mode_awesomeos.conf"))

				err = SelectBootEntry(config, "statereset")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to statereset"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("statereset_install-mode_awesomeos.conf"))

				err = SelectBootEntry(config, "cos")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to cos"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("active_install-mode_awesomeos.conf"))

				// also works using active (we want to get rid of the word cos later but this also needs to be applied in GRUB)
				err = SelectBootEntry(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("active_install-mode_awesomeos.conf"))
			})

			It("selects the boot entry in a extra-cmdline installation", func() {
				err := fs.WriteFile("/efi/loader/entries/active.conf", []byte("title Kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/active_foobar.conf", []byte("title Kairos\nefi /EFI/kairos/active_foobar.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive+3.conf", []byte("title Kairos (fallback)\nefi /EFI/kairos/passive.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive_foobar.conf", []byte("title Kairos (fallback)\nefi /EFI/kairos/passive_foobar.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery+3.conf", []byte("title Kairos recovery\nefi /EFI/kairos/recovery.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery_foobar.conf", []byte("title Kairos recovery\nefi /EFI/kairos/recovery_foobar.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset.conf", []byte("title Kairos state reset (auto)\nefi /EFI/kairos/statereset.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset_foobar.conf", []byte("title Kairos state reset (auto)\nefi /EFI/kairos/state_reset_foobar.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte("default active.conf"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = SelectBootEntry(config, "fallback")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to fallback"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("passive.conf"))

				err = SelectBootEntry(config, "fallback foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to fallback foobar"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("passive_foobar.conf"))

				err = SelectBootEntry(config, "recovery")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to recovery"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("recovery.conf"))

				err = SelectBootEntry(config, "recovery foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to recovery foobar"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("recovery_foobar.conf"))

				err = SelectBootEntry(config, "statereset")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to statereset"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("statereset.conf"))

				err = SelectBootEntry(config, "statereset foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to statereset foobar"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("statereset_foobar.conf"))

				err = SelectBootEntry(config, "cos")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to cos"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("active.conf"))

				err = SelectBootEntry(config, "cos foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to cos foobar"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("active_foobar.conf"))

				// also works using active (we want to get rid of the word cos later but this also needs to be applied in GRUB)
				err = SelectBootEntry(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("active.conf"))

				err = SelectBootEntry(config, "active foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active foobar"))
				Expect(ReadOneShotEfiVar(config)).To(Equal("active_foobar.conf"))
			})

			It("fails when the EFI partition cannot be found", func() {
				ghwTest.Clean()
				err := SelectBootEntry(config, "cos")
				Expect(err).To(HaveOccurred())
			})

			It("fails when the efi var cannot be written", func() {
				err := fs.WriteFile("/efi/loader/entries/active.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte("default active.conf"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				Expect(fs.RemoveAll("/sys/firmware/efi/efivars")).To(Succeed())

				err = SelectBootEntry(config, "active")
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("could not write EFI variable"))
			})

			// systemd-boot 256 requires the boot assessment suffix in the EFI variable entry ID.
			Context("systemd-boot 256 workaround", func() {
				BeforeEach(func() {
					getSystemdBootMajorVersion = func(_ string) uint16 { return 256 }
				})

				It("includes the assessment suffix in the EFI var for a default installation", func() {
					err := fs.WriteFile("/efi/loader/entries/active+2-1.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/passive+3.conf", []byte("title kairos (fallback)\nefi /EFI/kairos/passive.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/recovery+1-2.conf", []byte("title kairos recovery\nefi /EFI/kairos/recovery.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/statereset+2-1.conf", []byte("title kairos state reset (auto)\nefi /EFI/kairos/statereset.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/loader.conf", []byte(""), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())

					err = SelectBootEntry(config, "fallback")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("passive+3.conf"))

					err = SelectBootEntry(config, "recovery")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("recovery+1-2.conf"))

					err = SelectBootEntry(config, "statereset")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("statereset+2-1.conf"))

					err = SelectBootEntry(config, "cos")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("active+2-1.conf"))

					err = SelectBootEntry(config, "active")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("active+2-1.conf"))
				})

				It("fails when multiple assessment files match the selected entry", func() {
					err := fs.WriteFile("/efi/loader/entries/active+3.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/active+2-1.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/loader.conf", []byte(""), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())

					err = SelectBootEntry(config, "cos")
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("ambiguous"))
					Expect(memLog.String()).To(ContainSubstring("could not resolve boot entry"))
				})

				It("includes the assessment suffix in the EFI var for an extra-cmdline installation", func() {
					err := fs.WriteFile("/efi/loader/entries/active+3.conf", []byte("title Kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/active_foobar.conf", []byte("title Kairos\nefi /EFI/kairos/active_foobar.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/passive+3.conf", []byte("title Kairos (fallback)\nefi /EFI/kairos/passive.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/passive_foobar.conf", []byte("title Kairos (fallback)\nefi /EFI/kairos/passive_foobar.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/recovery+1.conf", []byte("title Kairos recovery\nefi /EFI/kairos/recovery.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/recovery_foobar.conf", []byte("title Kairos recovery\nefi /EFI/kairos/recovery_foobar.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/statereset+2.conf", []byte("title Kairos state reset (auto)\nefi /EFI/kairos/statereset.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/entries/statereset_foobar.conf", []byte("title Kairos state reset (auto)\nefi /EFI/kairos/statereset_foobar.efi\n"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
					err = fs.WriteFile("/efi/loader/loader.conf", []byte(""), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())

					// Entries with an assessment suffix use it in the EFI var
					err = SelectBootEntry(config, "fallback")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("passive+3.conf"))

					err = SelectBootEntry(config, "recovery")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("recovery+1.conf"))

					err = SelectBootEntry(config, "statereset")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("statereset+2.conf"))

					err = SelectBootEntry(config, "cos")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("active+3.conf"))

					// Entries without an assessment suffix use the plain name
					err = SelectBootEntry(config, "fallback foobar")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("passive_foobar.conf"))

					err = SelectBootEntry(config, "cos foobar")
					Expect(err).ToNot(HaveOccurred())
					Expect(ReadOneShotEfiVar(config)).To(Equal("active_foobar.conf"))
				})
			})
		})
	})

	Context("conf name conversions", func() {
		It("systemdConfToBootName fails for non conf files", func() {
			name, err := systemdConfToBootName("active")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown systemd-boot conf"))
			Expect(name).To(Equal(""))
		})
		It("systemdConfToBootName converts conf files to boot names", func() {
			Expect(systemdConfToBootName("active.conf")).To(Equal("cos"))
			Expect(systemdConfToBootName("active_foo.conf")).To(Equal("cos foo"))
			Expect(systemdConfToBootName("passive.conf")).To(Equal("fallback"))
			Expect(systemdConfToBootName("passive+3.conf")).To(Equal("fallback"))
			Expect(systemdConfToBootName("passive_bar.conf")).To(Equal("fallback bar"))
			Expect(systemdConfToBootName("recovery.conf")).To(Equal("recovery"))
			Expect(systemdConfToBootName("recovery_baz.conf")).To(Equal("recovery baz"))
			Expect(systemdConfToBootName("statereset.conf")).To(Equal("statereset"))
			Expect(systemdConfToBootName("statereset_qux.conf")).To(Equal("statereset qux"))
			Expect(systemdConfToBootName("my_custom_entry.conf")).To(Equal("my custom entry"))
		})
		It("bootNameToSystemdConf converts boot names to conf names", func() {
			Expect(bootNameToSystemdConf("cos")).To(Equal("active"))
			Expect(bootNameToSystemdConf("cos foo")).To(Equal("active_foo"))
			Expect(bootNameToSystemdConf("active")).To(Equal("active"))
			Expect(bootNameToSystemdConf("active foo")).To(Equal("active_foo"))
			Expect(bootNameToSystemdConf("fallback")).To(Equal("passive"))
			Expect(bootNameToSystemdConf("fallback bar")).To(Equal("passive_bar"))
			Expect(bootNameToSystemdConf("recovery")).To(Equal("recovery"))
			Expect(bootNameToSystemdConf("recovery baz")).To(Equal("recovery_baz"))
			Expect(bootNameToSystemdConf("statereset")).To(Equal("statereset"))
			Expect(bootNameToSystemdConf("statereset qux")).To(Equal("statereset_qux"))
			Expect(bootNameToSystemdConf("my custom entry")).To(Equal("my_custom_entry"))
		})
	})

	Context("efi variables", func() {
		It("ReadOneShotEfiVar fails when the variable does not exist", func() {
			_, err := ReadOneShotEfiVar(config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("couldn't open file"))
		})
		It("WriteOneShotEfiVar fails when the efivars dir does not exist", func() {
			Expect(fs.RemoveAll("/sys/firmware/efi/efivars")).To(Succeed())
			err := WriteOneShotEfiVar(config, "active.conf")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("couldn't open file"))
		})
		It("encodes and decodes UTF16 LE strings with NUL terminators", func() {
			encoded := EncondeUtf16LEStringNullTerminated("active.conf")
			Expect(ReadUtf16LEStringNullTerminated(encoded)).To(Equal("active.conf"))
		})
	})

	Context("getSystemdBootMajorVersion", func() {
		It("returns 0 when the systemd-boot binary cannot be read", func() {
			Expect(origGetSystemdBootMajorVersion("/nonexistent")).To(Equal(uint16(0)))
		})
	})

	Context("clearImmutable", func() {
		It("does nothing for non existing files", func() {
			Expect(clearImmutable("/this/does/not/exist/at/all")).To(Succeed())
		})
		It("returns an error when the file cannot be opened", func() {
			if os.Geteuid() == 0 {
				Skip("running as root, permissions are not enforced")
			}
			dir := GinkgoT().TempDir()
			file := filepath.Join(dir, "file")
			Expect(os.WriteFile(file, []byte("contents"), 0644)).To(Succeed())
			Expect(os.Chmod(dir, 0o000)).To(Succeed())
			DeferCleanup(func() {
				_ = os.Chmod(dir, 0o755)
			})
			Expect(clearImmutable(file)).ToNot(Succeed())
		})
		It("does nothing for files without the immutable flag", func() {
			f, err := os.CreateTemp("", "clearimmutable")
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(f.Name())
			Expect(f.Close()).To(Succeed())
			// A normal file has no immutable flag, so this should be a no-op.
			// Depending on the filesystem the ioctl may not be supported, in which
			// case an errno is returned, which is also a valid covered path.
			_ = clearImmutable(f.Name())
		})
	})

	Context("listSystemdEntries edge cases", func() {
		It("returns no entries when the entries dir does not exist", func() {
			entries, err := listSystemdEntries(config, &sdkPartitions.Partition{MountPoint: "/nonexistent"})
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(HaveLen(0))
		})
		It("skips dirs and non conf files", func() {
			err := fsutils.MkdirAll(fs, "/efi/loader/entries/somedir", os.ModeDir|os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/notaconf.txt", []byte("whatever"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/active.conf", []byte("title kairos\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			entries, err := listSystemdEntries(config, &sdkPartitions.Partition{MountPoint: "/efi"})
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(Equal([]string{"cos"}))
		})
	})

	Context("findEntryWithAssessment", func() {
		It("returns the base name unchanged when no assessment file exists", func() {
			err := fs.WriteFile("/efi/loader/entries/active.conf", []byte("title kairos\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			result, err := findEntryWithAssessment(config, "/efi", "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("active"))
		})
		It("returns the name with assessment when the file exists", func() {
			err := fs.WriteFile("/efi/loader/entries/active+3.conf", []byte("title kairos\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			result, err := findEntryWithAssessment(config, "/efi", "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("active+3"))
		})
		It("returns the name with compound assessment (N-M) when the file exists", func() {
			err := fs.WriteFile("/efi/loader/entries/passive+2-1.conf", []byte("title kairos (fallback)\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			result, err := findEntryWithAssessment(config, "/efi", "passive")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("passive+2-1"))
		})
		It("does not match a file with a different base name", func() {
			err := fs.WriteFile("/efi/loader/entries/active_foobar+3.conf", []byte("title kairos\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			result, err := findEntryWithAssessment(config, "/efi", "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal("active"))
		})
		It("returns an error when the entries dir cannot be read", func() {
			Expect(fs.RemoveAll("/efi/loader/entries")).To(Succeed())
			result, err := findEntryWithAssessment(config, "/efi", "active")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to inspect"))
			Expect(result).To(Equal("active"))
		})
		It("returns an error when multiple assessment files match the same base name", func() {
			err := fs.WriteFile("/efi/loader/entries/active+3.conf", []byte("title kairos\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/active+2-1.conf", []byte("title kairos\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			result, err := findEntryWithAssessment(config, "/efi", "active")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ambiguous"))
			Expect(result).To(Equal("active"))
		})
	})

	Context("Under grub", func() {
		Context("ListBootEntries", func() {
			It("fails to list the boot entries when there is no grub files", func() {
				err := ListBootEntries(config)
				Expect(err).To(HaveOccurred())
			})
			It("fails on the interactive prompt when there is no terminal", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("whatever whatever --id kairos {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				// There is no TTY in the test environment so the prompt fails
				err = ListBootEntries(config)
				Expect(err).To(HaveOccurred())
			})
		})
		Context("ListGrubEntries", func() {
			It("lists double-quoted grubcustom entries without an explicit id", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("menuentry \"Custom Entry\" {\n}"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				entries, err := listGrubEntries(config)
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(ConsistOf("Custom Entry"))
			})

			It("lists grubcustom entries without an explicit id", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("menuentry 'Custom Entry' {\n}"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				entries, err := listGrubEntries(config)
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(ConsistOf("Custom Entry"))
			})

			It("uses the explicit id instead of also listing the menuentry title", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("menuentry 'Custom Entry' --id customentry {\n}"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				entries, err := listGrubEntries(config)
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(ConsistOf("customentry"))
			})

			It("lists the boot entries if there is any", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("whatever whatever --id kairos {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/run/initramfs/cos-state/grub/grub.cfg", []byte("whatever whatever --id kairos2 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/etc/kairos/branding/grubmenu.cfg", []byte("whatever whatever --id kairos3 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				entries, err := listGrubEntries(config)
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(HaveLen(3))
				Expect(entries).To(ContainElement("kairos"))
				Expect(entries).To(ContainElement("kairos2"))
				Expect(entries).To(ContainElement("kairos3"))

			})
			It("list empty boot entries if there is none", func() {
				entries, err := listGrubEntries(config)
				Expect(err).To(HaveOccurred())
				Expect(entries).To(HaveLen(0))

			})
		})
		Context("SelectBootEntry", func() {
			BeforeEach(func() {
				runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
					switch cmd {
					default:
						return []byte{}, nil
					}
				}
			})
			It("fails to select the boot entry if it doesnt exist", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("whatever whatever --id kairos {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/run/initramfs/cos-state/grub/grub.cfg", []byte("whatever whatever --id kairos2 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/etc/kairos/branding/grubmenu.cfg", []byte("whatever whatever --id kairos3 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = SelectBootEntry(config, "nonexistant")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not exist"))
			})
			It("selects the boot entry", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("whatever whatever --id kairos {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/run/initramfs/cos-state/grub/grub.cfg", []byte("whatever whatever --id kairos2 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/etc/kairos/branding/grubmenu.cfg", []byte("whatever whatever --id kairos3 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				Expect(fs.Mkdir("/oem", os.ModePerm)).To(Succeed())

				err = SelectBootEntry(config, "kairos")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to kairos"))
				variables, err := utils.ReadPersistentVariables("/oem/grubenv", config)
				Expect(err).ToNot(HaveOccurred())
				Expect(variables["next_entry"]).To(Equal("kairos"))
			})
			It("fails to select the boot entry if there is no grub config", func() {
				err := SelectBootEntry(config, "kairos")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get any required GRUB configuration file"))
			})
			It("fails to select the boot entry if the grubenv cannot be written", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("whatever whatever --id kairos {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				// No /oem dir, so writing /oem/grubenv fails
				err = SelectBootEntry(config, "kairos")
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("could not set default boot entry"))
			})
			Context("with encrypted OEM", func() {
				BeforeEach(func() {
					config.Install = &sdkInstall.Install{Encrypt: []string{cnst.OEMLabel}}
					err := fs.WriteFile("/etc/cos/grub.cfg", []byte("whatever whatever --id kairos {"), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())
				})
				It("writes the next entry to the STATE grubenv", func() {
					// Recreate the ghw mock with a COS_STATE partition so the device can be found
					ghwTest.Clean()
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(sdkPartitions.Disk{
						Name: "device",
						Partitions: []*sdkPartitions.Partition{
							{
								Name:            "device1",
								FilesystemLabel: "COS_GRUB",
								FS:              "ext4",
								MountPoint:      "/efi",
							},
							{
								Name:            "device2",
								FilesystemLabel: "COS_STATE",
								FS:              "ext4",
							},
						},
					})
					ghwTest.CreateDevices()
					Expect(fsutils.MkdirAll(fs, cnst.StateDir, os.ModeDir|os.ModePerm)).To(Succeed())

					err := SelectBootEntry(config, "kairos")
					Expect(err).ToNot(HaveOccurred())
					Expect(memLog.String()).To(ContainSubstring("also writing to STATE partition's grubenv"))
					Expect(memLog.String()).To(ContainSubstring("Successfully set next_entry in STATE grubenv"))
					variables, err := utils.ReadPersistentVariables(filepath.Join(cnst.StateDir, cnst.GrubEnv), config)
					Expect(err).ToNot(HaveOccurred())
					Expect(variables["next_entry"]).To(Equal("kairos"))
					// Nothing should have been written to the OEM grubenv
					_, err = fs.Stat("/oem/grubenv")
					Expect(err).To(HaveOccurred())
				})
				It("warns when the STATE grubenv cannot be written", func() {
					// Recreate the ghw mock with a COS_STATE partition so the device can be found
					ghwTest.Clean()
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(sdkPartitions.Disk{
						Name: "device",
						Partitions: []*sdkPartitions.Partition{
							{
								Name:            "device2",
								FilesystemLabel: "COS_STATE",
								FS:              "ext4",
							},
						},
					})
					ghwTest.CreateDevices()
					// No StateDir in the fs, so writing the grubenv fails

					err := SelectBootEntry(config, "kairos")
					Expect(err).ToNot(HaveOccurred())
					Expect(memLog.String()).To(ContainSubstring("Could not set default boot entry in STATE grubenv"))
				})
				It("does nothing when the STATE device cannot be found", func() {
					// Default ghw mock has no COS_STATE partition
					err := SelectBootEntry(config, "kairos")
					Expect(err).ToNot(HaveOccurred())
					Expect(memLog.String()).To(ContainSubstring("Could not get STATE device by label"))
				})
				It("warns when the STATE partition cannot be remounted RW", func() {
					// Recreate the ghw mock with a COS_STATE partition so the device can be found
					ghwTest.Clean()
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(sdkPartitions.Disk{
						Name: "device",
						Partitions: []*sdkPartitions.Partition{
							{
								Name:            "device2",
								FilesystemLabel: "COS_STATE",
								FS:              "ext4",
							},
						},
					})
					ghwTest.CreateDevices()
					Expect(fsutils.MkdirAll(fs, cnst.StateDir, os.ModeDir|os.ModePerm)).To(Succeed())
					mounter.ErrorOnMount = true

					err := SelectBootEntry(config, "kairos")
					Expect(err).ToNot(HaveOccurred())
					Expect(memLog.String()).To(ContainSubstring("Could not remount STATE partition as RW"))
					variables, err := utils.ReadPersistentVariables(filepath.Join(cnst.StateDir, cnst.GrubEnv), config)
					Expect(err).ToNot(HaveOccurred())
					Expect(variables["next_entry"]).To(Equal("kairos"))
				})
			})
		})
	})
})
