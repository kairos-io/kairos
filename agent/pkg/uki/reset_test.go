/*
Copyright © 2026 Kairos authors

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

package uki

import (
	"bytes"
	"errors"
	"os"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Uki reset action", func() {
	var config *sdkConfig.Config
	var fs vfs.FS
	var logger sdkLogger.KairosLogger
	var runner *v1mock.FakeRunner
	var mounter *v1mock.ErrorMounter
	var syscallMock *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var extractor *v1mock.FakeImageExtractor
	var cleanup func()
	var memLog *bytes.Buffer
	var spec *v1.ResetUkiSpec
	var reset *ResetAction
	var ghwTest ghwMock.GhwMock

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscallMock = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		extractor = v1mock.NewFakeImageExtractor(logger)
		cloudInit = &v1mock.FakeCloudInitRunner{}

		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())

		Expect(fsutils.MkdirAll(fs, "/efi/EFI/kairos", constants.DirPerm)).To(Succeed())
		Expect(fsutils.MkdirAll(fs, "/efi/loader/entries", constants.DirPerm)).To(Succeed())

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

		spec = &v1.ResetUkiSpec{
			Partitions: sdkPartitions.ElementalPartitions{
				EFI: &sdkPartitions.Partition{
					FilesystemLabel: "COS_GRUB",
					FS:              "vfat",
					MountPoint:      "/efi",
					Path:            "/dev/device1",
					Name:            "efi",
				},
				OEM: &sdkPartitions.Partition{
					FilesystemLabel: "COS_OEM",
					FS:              "ext4",
					MountPoint:      "/oem",
					Path:            "/dev/device2",
					Name:            "oem",
				},
				Persistent: &sdkPartitions.Partition{
					FilesystemLabel: "COS_PERSISTENT",
					FS:              "ext4",
					MountPoint:      "/usr/local",
					Path:            "/dev/device3",
					Name:            "persistent",
				},
			},
		}
		reset = NewResetAction(config, spec)

		mainDisk := sdkPartitions.Disk{
			Name: "device",
			Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "COS_GRUB",
					FS:              "vfat",
					MountPoint:      "/efi",
				},
			},
		}
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			GinkgoWriter.Printf(memLog.String())
		}
		ghwTest.Clean()
		cleanup()
	})

	It("fails when the EFI partition can not be remounted RW", func() {
		// strict mode also surfaces the pre-reset stage errors
		config.Strict = true
		mounter.ErrorOnMount = true
		err := reset.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mount"))
	})

	It("fails when the OEM partition can not be unmounted", func() {
		spec.FormatOEM = true
		// make the OEM partition look mounted
		Expect(mounter.Mount("/dev/device2", "/oem", "ext4", []string{"rw"})).To(Succeed())
		mounter.ErrorOnUnmount = true

		err := reset.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unmount"))
	})

	It("fails when formatting the OEM partition fails", func() {
		spec.FormatOEM = true
		runner.ReturnError = errors.New("mkfs error")
		err := reset.Run()
		Expect(err).To(HaveOccurred())
	})

	It("fails when the OEM partition can not be mounted back", func() {
		spec.FormatOEM = true
		mounter.ErrorOnMount = true
		err := reset.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mount"))
	})

	It("fails when formatting the persistent partition fails", func() {
		spec.FormatPersistent = true
		runner.ReturnError = errors.New("mkfs error")
		err := reset.Run()
		Expect(err).To(HaveOccurred())
	})

	It("fails when copying recovery artifacts to active fails", func() {
		// a recovery prefixed conf file is parsed with an os based reader
		// which does not see the test fs, so the copy fails
		Expect(fs.WriteFile("/efi/loader/entries/recovery.conf",
			[]byte("title Kairos\nefi /EFI/kairos/recovery.efi\n"), os.ModePerm)).To(Succeed())

		err := reset.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("copying recovery to active"))
	})

	It("resets successfully selecting the active boot entry", func() {
		// uki mode so the boot entry selection goes through systemd-boot
		Expect(fsutils.MkdirAll(fs, "/proc", constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile("/proc/cmdline", []byte("rd.immucore.uki"), os.ModePerm)).To(Succeed())
		Expect(fsutils.MkdirAll(fs, "/sys/firmware/efi/efivars", constants.DirPerm)).To(Succeed())

		// conf files are parsed with an os based reader while artifacts are
		// managed through the config fs, so the EFI mountpoint is mirrored in
		// a real temporary directory and in the test fs
		efiDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(efiDir+"/loader/entries", 0755)).To(Succeed())
		Expect(fsutils.MkdirAll(fs, efiDir+"/loader/entries", constants.DirPerm)).To(Succeed())
		writeBoth := func(path, content string) {
			Expect(os.WriteFile(path, []byte(content), 0644)).To(Succeed())
			Expect(fs.WriteFile(path, []byte(content), os.ModePerm)).To(Succeed())
		}
		writeBoth(efiDir+"/loader/loader.conf", "timeout 5\n")
		writeBoth(efiDir+"/loader/entries/active+2-1.conf", "title kairos\nefi /EFI/kairos/active.efi\n")
		writeBoth(efiDir+"/loader/entries/passive+3.conf", "title kairos (fallback)\nefi /EFI/kairos/passive.efi\n")
		writeBoth(efiDir+"/loader/entries/recovery+1-2.conf", "title kairos recovery\nefi /EFI/kairos/recovery.efi\n")
		writeBoth(efiDir+"/loader/entries/statereset+2-1.conf", "title kairos state reset (auto)\nefi /EFI/kairos/statereset.efi\n")

		spec.Partitions.EFI.MountPoint = efiDir
		// the boot entry selection looks the EFI partition up via ghw
		ghwTest.Clean()
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{
			Name: "device",
			Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "COS_GRUB",
					FS:              "vfat",
					MountPoint:      efiDir,
				},
			},
		})
		ghwTest.CreateDevices()

		// recovery artifact to rotate to active in the default UKI dir
		Expect(fs.WriteFile("/efi/EFI/kairos/recovery.efi", []byte("recovery"), os.ModePerm)).To(Succeed())

		spec.FormatPersistent = true
		spec.FormatOEM = true
		Expect(reset.Run()).To(Succeed())

		// recovery was copied to active
		exists, _ := fsutils.Exists(fs, "/efi/EFI/kairos/active.efi")
		Expect(exists).To(BeTrue())
		// the one shot boot entry was written
		exists, _ = fsutils.Exists(fs, "/sys/firmware/efi/efivars/LoaderEntryOneShot-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f")
		Expect(exists).To(BeTrue())
	})

	It("formats partitions, rotates recovery artifacts and fails on boot entry selection", func() {
		// uki mode so the boot entry selection goes through systemd-boot
		Expect(fsutils.MkdirAll(fs, "/proc", constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile("/proc/cmdline", []byte("rd.immucore.uki"), os.ModePerm)).To(Succeed())

		spec.FormatPersistent = true
		spec.FormatOEM = true
		Expect(fs.WriteFile("/efi/EFI/kairos/recovery.efi", []byte("recovery"), os.ModePerm)).To(Succeed())

		// there are no loader entries, so selecting the boot entry fails after
		// everything else has been done
		err := reset.Run()
		Expect(err).To(HaveOccurred())

		// recovery was copied to active
		exists, _ := fsutils.Exists(fs, "/efi/EFI/kairos/active.efi")
		Expect(exists).To(BeTrue())
		exists, _ = fsutils.Exists(fs, "/efi/EFI/kairos/recovery.efi")
		Expect(exists).To(BeTrue())
	})
})
