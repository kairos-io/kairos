/*
Copyright © 2021 SUSE LLC

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

package utils_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/utils"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Utils", Label("utils"), func() {
	var runner *v1mock.FakeRunner
	var logger sdkLogger.KairosLogger
	var fs vfs.FS
	var cleanup func()

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		logger = sdkLogger.NewNullLogger()
		// Ensure /tmp exists in the VFS
		fs, cleanup, _ = vfst.NewTestFS(nil)
		fs.Mkdir("/tmp", constants.DirPerm)
		fs.Mkdir("/run", constants.DirPerm)
		fs.Mkdir("/etc", constants.DirPerm)

	})
	AfterEach(func() { cleanup() })
	Describe("CopyFile", Label("CopyFile"), func() {
		It("Copies source file to target file", func() {
			err := utils.MkdirAll(fs, "/some", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create("/some/file")
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Stat("/some/otherfile")
			Expect(err).Should(HaveOccurred())
			Expect(utils.CopyFile(fs, "/some/file", "/some/otherfile")).ShouldNot(HaveOccurred())
			e, err := utils.Exists(fs, "/some/otherfile")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(e).To(BeTrue())
		})
		It("Copies source file to target folder", func() {
			err := utils.MkdirAll(fs, "/some", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			err = utils.MkdirAll(fs, "/someotherfolder", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create("/some/file")
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Stat("/someotherfolder/file")
			Expect(err).Should(HaveOccurred())
			Expect(utils.CopyFile(fs, "/some/file", "/someotherfolder")).ShouldNot(HaveOccurred())
			e, err := utils.Exists(fs, "/someotherfolder/file")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(e).To(BeTrue())
		})
		It("Fails to open non existing file", func() {
			err := utils.MkdirAll(fs, "/some", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(utils.CopyFile(fs, "/some/file", "/some/otherfile")).NotTo(BeNil())
			_, err = fs.Stat("/some/otherfile")
			Expect(err).NotTo(BeNil())
		})
		It("Fails to copy on non writable target", func() {
			err := utils.MkdirAll(fs, "/some", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			fs.Create("/some/file")
			_, err = fs.Stat("/some/otherfile")
			Expect(err).NotTo(BeNil())
			fs = vfs.NewReadOnlyFS(fs)
			Expect(utils.CopyFile(fs, "/some/file", "/some/otherfile")).NotTo(BeNil())
			_, err = fs.Stat("/some/otherfile")
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("CreateDirStructure", Label("CreateDirStructure"), func() {
		It("Creates essential directories", func() {
			dirList := []string{"sys", "proc", "dev", "tmp", "boot", "usr/local", "oem"}
			for _, dir := range dirList {
				_, err := fs.Stat(fmt.Sprintf("/my/root/%s", dir))
				Expect(err).NotTo(BeNil())
			}
			Expect(utils.CreateDirStructure(fs, "/my/root")).To(BeNil())
			for _, dir := range dirList {
				fi, err := fs.Stat(fmt.Sprintf("/my/root/%s", dir))
				Expect(err).To(BeNil())
				if fi.Name() == "tmp" {
					Expect(fmt.Sprintf("%04o", fi.Mode().Perm())).To(Equal("0777"))
					Expect(fi.Mode() & os.ModeSticky).NotTo(Equal(0))
				}
				if fi.Name() == "sys" {
					Expect(fmt.Sprintf("%04o", fi.Mode().Perm())).To(Equal("0555"))
				}
			}
		})
		It("Fails on non writable target", func() {
			fs = vfs.NewReadOnlyFS(fs)
			Expect(utils.CreateDirStructure(fs, "/my/root")).NotTo(BeNil())
		})
	})
	Describe("DirSize", Label("fs"), func() {
		BeforeEach(func() {
			err := utils.MkdirAll(fs, "/folder/subfolder", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			f, err := fs.Create("/folder/file")
			Expect(err).ShouldNot(HaveOccurred())
			err = f.Truncate(1024)
			Expect(err).ShouldNot(HaveOccurred())
			f, err = fs.Create("/folder/subfolder/file")
			Expect(err).ShouldNot(HaveOccurred())
			err = f.Truncate(2048)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Returns the expected size of a test folder", func() {
			size, err := utils.DirSize(fs, "/folder")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(size).To(Equal(int64(3072)))
		})
	})
	Describe("CalcFileChecksum", Label("checksum"), func() {
		It("compute correct sha256 checksum", func() {
			testData := strings.Repeat("abcdefghilmnopqrstuvz\n", 20)
			testDataSHA256 := "7f182529f6362ae9cfa952ab87342a7180db45d2c57b52b50a68b6130b15a422"

			err := fs.Mkdir("/iso", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			err = fs.WriteFile("/iso/test.iso", []byte(testData), 0644)
			Expect(err).ShouldNot(HaveOccurred())

			checksum, err := utils.CalcFileChecksum(fs, "/iso/test.iso")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(checksum).To(Equal(testDataSHA256))
		})
	})
	Describe("CreateSquashFS", Label("CreateSquashFS"), func() {
		It("runs with no options if none given", func() {
			err := utils.CreateSquashFS(runner, logger, "source", "dest", []string{})
			Expect(runner.IncludesCmds([][]string{
				{"mksquashfs", "source", "dest"},
			})).To(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})
		It("runs with options if given", func() {
			err := utils.CreateSquashFS(runner, logger, "source", "dest", constants.GetDefaultSquashfsOptions())
			cmd := []string{"mksquashfs", "source", "dest"}
			cmd = append(cmd, constants.GetDefaultSquashfsOptions()...)
			Expect(runner.IncludesCmds([][]string{
				cmd,
			})).To(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})
		It("returns an error if it fails", func() {
			runner.ReturnError = errors.New("error")
			err := utils.CreateSquashFS(runner, logger, "source", "dest", []string{})
			Expect(runner.IncludesCmds([][]string{
				{"mksquashfs", "source", "dest"},
			})).To(BeNil())
			Expect(err).To(HaveOccurred())
		})
	})
	Describe("GetUkiCmdline", Label("GetUkiCmdline"), func() {
		var defaultCmdline string
		BeforeEach(func() {
			defaultCmdline = constants.UkiCmdline
			// viper is process-global, so clear the keys these specs set.
			// Without this, how many entries a spec sees depends on which
			// specs ran before it.
			viper.Set("extend-cmdline", "")
			viper.Set("extra-cmdline", []string{})
			viper.Set("single-efi-cmdline", []string{})
		})

		It("returns the default cmdline", func() {
			entries := utils.GetUkiCmdline()
			Expect(entries[0].Cmdline).To(Equal(defaultCmdline))
		})

		// The installer copies norole.efi byte for byte into every role, so a
		// keyword put on the live media's cmdline is on the installed
		// machine's cmdline for its whole life. kairos-io/kairos#5000.
		It("puts no install keyword on any entry it builds", func() {
			viper.Set("extra-cmdline", []string{"key=value testkey"})
			viper.Set("single-efi-cmdline", []string{"My Entry: key=value"})
			entries := append(utils.GetUkiCmdline(), utils.GetUkiSingleCmdlines(sdkLogger.NewNullLogger())...)
			Expect(entries).ToNot(BeEmpty())
			for _, entry := range entries {
				for _, keyword := range []string{"install-mode", "install-mode-interactive", "interactive-install"} {
					Expect(strings.Fields(entry.Cmdline)).ToNot(
						ContainElement(keyword),
						"entry %q must not carry %q", entry.FileName, keyword)
				}
			}
		})

		It("returns the default cmdline with the cmdline flag", func() {
			viper.Set("extra-cmdline", []string{"key=value testkey"})
			entries := utils.GetUkiCmdline()
			cmdlines := []string{}
			for _, entry := range entries {
				cmdlines = append(cmdlines, entry.Cmdline)
			}
			Expect(cmdlines).To(ContainElements(defaultCmdline))
			Expect(cmdlines).To(ContainElements(defaultCmdline + " key=value testkey"))
		})

		It("returns more than one cmdline with the cmdline flag if specified multiple values", func() {
			viper.Set("extra-cmdline", []string{"key=value testkey", "another=value anotherkey"})
			entries := utils.GetUkiCmdline()
			cmdlines := []string{}
			for _, entry := range entries {
				cmdlines = append(cmdlines, entry.Cmdline)
			}

			// Should contain the default one
			Expect(cmdlines).To(ContainElements(defaultCmdline))
			// Also the extra ones
			Expect(cmdlines).To(ContainElements(defaultCmdline + " key=value testkey"))
			Expect(cmdlines).To(ContainElements(defaultCmdline + " another=value anotherkey"))
		})

		// An entry whose whole addition is "install-mode" used to be renamed to
		// the bare basename, which is the default entry's name. Two entries
		// with one name overwrite each other's .efi and .conf.
		It("gives every entry its own file name, install keyword or not", func() {
			viper.Set("extra-cmdline", []string{"install-mode", "install-mode-interactive"})
			entries := utils.GetUkiCmdline()
			Expect(entries).To(HaveLen(3))

			names := []string{}
			for _, entry := range entries {
				names = append(names, entry.FileName)
			}
			// ConsistOf compares as a multiset, so a name claimed twice fails
			// here rather than silently overwriting an .efi on disk.
			Expect(names).To(ConsistOf(
				constants.ArtifactBaseName,
				constants.ArtifactBaseName+"_install-mode",
				constants.ArtifactBaseName+"_install-mode-interactive",
			))
		})

		It("expands the default cmdline if extended-cmdline is used", func() {
			viper.Set("extend-cmdline", "key=value testkey")
			entries := utils.GetUkiCmdline()
			for _, entry := range entries {
				Expect(entry.Cmdline).To(MatchRegexp(".*key=value testkey"))
			}
		})
	})

	Describe("GetUkiSingleCmdlines", Label("GetUkiSingleCmdlines"), func() {
		var defaultCmdline string
		BeforeEach(func() {
			defaultCmdline = constants.UkiCmdline
		})

		It("returns the specified entry", func() {
			viper.Set("single-efi-cmdline", []string{"My Entry: key=value"})
			viper.Set("boot-branding", "Kairos")

			entries := utils.GetUkiSingleCmdlines(sdkLogger.NewNullLogger())
			Expect(entries[0].Cmdline).To(MatchRegexp(defaultCmdline + "  key=value"))
			Expect(entries[0].Title).To(ContainSubstring("Kairos (My Entry)"))
			Expect(entries[0].FileName).To(Equal("My_Entry"))
		})
	})

	Describe("NameFromRootfs", Label("NameFromRootfs"), func() {
		var fs vfs.FS
		var cleanup func()

		BeforeEach(func() {
			fs, cleanup, _ = vfst.NewTestFS(nil)
			fs.Mkdir("/etc", constants.DirPerm)
		})

		AfterEach(func() {
			cleanup()
		})

		It("correctly formats name for standard variant with k8s", func() {
			// Create a temporary directory for our test
			tmpDir, err := os.MkdirTemp("", "kairos-test-*")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			// Create the etc directory
			err = os.MkdirAll(filepath.Join(tmpDir, "etc"), 0755)
			Expect(err).ToNot(HaveOccurred())

			// Create a test kairos-release file with k8s information
			kairosRelease := `KAIROS_ARCH="amd64"
KAIROS_BUG_REPORT_URL="https://github.com/kairos-io/kairos/issues"
KAIROS_FAMILY="debian"
KAIROS_FIPS="false"
KAIROS_FLAVOR="ubuntu"
KAIROS_FLAVOR_RELEASE="24.04"
KAIROS_FRAMEWORK_VERSION="v2.22.0"
KAIROS_HOME_URL="https://github.com/kairos-io/kairos"
KAIROS_ID="kairos"
KAIROS_ID_LIKE="kairos-standard-ubuntu-24.04"
KAIROS_IMAGE_LABEL="24.04-standard-amd64-generic-v3.4.2"
KAIROS_MODEL="generic"
KAIROS_NAME="kairos-standard-ubuntu-24.04"
KAIROS_REGISTRY_AND_ORG="quay.io/kairos"
KAIROS_RELEASE="v3.4.2"
KAIROS_SOFTWARE_VERSION="v1.32.4+k3s1"
KAIROS_SOFTWARE_VERSION_PREFIX="k3s"
KAIROS_TARGETARCH="amd64"
KAIROS_VARIANT="standard"
KAIROS_VERSION="v3.4.2"`

			err = os.WriteFile(filepath.Join(tmpDir, "etc/kairos-release"), []byte(kairosRelease), 0644)
			Expect(err).ToNot(HaveOccurred())

			name := utils.NameFromRootfs(tmpDir)

			Expect(name).To(Equal("ubuntu-24.04-standard-amd64-generic-v3.4.2-k3sv1.32.4+k3s1"))
		})

		It("correctly formats name for core variant without k8s", func() {
			// Create a temporary directory for our test
			tmpDir, err := os.MkdirTemp("", "kairos-test-*")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			// Create the etc directory
			err = os.MkdirAll(filepath.Join(tmpDir, "etc"), 0755)
			Expect(err).ToNot(HaveOccurred())

			// Create a test kairos-release file without k8s information
			kairosRelease := `KAIROS_ARCH="amd64"
KAIROS_BUG_REPORT_URL="https://github.com/kairos-io/kairos/issues"
KAIROS_FAMILY="debian"
KAIROS_FIPS="false"
KAIROS_FLAVOR="ubuntu"
KAIROS_FLAVOR_RELEASE="24.04"
KAIROS_FRAMEWORK_VERSION="v2.22.0"
KAIROS_HOME_URL="https://github.com/kairos-io/kairos"
KAIROS_ID="kairos"
KAIROS_ID_LIKE="kairos-core-ubuntu-24.04"
KAIROS_IMAGE_LABEL="24.04-core-amd64-generic-v3.4.2"
KAIROS_MODEL="generic"
KAIROS_NAME="kairos-core-ubuntu-24.04"
KAIROS_REGISTRY_AND_ORG="quay.io/kairos"
KAIROS_RELEASE="v3.4.2"
KAIROS_TARGETARCH="amd64"
KAIROS_VARIANT="core"
KAIROS_VERSION="v3.4.2"`

			err = os.WriteFile(filepath.Join(tmpDir, "etc/kairos-release"), []byte(kairosRelease), 0644)
			Expect(err).ToNot(HaveOccurred())

			name := utils.NameFromRootfs(tmpDir)
			Expect(name).To(Equal("ubuntu-24.04-core-amd64-generic-v3.4.2"))
		})

		It("falls back to TARGETARCH if ARCH is missing", func() {
			tmpDir, err := os.MkdirTemp("", "kairos-test-*")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			err = os.MkdirAll(filepath.Join(tmpDir, "etc"), 0755)
			Expect(err).ToNot(HaveOccurred())

			kairosRelease := `KAIROS_FLAVOR="ubuntu"
KAIROS_FLAVOR_RELEASE="24.04"
KAIROS_VARIANT="core"
KAIROS_MODEL="generic"
KAIROS_VERSION="v3.4.2"
KAIROS_TARGETARCH="amd64"`
			err = os.WriteFile(filepath.Join(tmpDir, "etc/kairos-release"), []byte(kairosRelease), 0644)
			Expect(err).ToNot(HaveOccurred())

			name := utils.NameFromRootfs(tmpDir)
			// ARCH is missing, should use TARGETARCH
			Expect(name).To(ContainSubstring("amd64"))
		})

		It("falls back to non-k8s name if k8s provider is missing", func() {
			tmpDir, err := os.MkdirTemp("", "kairos-test-*")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			err = os.MkdirAll(filepath.Join(tmpDir, "etc"), 0755)
			Expect(err).ToNot(HaveOccurred())

			kairosRelease := `KAIROS_FLAVOR="ubuntu"
KAIROS_FLAVOR_RELEASE="24.04"
KAIROS_VARIANT="standard"
KAIROS_MODEL="generic"
KAIROS_VERSION="v3.4.2"
KAIROS_ARCH="amd64"`
			err = os.WriteFile(filepath.Join(tmpDir, "etc/kairos-release"), []byte(kairosRelease), 0644)
			Expect(err).ToNot(HaveOccurred())

			name := utils.NameFromRootfs(tmpDir)
			// Should fall back to non-k8s name
			Expect(name).To(Equal("ubuntu-24.04-standard-amd64-generic-v3.4.2"))
		})

		It("falls back to non-k8s name if k8s version is missing", func() {
			tmpDir, err := os.MkdirTemp("", "kairos-test-*")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			err = os.MkdirAll(filepath.Join(tmpDir, "etc"), 0755)
			Expect(err).ToNot(HaveOccurred())

			kairosRelease := `KAIROS_FLAVOR="ubuntu"
KAIROS_FLAVOR_RELEASE="24.04"
KAIROS_VARIANT="standard"
KAIROS_MODEL="generic"
KAIROS_VERSION="v3.4.2"
KAIROS_ARCH="amd64"
KAIROS_SOFTWARE_VERSION_PREFIX="k3s"`
			err = os.WriteFile(filepath.Join(tmpDir, "etc/kairos-release"), []byte(kairosRelease), 0644)
			Expect(err).ToNot(HaveOccurred())

			name := utils.NameFromRootfs(tmpDir)
			// Should fall back to non-k8s name
			Expect(name).To(Equal("ubuntu-24.04-standard-amd64-generic-v3.4.2"))
		})

		It("falls back to os-release if kairos-release does not exist", func() {
			tmpDir, err := os.MkdirTemp("", "kairos-test-*")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			err = os.MkdirAll(filepath.Join(tmpDir, "etc"), 0755)
			Expect(err).ToNot(HaveOccurred())

			osRelease := `FLAVOR=ubuntu
IMAGE_LABEL=custom-label`
			err = os.WriteFile(filepath.Join(tmpDir, "etc/os-release"), []byte(osRelease), 0644)
			Expect(err).ToNot(HaveOccurred())

			name := utils.NameFromRootfs(tmpDir)
			Expect(name).To(Equal("ubuntu-custom-label"))
		})

		It("handles missing fields gracefully", func() {
			tmpDir, err := os.MkdirTemp("", "kairos-test-*")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			err = os.MkdirAll(filepath.Join(tmpDir, "etc"), 0755)
			Expect(err).ToNot(HaveOccurred())

			kairosRelease := `KAIROS_FLAVOR="ubuntu"`
			err = os.WriteFile(filepath.Join(tmpDir, "etc/kairos-release"), []byte(kairosRelease), 0644)
			Expect(err).ToNot(HaveOccurred())

			name := utils.NameFromRootfs(tmpDir)
			// Should not panic, may return incomplete name
			Expect(name).To(ContainSubstring("ubuntu"))
		})
	})

	Describe("GolangArchToArch", Label("arch"), func() {
		// This function is a copy of the SDK's platform.golangArchToArch.
		// The two have to answer alike, because WithArch calls both: it
		// normalizes through this one and then builds a Platform through
		// the SDK one, so a value only one of them accepts fails a build
		// that the other would have run.
		DescribeTable("maps an architecture name the way the SDK does",
			func(in, want string) {
				got, err := utils.GolangArchToArch(in)
				Expect(err).ToNot(HaveOccurred())
				Expect(got).To(Equal(want))

				p, err := platform.NewPlatformFromArch(in)
				Expect(err).ToNot(HaveOccurred(), "the SDK rejects %q", in)
				Expect(p.Arch).To(Equal(want), "the SDK disagrees on %q", in)
			},
			Entry("amd64", "amd64", constants.Archx86),
			// x86_64 is what GetArchFromRootfs reads out of KAIROS_ARCH
			// and what the arch: key of a config file is likely to say.
			Entry("x86_64", "x86_64", constants.Archx86),
			Entry("arm64", "arm64", constants.ArchArm64),
			Entry("riscv64", "riscv64", constants.ArchRiscv64),
			Entry("uppercase", "X86_64", constants.Archx86),
		)

		It("rejects an architecture it does not know", func() {
			_, err := utils.GolangArchToArch("sparc")
			Expect(err).To(HaveOccurred())
		})
	})
})
