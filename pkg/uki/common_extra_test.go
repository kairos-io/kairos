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
	"encoding/binary"
	"os"
	"path/filepath"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkutils "github.com/kairos-io/kairos-sdk/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
)

// patchPEMajorImageVersion copies the PE binary at src to dst while setting
// the MajorImageVersion field of the PE64 optional header to version.
func patchPEMajorImageVersion(src, dst string, version uint16) {
	data, err := os.ReadFile(src)
	Expect(err).ToNot(HaveOccurred())
	// e_lfanew lives at offset 0x3C and points to the PE signature
	peOff := binary.LittleEndian.Uint32(data[0x3C:])
	// PE signature (4 bytes) + COFF file header (20 bytes) = optional header start
	optOff := peOff + 4 + 20
	// MajorImageVersion is at offset 44 inside the PE32+ optional header
	binary.LittleEndian.PutUint16(data[optOff+44:], version)
	Expect(os.WriteFile(dst, data, 0644)).To(Succeed())
}

var _ = Describe("Common helpers", func() {
	var fs vfs.FS
	var dir string
	var memLog *bytes.Buffer
	var logger sdkLogger.KairosLogger

	BeforeEach(func() {
		// Several helpers in common.go mix the KairosFS abstraction with
		// direct os.* calls, so tests use the real OS filesystem rooted in a
		// temporary directory.
		fs = vfs.OSFS
		dir = GinkgoT().TempDir()
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
	})

	Describe("copyArtifact", func() {
		It("copies the file replacing the role in the base name only", func() {
			src := filepath.Join(dir, "active.efi")
			Expect(os.WriteFile(src, []byte("data"), 0644)).To(Succeed())

			newName, err := copyArtifact(fs, src, "active", "passive")
			Expect(err).ToNot(HaveOccurred())
			Expect(newName).To(Equal(filepath.Join(dir, "passive.efi")))
			content, err := os.ReadFile(newName)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("data"))
		})

		It("fails when the source does not exist", func() {
			_, err := copyArtifact(fs, filepath.Join(dir, "missing.efi"), "missing", "newrole")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("removeArtifactSetWithRole", func() {
		It("removes only files prefixed with the role", func() {
			Expect(os.WriteFile(filepath.Join(dir, "active.efi"), []byte("a"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "active.conf"), []byte("a"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "passive.efi"), []byte("p"), 0644)).To(Succeed())

			Expect(removeArtifactSetWithRole(fs, dir, "active")).To(Succeed())

			Expect(filepath.Join(dir, "active.efi")).ToNot(BeAnExistingFile())
			Expect(filepath.Join(dir, "active.conf")).ToNot(BeAnExistingFile())
			Expect(filepath.Join(dir, "passive.efi")).To(BeAnExistingFile())
		})
	})

	Describe("copyArtifactSetRole", func() {
		It("copies efi and conf artifacts replacing role, conf key and title", func() {
			Expect(os.WriteFile(filepath.Join(dir, "norole.efi"), []byte("efi"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "norole.conf"),
				[]byte("title Kairos\nefi /EFI/kairos/norole.efi\n"), 0644)).To(Succeed())

			Expect(copyArtifactSetRole(fs, dir, "norole", "active", logger)).To(Succeed())

			Expect(filepath.Join(dir, "active.efi")).To(BeAnExistingFile())
			conf, err := sdkutils.SystemdBootConfReader(filepath.Join(dir, "active.conf"))
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["efi"]).To(Equal("/EFI/kairos/active.efi"))
			expectedTitle, err := cnst.BootTitleForRole("active", "Kairos")
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["title"]).To(Equal(expectedTitle))
		})

		It("falls back to the uki key when the efi key is missing", func() {
			Expect(os.WriteFile(filepath.Join(dir, "norole.conf"),
				[]byte("title Kairos\nuki /EFI/kairos/norole.efi\n"), 0644)).To(Succeed())

			Expect(copyArtifactSetRole(fs, dir, "norole", "passive", logger)).To(Succeed())

			conf, err := sdkutils.SystemdBootConfReader(filepath.Join(dir, "passive.conf"))
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["uki"]).To(Equal("/EFI/kairos/passive.efi"))
			expectedTitle, err := cnst.BootTitleForRole("passive", "Kairos")
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["title"]).To(Equal(expectedTitle))
		})

		It("fails when the conf has neither efi nor uki keys", func() {
			Expect(os.WriteFile(filepath.Join(dir, "norole.conf"),
				[]byte("title Kairos\nfoo bar\n"), 0644)).To(Succeed())

			err := copyArtifactSetRole(fs, dir, "norole", "active", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no uki entry"))
		})

		It("fails when the conf has no title", func() {
			Expect(os.WriteFile(filepath.Join(dir, "norole.conf"),
				[]byte("efi /EFI/kairos/norole.efi\n"), 0644)).To(Succeed())

			err := copyArtifactSetRole(fs, dir, "norole", "active", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no title"))
		})

		It("fails when the artifact dir does not exist", func() {
			Expect(copyArtifactSetRole(fs, filepath.Join(dir, "missing"), "norole", "active", logger)).ToNot(Succeed())
		})

		It("fails when an artifact can not be read", func() {
			if os.Geteuid() == 0 {
				Skip("file permissions are not enforced for root")
			}
			Expect(os.WriteFile(filepath.Join(dir, "norole.efi"), []byte("efi"), 0644)).To(Succeed())
			Expect(os.Chmod(filepath.Join(dir, "norole.efi"), 0000)).To(Succeed())

			err := copyArtifactSetRole(fs, dir, "norole", "active", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("copying artifact"))
		})

		It("fails when the new role is not a valid boot title role", func() {
			Expect(os.WriteFile(filepath.Join(dir, "norole.conf"),
				[]byte("title Kairos\nefi /EFI/kairos/norole.efi\n"), 0644)).To(Succeed())

			err := copyArtifactSetRole(fs, dir, "norole", "badrole", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid role"))
		})
	})

	Describe("overwriteArtifactSetRole", func() {
		It("replaces the new role artifacts with the old role ones", func() {
			Expect(os.WriteFile(filepath.Join(dir, "active.efi"), []byte("new"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "passive.efi"), []byte("old"), 0644)).To(Succeed())

			Expect(overwriteArtifactSetRole(fs, dir, "active", "passive", logger)).To(Succeed())

			content, err := os.ReadFile(filepath.Join(dir, "passive.efi"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("new"))
			Expect(filepath.Join(dir, "active.efi")).To(BeAnExistingFile())
		})

		It("wraps errors from deleting the new role artifacts", func() {
			if os.Geteuid() == 0 {
				Skip("file permissions are not enforced for root")
			}
			Expect(os.WriteFile(filepath.Join(dir, "passive.efi"), []byte("old"), 0644)).To(Succeed())
			Expect(os.Chmod(dir, 0555)).To(Succeed())
			DeferCleanup(func() {
				Expect(os.Chmod(dir, 0755)).To(Succeed())
			})

			err := overwriteArtifactSetRole(fs, dir, "active", "passive", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("deleting role"))
		})

		It("wraps errors from copying the artifact set", func() {
			// conf without title makes the copy fail
			Expect(os.WriteFile(filepath.Join(dir, "active.conf"),
				[]byte("efi /EFI/kairos/active.efi\n"), 0644)).To(Succeed())

			err := overwriteArtifactSetRole(fs, dir, "active", "passive", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("copying artifact set role"))
		})
	})

	Describe("replaceRoleInKey", func() {
		It("fails when the file does not exist", func() {
			err := replaceRoleInKey(filepath.Join(dir, "missing.conf"), "efi", "a", "b", logger)
			Expect(err).To(HaveOccurred())
		})

		It("fails when the key is missing", func() {
			path := filepath.Join(dir, "test.conf")
			Expect(os.WriteFile(path, []byte("title Kairos\n"), 0644)).To(Succeed())

			err := replaceRoleInKey(path, "efi", "a", "b", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no efi entry"))
		})

		It("replaces the role in the given key", func() {
			path := filepath.Join(dir, "test.conf")
			Expect(os.WriteFile(path, []byte("efi /EFI/kairos/active.efi\n"), 0644)).To(Succeed())

			Expect(replaceRoleInKey(path, "efi", "active", "passive", logger)).To(Succeed())

			conf, err := sdkutils.SystemdBootConfReader(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["efi"]).To(Equal("/EFI/kairos/passive.efi"))
		})
	})

	Describe("replaceConfTitle", func() {
		It("fails when the file does not exist", func() {
			Expect(replaceConfTitle(filepath.Join(dir, "missing.conf"), "active")).ToNot(Succeed())
		})

		It("fails when there is no title", func() {
			path := filepath.Join(dir, "test.conf")
			Expect(os.WriteFile(path, []byte("efi /EFI/kairos/active.efi\n"), 0644)).To(Succeed())

			err := replaceConfTitle(path, "active")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no title"))
		})

		It("fails when the role is invalid", func() {
			path := filepath.Join(dir, "test.conf")
			Expect(os.WriteFile(path, []byte("title Kairos\n"), 0644)).To(Succeed())

			err := replaceConfTitle(path, "wrongrole")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid role"))
		})

		It("replaces the title for the given role", func() {
			path := filepath.Join(dir, "test.conf")
			Expect(os.WriteFile(path, []byte("title Kairos\n"), 0644)).To(Succeed())

			Expect(replaceConfTitle(path, "recovery")).To(Succeed())

			conf, err := sdkutils.SystemdBootConfReader(path)
			Expect(err).ToNot(HaveOccurred())
			expectedTitle, err := cnst.BootTitleForRole("recovery", "Kairos")
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["title"]).To(Equal(expectedTitle))
		})
	})

	Describe("copyFile", func() {
		It("copies the file contents", func() {
			src := filepath.Join(dir, "src")
			dst := filepath.Join(dir, "dst")
			Expect(os.WriteFile(src, []byte("payload"), 0644)).To(Succeed())

			Expect(copyFile(src, dst)).To(Succeed())

			content, err := os.ReadFile(dst)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("payload"))
		})

		It("panics when the source is missing", func() {
			Expect(func() {
				_ = copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"))
			}).To(Panic())
		})

		It("panics when the destination dir is missing", func() {
			src := filepath.Join(dir, "src")
			Expect(os.WriteFile(src, []byte("payload"), 0644)).To(Succeed())
			Expect(func() {
				_ = copyFile(src, filepath.Join(dir, "nonexistent", "dst"))
			}).To(Panic())
		})
	})

	Describe("AddSystemdConfSortKey", func() {
		It("adds the proper sort key to each conf file", func() {
			files := map[string]string{
				"active.conf":     "0001",
				"passive.conf":    "0002",
				"recovery.conf":   "0003",
				"statereset.conf": "0004",
				"other.conf":      "0010",
			}
			for name := range files {
				Expect(os.WriteFile(filepath.Join(dir, name),
					[]byte("title Kairos\nefi /EFI/kairos/foo.efi\n"), 0644)).To(Succeed())
			}
			Expect(os.WriteFile(filepath.Join(dir, "loader.conf"),
				[]byte("timeout 5\n"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "active.efi"), []byte("efi"), 0644)).To(Succeed())

			Expect(AddSystemdConfSortKey(fs, dir, logger)).To(Succeed())

			for name, sortKey := range files {
				conf, err := sdkutils.SystemdBootConfReader(filepath.Join(dir, name))
				Expect(err).ToNot(HaveOccurred())
				Expect(conf["sort-key"]).To(Equal(sortKey), name)
			}
			// loader.conf is left untouched
			conf, err := sdkutils.SystemdBootConfReader(filepath.Join(dir, "loader.conf"))
			Expect(err).ToNot(HaveOccurred())
			Expect(conf).ToNot(HaveKey("sort-key"))
		})

		It("fails when the dir does not exist", func() {
			Expect(AddSystemdConfSortKey(fs, filepath.Join(dir, "missing"), logger)).ToNot(Succeed())
		})

		It("panics on unreadable conf files", func() {
			// when the conf reader fails the error is only logged and the
			// returned nil map is written to, which panics
			if os.Geteuid() == 0 {
				Skip("file permissions are not enforced for root")
			}
			path := filepath.Join(dir, "active.conf")
			Expect(os.WriteFile(path, []byte("title Kairos\n"), 0644)).To(Succeed())
			Expect(os.Chmod(path, 0000)).To(Succeed())

			Expect(func() {
				_ = AddSystemdConfSortKey(fs, dir, logger)
			}).To(Panic())
		})
	})

	Describe("removeDefaultKeysFromLoaderConf", func() {
		var loaderConf string

		BeforeEach(func() {
			Expect(os.MkdirAll(filepath.Join(dir, "loader"), 0755)).To(Succeed())
			loaderConf = filepath.Join(dir, "loader/loader.conf")
		})

		It("fails when loader.conf is missing", func() {
			Expect(removeDefaultKeysFromLoaderConf(fs, dir, logger)).ToNot(Succeed())
		})

		It("removes the default key when it points to active", func() {
			Expect(os.WriteFile(loaderConf, []byte("default active_install\ntimeout 5\n"), 0644)).To(Succeed())

			Expect(removeDefaultKeysFromLoaderConf(fs, dir, logger)).To(Succeed())

			conf, err := utils.SystemdBootConfReader(fs, loaderConf)
			Expect(err).ToNot(HaveOccurred())
			Expect(conf).ToNot(HaveKey("default"))
			Expect(conf).To(HaveKey("timeout"))
		})

		It("keeps the default key when it does not point to active", func() {
			Expect(os.WriteFile(loaderConf, []byte("default recovery\n"), 0644)).To(Succeed())

			Expect(removeDefaultKeysFromLoaderConf(fs, dir, logger)).To(Succeed())

			conf, err := utils.SystemdBootConfReader(fs, loaderConf)
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["default"]).To(Equal("recovery"))
		})

		It("fails when loader.conf can not be written", func() {
			if os.Geteuid() == 0 {
				Skip("file permissions are not enforced for root")
			}
			Expect(os.WriteFile(loaderConf, []byte("default active_install\n"), 0644)).To(Succeed())
			Expect(os.Chmod(loaderConf, 0444)).To(Succeed())

			Expect(removeDefaultKeysFromLoaderConf(fs, dir, logger)).ToNot(Succeed())
		})

		It("does nothing when there is no default key", func() {
			Expect(os.WriteFile(loaderConf, []byte("timeout 5\n"), 0644)).To(Succeed())

			Expect(removeDefaultKeysFromLoaderConf(fs, dir, logger)).To(Succeed())

			conf, err := utils.SystemdBootConfReader(fs, loaderConf)
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["timeout"]).To(Equal("5"))
		})
	})

	Describe("upgradeEfiKeysInLoaderEntries", func() {
		var entriesDir string
		var fixture string

		BeforeEach(func() {
			entriesDir = filepath.Join(dir, "loader/entries")
			Expect(os.MkdirAll(filepath.Join(dir, "EFI/BOOT"), 0755)).To(Succeed())
			Expect(os.MkdirAll(entriesDir, 0755)).To(Succeed())
			cwd, err := os.Getwd()
			Expect(err).ToNot(HaveOccurred())
			fixture = filepath.Join(cwd, "tests/fbx64.efi")
		})

		It("skips the upgrade when systemd-boot can not be read", func() {
			Expect(upgradeEfiKeysInLoaderEntries("amd64", fs, dir, logger)).To(Succeed())
		})

		It("does not touch entries when systemd-boot is older than 259", func() {
			// fixture has MajorImageVersion 0
			data, err := os.ReadFile(fixture)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(dir, "EFI/BOOT/BOOTX64.EFI"), data, 0644)).To(Succeed())
			entry := filepath.Join(entriesDir, "active.conf")
			Expect(os.WriteFile(entry, []byte("title Kairos\nefi /EFI/kairos/active.efi\n"), 0644)).To(Succeed())

			Expect(upgradeEfiKeysInLoaderEntries("amd64", fs, dir, logger)).To(Succeed())

			conf, err := utils.SystemdBootConfReader(fs, entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["efi"]).To(Equal("/EFI/kairos/active.efi"))
			Expect(conf).ToNot(HaveKey("uki"))
		})

		It("upgrades efi keys to uki keys when systemd-boot is >= 259", func() {
			patchPEMajorImageVersion(fixture, filepath.Join(dir, "EFI/BOOT/BOOTX64.EFI"), 259)
			withEfi := filepath.Join(entriesDir, "active.conf")
			Expect(os.WriteFile(withEfi, []byte("title Kairos\nefi /EFI/kairos/active.efi\n"), 0644)).To(Succeed())
			withoutEfi := filepath.Join(entriesDir, "passive.conf")
			Expect(os.WriteFile(withoutEfi, []byte("title Kairos\nuki /EFI/kairos/passive.efi\n"), 0644)).To(Succeed())
			notAConf := filepath.Join(entriesDir, "readme.txt")
			Expect(os.WriteFile(notAConf, []byte("hello"), 0644)).To(Succeed())

			Expect(upgradeEfiKeysInLoaderEntries("amd64", fs, dir, logger)).To(Succeed())

			conf, err := utils.SystemdBootConfReader(fs, withEfi)
			Expect(err).ToNot(HaveOccurred())
			Expect(conf).ToNot(HaveKey("efi"))
			Expect(conf["uki"]).To(Equal("/EFI/kairos/active.efi"))

			conf, err = utils.SystemdBootConfReader(fs, withoutEfi)
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["uki"]).To(Equal("/EFI/kairos/passive.efi"))

			content, err := os.ReadFile(notAConf)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("hello"))
		})

		It("uses the arm64 systemd-boot binary name", func() {
			patchPEMajorImageVersion(fixture, filepath.Join(dir, "EFI/BOOT/BOOTAA64.EFI"), 260)
			entry := filepath.Join(entriesDir, "active.conf")
			Expect(os.WriteFile(entry, []byte("title Kairos\nefi /EFI/kairos/active.efi\n"), 0644)).To(Succeed())

			Expect(upgradeEfiKeysInLoaderEntries("arm64", fs, dir, logger)).To(Succeed())

			conf, err := utils.SystemdBootConfReader(fs, entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(conf["uki"]).To(Equal("/EFI/kairos/active.efi"))
		})

		It("fails when an entry can not be read", func() {
			if os.Geteuid() == 0 {
				Skip("file permissions are not enforced for root")
			}
			patchPEMajorImageVersion(fixture, filepath.Join(dir, "EFI/BOOT/BOOTX64.EFI"), 259)
			entry := filepath.Join(entriesDir, "active.conf")
			Expect(os.WriteFile(entry, []byte("title Kairos\nefi /EFI/kairos/active.efi\n"), 0644)).To(Succeed())
			Expect(os.Chmod(entry, 0000)).To(Succeed())

			Expect(upgradeEfiKeysInLoaderEntries("amd64", fs, dir, logger)).ToNot(Succeed())
		})

		It("fails when an entry can not be written", func() {
			if os.Geteuid() == 0 {
				Skip("file permissions are not enforced for root")
			}
			patchPEMajorImageVersion(fixture, filepath.Join(dir, "EFI/BOOT/BOOTX64.EFI"), 259)
			entry := filepath.Join(entriesDir, "active.conf")
			Expect(os.WriteFile(entry, []byte("title Kairos\nefi /EFI/kairos/active.efi\n"), 0644)).To(Succeed())
			Expect(os.Chmod(entry, 0444)).To(Succeed())

			Expect(upgradeEfiKeysInLoaderEntries("amd64", fs, dir, logger)).ToNot(Succeed())
		})

		It("fails when the entries dir is missing and systemd-boot is >= 259", func() {
			Expect(os.RemoveAll(entriesDir)).To(Succeed())
			patchPEMajorImageVersion(fixture, filepath.Join(dir, "EFI/BOOT/BOOTX64.EFI"), 259)

			Expect(upgradeEfiKeysInLoaderEntries("amd64", fs, dir, logger)).ToNot(Succeed())
		})
	})
})
