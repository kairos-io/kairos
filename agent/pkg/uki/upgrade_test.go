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
	"fmt"
	"os"
	"path/filepath"

	"github.com/foxboron/go-uefi/efi/attributes"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Uki upgrade action", func() {
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
	var spec *v1.UpgradeUkiSpec
	var upgrader *UpgradeAction

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

		Expect(fsutils.MkdirAll(fs, "/efi/EFI/Kairos", constants.DirPerm)).To(Succeed())
		Expect(fsutils.MkdirAll(fs, "/source", constants.DirPerm)).To(Succeed())

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

		spec = &v1.UpgradeUkiSpec{
			Active: sdkImages.Image{
				Source: sdkImages.NewDirSrc("/source"),
			},
			EfiPartition: &sdkPartitions.Partition{
				FilesystemLabel: "COS_GRUB",
				FS:              "vfat",
				MountPoint:      "/efi",
				Path:            "/dev/device1",
				Name:            "efi",
			},
		}
		upgrader = NewUpgradeAction(config, spec)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			GinkgoWriter.Printf(memLog.String())
		}
		cleanup()
	})

	It("fails when the EFI partition can not be remounted RW", func() {
		mounter.ErrorOnMount = true
		err := upgrader.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mount"))
	})

	It("fails the active upgrade when the artifact has no valid signature", func() {
		// strict mode also surfaces the pre-upgrade stage errors
		config.Strict = true
		// the source contains no artifact, so the signature check fails before
		// any artifact rotation happens
		err := upgrader.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not exist"))
	})

	It("fails when the source can not be dumped", func() {
		spec.Active.Source = sdkImages.NewEmptySrc()
		err := upgrader.Run()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown image source type"))
	})

	Describe("with a validly signed artifact in place", func() {
		BeforeEach(func() {
			// redirect the efivars lookup to the test fs and install a db that
			// contains the certificate which signed the test artifact
			Expect(fsutils.MkdirAll(fs, "/sys/firmware/efi/efivars", constants.DirPerm)).To(Succeed())
			dbFile := fmt.Sprintf("db-%s", attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format())
			db, err := os.ReadFile("tests/db")
			Expect(err).ToNot(HaveOccurred())
			Expect(fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", dbFile), db, os.ModePerm)).To(Succeed())
			oldEfivars := attributes.Efivars
			rawEfivars, err := fs.(*vfst.TestFS).RawPath("/sys/firmware/efi/efivars")
			Expect(err).ToNot(HaveOccurred())
			attributes.Efivars = rawEfivars
			DeferCleanup(func() {
				attributes.Efivars = oldEfivars
			})

			// the signed artifact is already in place as the unassigned role
			signed, err := os.ReadFile("tests/fbx64.signed.efi")
			Expect(err).ToNot(HaveOccurred())
			Expect(fs.WriteFile("/efi/EFI/Kairos/"+UnassignedArtifactRole+".efi", signed, os.ModePerm)).To(Succeed())
		})

		It("installs the new artifact as active and fails removing the unassigned set", func() {
			// the signature check passes and the new artifact is installed as
			// active, but removing the unassigned artifacts goes through the
			// real OS paths and fails
			err := upgrader.Run()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("removing artifact set"))

			exists, _ := fsutils.Exists(fs, "/efi/EFI/Kairos/active.efi")
			Expect(exists).To(BeTrue())
		})

		It("fails rotating the current active artifact to passive", func() {
			// deleting the previous active artifact goes through the real OS
			// paths and fails
			Expect(fs.WriteFile("/efi/EFI/Kairos/active.efi", []byte("old active"), os.ModePerm)).To(Succeed())

			err := upgrader.Run()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("installing the new artifacts as active"))

			// the old active was still rotated to passive
			content, err := fs.ReadFile("/efi/EFI/Kairos/passive.efi")
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("old active"))
		})
	})

	It("fails a single entry upgrade when the target entry is not installed", func() {
		spec.Entry = "kairos-uki-test-nonexistent-entry"
		err := upgrader.Run()
		Expect(err).To(HaveOccurred())
	})

	It("fails a recovery upgrade when the recovery entry is not installed", func() {
		spec.Entry = constants.BootEntryRecovery
		Expect(spec.RecoveryUpgrade()).To(BeTrue())
		err := upgrader.Run()
		Expect(err).To(HaveOccurred())
	})
})
