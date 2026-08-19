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
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Uki install action", func() {
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
	var spec *v1.InstallUkiSpec
	var installer *InstallAction

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

		Expect(fsutils.MkdirAll(fs, "/source/EFI/BOOT", constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile("/source/EFI/BOOT/bootx64.efi", []byte("efi data"), os.ModePerm)).To(Succeed())
		Expect(fsutils.MkdirAll(fs, "/efi", constants.DirPerm)).To(Succeed())

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

		spec = &v1.InstallUkiSpec{
			Target:   "/dev/device",
			NoFormat: true,
			Active: sdkImages.Image{
				Source: sdkImages.NewDirSrc("/source"),
			},
		}
		spec.Partitions.EFI = &sdkPartitions.Partition{
			FilesystemLabel: "COS_GRUB",
			FS:              "vfat",
			MountPoint:      "/efi",
			Path:            "/dev/device1",
			Name:            "efi",
		}
		installer = NewInstallAction(config, spec)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			GinkgoWriter.Printf(memLog.String())
		}
		cleanup()
	})

	It("installs successfully when skipping format", func() {
		Expect(installer.Run()).To(Succeed())
		// the default EFI dir structure was created
		exists, _ := fsutils.Exists(fs, constants.EfiDir+"/EFI/BOOT")
		Expect(exists).To(BeTrue())
	})

	It("detects a pre-configured device when target is auto and no-format is set", func() {
		mainDisk := sdkPartitions.Disk{
			Name: "device",
			Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "COS_STATE",
					FS:              "ext4",
				},
			},
		}
		ghwTest := ghwMock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()
		defer ghwTest.Clean()

		spec.Target = "auto"
		Expect(installer.Run()).To(Succeed())
		Expect(spec.Target).To(Equal("/dev/device"))
	})

	It("continues with an empty target when no pre-configured device is found", func() {
		// ghw is pointed to a fake env where no COS_STATE partition exists, so
		// DetectPreConfiguredDevice returns an empty device and no error
		ghwTest := ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "vdevice"})
		ghwTest.CreateDevices()
		defer ghwTest.Clean()

		spec.Target = ""
		Expect(installer.Run()).To(Succeed())
		Expect(spec.Target).To(BeEmpty())
	})

	It("fails when partitions can not be mounted", func() {
		mounter.ErrorOnMount = true
		err := installer.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mount"))
	})

	It("fails when deactivating devices fails", func() {
		spec.NoFormat = false
		runner.ReturnError = errors.New("blkdeactivate failure")
		err := installer.Run()
		Expect(err).To(HaveOccurred())
	})

	It("fails the before-install hook in strict mode", func() {
		// without /proc/cmdline in the test fs every stage errors out, which
		// is fatal in strict mode once the before-install hook runs
		config.Strict = true
		err := installer.Run()
		Expect(err).To(HaveOccurred())
	})

	It("fails when the device can not be partitioned", func() {
		spec.NoFormat = false
		spec.Target = "/dev/uki-test-nonexistent-device"
		err := installer.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not exist"))
	})

	It("fails when the cloud config can not be copied", func() {
		spec.CloudInit = []string{"/uki-test-missing-cloud-config.yaml"}
		err := installer.Run()
		Expect(err).To(HaveOccurred())
	})

	It("fails when the EFI directories can not be created", func() {
		// a file in the way of the default EFI dir makes MkdirAll fail
		Expect(fsutils.MkdirAll(fs, "/run/cos", constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile(constants.EfiDir, []byte("not a dir"), os.ModePerm)).To(Succeed())

		err := installer.Run()
		Expect(err).To(HaveOccurred())
	})

	It("fails when the source can not be dumped", func() {
		spec.Active.Source = sdkImages.NewEmptySrc()
		err := installer.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown image source type"))
	})

	It("fails when the post-install hooks fail", func() {
		// grub options force the Finish hook to write the OEM grubenv file,
		// which fails because a directory is in its place
		config.Install.GrubOptions = map[string]string{"foo": "bar"}
		Expect(fsutils.MkdirAll(fs, "/oem/grubenv", constants.DirPerm)).To(Succeed())
		err := installer.Run()
		Expect(err).To(HaveOccurred())
	})

	It("fails when a conf file in the EFI partition can not be parsed", func() {
		// conf files are parsed with an os based reader which does not see
		// the test fs, so any conf file in the EFI partition fails the walk
		Expect(fsutils.MkdirAll(fs, "/efi/loader/entries", constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile("/efi/loader/entries/foo.conf", []byte("cmdline test\n"), os.ModePerm)).To(Succeed())

		err := installer.Run()
		Expect(err).To(HaveOccurred())
	})

	Describe("with conf files visible to both the test fs and the OS", func() {
		// conf files are parsed with an os based reader while artifacts are
		// managed through the config fs, so the EFI mountpoint is mirrored in
		// a real temporary directory and in the test fs
		var efiDir string

		writeBoth := func(path, content string) {
			Expect(os.WriteFile(path, []byte(content), 0644)).To(Succeed())
			Expect(fs.WriteFile(path, []byte(content), os.ModePerm)).To(Succeed())
		}

		BeforeEach(func() {
			efiDir = GinkgoT().TempDir()
			Expect(fsutils.MkdirAll(fs, efiDir+"/loader/entries", constants.DirPerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, efiDir+"/EFI/kairos", constants.DirPerm)).To(Succeed())
			Expect(os.MkdirAll(efiDir+"/loader/entries", 0755)).To(Succeed())
			Expect(os.MkdirAll(efiDir+"/EFI/kairos", 0755)).To(Succeed())
			spec.Partitions.EFI.MountPoint = efiDir
		})

		It("skips entries and rotates the unassigned artifacts to all roles", func() {
			spec.SkipEntries = []string{"interactive-install"}
			writeBoth(efiDir+"/loader/entries/skipme.conf",
				"title Kairos\ncmdline foo interactive-install bar\nefi /EFI/kairos/skipme.efi\n")
			writeBoth(efiDir+"/loader/entries/keep.conf",
				"title Kairos\ncmdline console=tty1\n")
			writeBoth(efiDir+"/loader/entries/nocmd.conf", "title Kairos\n")
			// referenced by skipme.conf, only needs to exist in the test fs
			Expect(fs.WriteFile(efiDir+"/EFI/kairos/skipme.efi", []byte("data"), os.ModePerm)).To(Succeed())
			// unassigned artifact, present in both so the rotation works end to end
			writeBoth(efiDir+"/EFI/kairos/"+UnassignedArtifactRole+".efi", "artifact")

			Expect(installer.Run()).To(Succeed())

			// the skipped entry and its efi file are gone
			exists, _ := fsutils.Exists(fs, efiDir+"/loader/entries/skipme.conf")
			Expect(exists).To(BeFalse())
			exists, _ = fsutils.Exists(fs, efiDir+"/EFI/kairos/skipme.efi")
			Expect(exists).To(BeFalse())
			// the kept entry got a boot assessment
			exists, _ = fsutils.Exists(fs, efiDir+"/loader/entries/keep+3.conf")
			Expect(exists).To(BeTrue())
			// the unassigned artifact was installed under every role
			for _, role := range []string{"active", "passive", "recovery", "statereset"} {
				exists, _ = fsutils.Exists(fs, efiDir+"/EFI/kairos/"+role+".efi")
				Expect(exists).To(BeTrue(), role)
			}
		})

		It("fails when the artifact set conf can not be rewritten for a role", func() {
			// the copied conf only exists in the test fs, so rewriting its
			// keys through the os based reader fails
			writeBoth(efiDir+"/EFI/kairos/"+UnassignedArtifactRole+".conf",
				"title Kairos\nefi /EFI/kairos/"+UnassignedArtifactRole+".efi\n")

			err := installer.Run()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("installing the new artifact set"))
		})

		It("fails when the unassigned artifacts can not be removed", func() {
			// present in the test fs only, so the os based removal fails
			Expect(fs.WriteFile(efiDir+"/EFI/kairos/"+UnassignedArtifactRole+".efi", []byte("artifact"), os.ModePerm)).To(Succeed())

			err := installer.Run()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("removing artifact set"))
		})
	})

	Describe("SkipEntry", func() {
		BeforeEach(func() {
			Expect(fsutils.MkdirAll(fs, "/efi/EFI/kairos", constants.DirPerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, "/efi/loader/entries", constants.DirPerm)).To(Succeed())
		})

		It("removes the efi file and its conf", func() {
			Expect(fs.WriteFile("/efi/EFI/kairos/interactive.efi", []byte("data"), os.ModePerm)).To(Succeed())
			Expect(fs.WriteFile("/efi/loader/entries/interactive.conf", []byte("data"), os.ModePerm)).To(Succeed())

			conf := map[string]string{"efi": "/EFI/kairos/interactive.efi"}
			Expect(installer.SkipEntry("/efi/loader/entries/interactive.conf", conf)).To(Succeed())

			exists, _ := fsutils.Exists(fs, "/efi/EFI/kairos/interactive.efi")
			Expect(exists).To(BeFalse())
			exists, _ = fsutils.Exists(fs, "/efi/loader/entries/interactive.conf")
			Expect(exists).To(BeFalse())
		})

		It("fails when the efi file does not exist", func() {
			conf := map[string]string{"efi": "/EFI/kairos/missing.efi"}
			Expect(installer.SkipEntry("/efi/loader/entries/missing.conf", conf)).ToNot(Succeed())
		})

		It("does nothing when the conf has no efi key", func() {
			Expect(installer.SkipEntry("/whatever", map[string]string{})).To(Succeed())
		})
	})

	Describe("Hook", func() {
		It("ignores hook errors when not in strict mode", func() {
			cloudInit.Error = true
			config.Strict = false
			Expect(Hook(config, constants.AfterInstallHook)).To(Succeed())
		})

		It("fails on hook errors when in strict mode", func() {
			cloudInit.Error = true
			config.Strict = true
			Expect(Hook(config, constants.AfterInstallHook)).ToNot(Succeed())
		})
	})
})
