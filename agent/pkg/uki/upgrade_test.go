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
	"os"
	"path/filepath"

	"github.com/foxboron/go-uefi/efi/attributes"
	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/internal/testartifacts"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"

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
		// The KAIROS_INIT_VERSION downgrade gate reads this on the
		// running system side of the compare; the target side is
		// stubbed by tests that get far enough to hit prepareFinalize.
		Expect(fsutils.MkdirAll(fs, "/etc", constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile(constants.KairosReleaseFile, []byte(`KAIROS_INIT_VERSION="v4.3.0"`+"\n"), 0o644)).To(Succeed())

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
			signer, err := testartifacts.NewKeyPair("uki upgrade test signer")
			Expect(err).ToNot(HaveOccurred())
			signed, err := testartifacts.SignPE(testartifacts.MinimalPE(testartifacts.PEOptions{}), signer)
			Expect(err).ToNot(HaveOccurred())
			db, err := testartifacts.CertDBVar(signer.Cert)
			Expect(err).ToNot(HaveOccurred())
			Expect(fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", testartifacts.SignatureDBVarName("db")), db, os.ModePerm)).To(Succeed())
			oldEfivars := attributes.Efivars
			rawEfivars, err := fs.(*vfst.TestFS).RawPath("/sys/firmware/efi/efivars")
			Expect(err).ToNot(HaveOccurred())
			attributes.Efivars = rawEfivars
			DeferCleanup(func() {
				attributes.Efivars = oldEfivars
			})

			// the signed artifact is already in place as the unassigned role
			Expect(fs.WriteFile("/efi/EFI/Kairos/"+UnassignedArtifactRole+".efi", signed, os.ModePerm)).To(Succeed())

			// Skip the signer-match check for these rotation-edge tests;
			// they intentionally corrupt / overwrite active.efi to trigger
			// specific rotation failures, so a real signer-match would
			// short-circuit before the test's actual failure path. The
			// signer-match rule itself has its own coverage.
			origSigner := requireSameSignerAsBootedFn
			requireSameSignerAsBootedFn = func(*sdkConfig.Config, string) error { return nil }
			DeferCleanup(func() { requireSameSignerAsBootedFn = origSigner })

			// The signed test .efi is not a real Kairos UKI, so its
			// .initrd cannot be walked by the production extractor.
			// Stub extractFromInitrd for the length of this Describe so
			// prepareFinalize returns a valid stage (target's
			// kairos-release satisfies the downgrade gate) and control
			// flows on to the rotation logic these tests are about.
			origExtract := extractFromInitrd
			extractFromInitrd = initrdWith(`KAIROS_INIT_VERSION="v4.3.0"` + "\n")
			DeferCleanup(func() { extractFromInitrd = origExtract })
		})

		It("installs the new artifact as active and removes the unassigned set", func() {
			signed, err := fs.ReadFile("/efi/EFI/Kairos/" + UnassignedArtifactRole + ".efi")
			Expect(err).ToNot(HaveOccurred())

			Expect(upgrader.Run()).To(Succeed())

			active, err := fs.ReadFile("/efi/EFI/Kairos/active.efi")
			Expect(err).ToNot(HaveOccurred())
			Expect(active).To(Equal(signed))

			exists, err := fsutils.Exists(fs, "/efi/EFI/Kairos/"+UnassignedArtifactRole+".efi")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("rotates the current active artifact to passive", func() {
			Expect(fs.WriteFile("/efi/EFI/Kairos/active.efi", []byte("old active"), os.ModePerm)).To(Succeed())
			signed, err := fs.ReadFile("/efi/EFI/Kairos/" + UnassignedArtifactRole + ".efi")
			Expect(err).ToNot(HaveOccurred())

			Expect(upgrader.Run()).To(Succeed())

			// the old active became passive and the new artifact is active
			content, err := fs.ReadFile("/efi/EFI/Kairos/passive.efi")
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("old active"))

			active, err := fs.ReadFile("/efi/EFI/Kairos/active.efi")
			Expect(err).ToNot(HaveOccurred())
			Expect(active).To(Equal(signed))
		})

		// kairos-io/kairos#4917 asks a refused upgrade to abort before the
		// first write, so the ESP is left exactly as it was found. Run pushes
		// removeArtifactSetWithRole onto its cleanup stack on each of the
		// pre-rotation refusals to keep that promise. These specs are what
		// holds it: they check the error and then that the dumped unassigned
		// set is gone and the two entries the machine can still boot are
		// byte-identical.
		Describe("a refusal before rotation", func() {
			BeforeEach(func() {
				Expect(fs.WriteFile("/efi/EFI/Kairos/active.efi", []byte("old active"), os.ModePerm)).To(Succeed())
				Expect(fs.WriteFile("/efi/EFI/Kairos/passive.efi", []byte("old passive"), os.ModePerm)).To(Succeed())
			})

			expectEspUnwound := func() {
				exists, err := fsutils.Exists(fs, "/efi/EFI/Kairos/"+UnassignedArtifactRole+".efi")
				Expect(err).ToNot(HaveOccurred())
				Expect(exists).To(BeFalse(), "the unassigned set must not be left on the ESP")

				active, err := fs.ReadFile("/efi/EFI/Kairos/active.efi")
				Expect(err).ToNot(HaveOccurred())
				Expect(string(active)).To(Equal("old active"))

				passive, err := fs.ReadFile("/efi/EFI/Kairos/passive.efi")
				Expect(err).ToNot(HaveOccurred())
				Expect(string(passive)).To(Equal("old passive"))
			}

			It("unwinds the unassigned set when the signer does not match the booted one", func() {
				requireSameSignerAsBootedFn = func(*sdkConfig.Config, string) error {
					return fmt.Errorf("signed by a different certificate")
				}

				err := upgrader.Run()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("signed by a different certificate"))
				expectEspUnwound()
			})

			It("unwinds the unassigned set when the target is a KAIROS_INIT_VERSION downgrade", func() {
				extractFromInitrd = initrdWith(`KAIROS_INIT_VERSION="v4.2.0"` + "\n")

				err := upgrader.Run()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("refusing to upgrade"))
				Expect(err.Error()).To(ContainSubstring("v4.2.0"))
				expectEspUnwound()
			})

			It("unwinds the unassigned set when the target carries no kairos-release", func() {
				extractFromInitrd = func(_ string, extractions map[string]string) ([]string, error) {
					found := []string{}
					for src, dst := range extractions {
						if src == constants.KairosReleaseFile {
							continue
						}
						if err := os.WriteFile(dst, nil, 0o644); err != nil {
							return found, err
						}
						found = append(found, src)
					}
					return found, nil
				}

				err := upgrader.Run()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("carries no " + constants.KairosReleaseFile))
				expectEspUnwound()
			})

			It("unwinds the unassigned set when the EFI partition can not hold both copies", func() {
				Expect(fs.Truncate("/efi/EFI/Kairos/active.efi", tooBigFor(fs, "/efi"))).To(Succeed())

				err := upgrader.Run()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not enough space on the EFI partition"))

				exists, err := fsutils.Exists(fs, "/efi/EFI/Kairos/"+UnassignedArtifactRole+".efi")
				Expect(err).ToNot(HaveOccurred())
				Expect(exists).To(BeFalse(), "the unassigned set must not be left on the ESP")

				passive, err := fs.ReadFile("/efi/EFI/Kairos/passive.efi")
				Expect(err).ToNot(HaveOccurred())
				Expect(string(passive)).To(Equal("old passive"))
			})
		})
	})

	Describe("installing a single entry", func() {
		// The dump into this directory goes through cfg.Fs, and the test
		// runner stands in for rsync without copying anything, so the specs
		// write what the dump would have produced straight into it. The path
		// is predictable because fsutils.TempDir skips the random suffix on a
		// vfst.TestFS.
		var dumpDir string

		BeforeEach(func() {
			dumpDir = filepath.Join(os.TempDir(), "kairos-uki-entry-")
			Expect(fsutils.MkdirAll(fs, filepath.Join(dumpDir, "EFI", "kairos"), constants.DirPerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Join(dumpDir, "loader", "entries"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(dumpDir, "EFI", "kairos", UnassignedArtifactRole+".efi"), []byte("new artifact"), 0o644)).To(Succeed())
			Expect(fs.WriteFile(
				filepath.Join(dumpDir, "loader", "entries", UnassignedArtifactRole+".conf"),
				[]byte("title Kairos\nefi /EFI/kairos/"+UnassignedArtifactRole+".efi\n"), 0o644)).To(Succeed())

			// The entry being upgraded has to already be installed on the ESP
			Expect(fsutils.MkdirAll(fs, "/efi/EFI/kairos", constants.DirPerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, "/efi/loader/entries", constants.DirPerm)).To(Succeed())
		})

		It("installs the artifact and the conf on the given filesystem", func() {
			Expect(fs.WriteFile("/efi/EFI/kairos/recovery.efi", []byte("old artifact"), 0o644)).To(Succeed())
			spec.Entry = constants.BootEntryRecovery
			Expect(spec.RecoveryUpgrade()).To(BeTrue())

			Expect(upgrader.Run()).To(Succeed())

			efi, err := fs.ReadFile("/efi/EFI/kairos/recovery.efi")
			Expect(err).ToNot(HaveOccurred())
			Expect(string(efi)).To(Equal("new artifact"))

			conf, err := fs.ReadFile("/efi/loader/entries/recovery.conf")
			Expect(err).ToNot(HaveOccurred())
			// installEntry rewrites the role in the efi key, installRecovery
			// then rewrites the title
			Expect(string(conf)).To(ContainSubstring("efi /EFI/kairos/recovery.efi"))
			Expect(string(conf)).ToNot(ContainSubstring(UnassignedArtifactRole))
			Expect(string(conf)).To(ContainSubstring("title Kairos recovery"))
		})

		It("removes the dump directory from the filesystem it created it on", func() {
			Expect(fs.WriteFile("/efi/EFI/kairos/recovery.efi", []byte("old artifact"), 0o644)).To(Succeed())
			spec.Entry = constants.BootEntryRecovery

			Expect(upgrader.Run()).To(Succeed())

			exists, err := fsutils.Exists(fs, dumpDir)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse(), "the dump directory should have been removed from the given filesystem")
		})

		It("fails a single entry upgrade when the target entry is not installed", func() {
			spec.Entry = "kairos-uki-test-nonexistent-entry"
			err := upgrader.Run()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not stat target efi file"))
			// Nothing was written for an entry that is not installed
			exists, err := fsutils.Exists(fs, "/efi/loader/entries/kairos-uki-test-nonexistent-entry.conf")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("fails a recovery upgrade when the recovery entry is not installed", func() {
			spec.Entry = constants.BootEntryRecovery
			Expect(spec.RecoveryUpgrade()).To(BeTrue())
			err := upgrader.Run()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not stat target efi file"))
		})
	})
})

// initrdWith stands in for ExtractFromInitrd. The signed test .efi is not a
// real Kairos UKI, so its .initrd cannot be walked by the production
// extractor; this writes the files prepareFinalize asks for straight into the
// host temp dir it chose, with release as the target's /etc/kairos-release.
func initrdWith(release string) func(string, map[string]string) ([]string, error) {
	return func(_ string, extractions map[string]string) ([]string, error) {
		found := []string{}
		for src, dst := range extractions {
			var body []byte
			if src == constants.KairosReleaseFile {
				body = []byte(release)
			}
			if err := os.WriteFile(dst, body, 0o644); err != nil {
				return found, err
			}
			found = append(found, src)
		}
		return found, nil
	}
}
