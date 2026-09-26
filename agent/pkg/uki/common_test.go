package uki

import (
	"bytes"
	"os"
	"path/filepath"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkutils "github.com/kairos-io/kairos/v4/sdk/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Common functions tests", func() {
	Describe("copyArtifactSetRole", func() {
		var fs vfs.FS
		var err error
		var memLog *bytes.Buffer
		var logger sdkLogger.KairosLogger

		BeforeEach(func() {
			fs, _, err = vfst.NewTestFS(map[string]interface{}{})
			Expect(err).ToNot(HaveOccurred())

			logger = sdkLogger.NewBufferLogger(memLog)
			logger.SetLevel("debug")

			Expect(fsutils.MkdirAll(fs, "/active", cnst.DirPerm)).ToNot(HaveOccurred())
			Expect(fsutils.MkdirAll(fs, "/other", cnst.DirPerm)).ToNot(HaveOccurred())

			f, err := fs.Create("/other/active.efi")
			Expect(err).ToNot(HaveOccurred())

			_, err = os.Stat(f.Name())
			Expect(err).ToNot(HaveOccurred())

			f, err = fs.Create("/other/other.efi")
			Expect(err).ToNot(HaveOccurred())

			_, err = os.Stat(f.Name())
			Expect(err).ToNot(HaveOccurred())
		})

		It("skips directories", func() {
			err = copyArtifactSetRole(fs, "/", "active", "passive", logger)
			Expect(err).ToNot(HaveOccurred())
		})

		It("replaces only the base file name", func() {
			err = copyArtifactSetRole(fs, "/other", "other", "newother", logger)
			Expect(err).ToNot(HaveOccurred())

			glob, _ := fs.Glob("/other/*")
			Expect(glob).To(HaveExactElements([]string{
				"/other/active.efi",
				"/other/newother.efi",
				"/other/other.efi",
			}))
		})
	})
})

var _ = Describe("copyArtifactSetRole cmdline", func() {
	// The live entry is the artifact set the installer rolls into every role,
	// so whatever cmdline it carries is the cmdline the installed system boots
	// with. Drive the real function over a real directory: replaceRoleInKey
	// reads and writes the .conf through os, not through the vfs.
	var dir string
	var logger sdkLogger.KairosLogger

	entryFor := func(role string) map[string]string {
		conf, err := sdkutils.SystemdBootConfReader(filepath.Join(dir, "loader/entries", role+".conf"))
		Expect(err).ToNot(HaveOccurred())
		return conf
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		logger = sdkLogger.NewBufferLogger(&bytes.Buffer{})
		Expect(os.MkdirAll(filepath.Join(dir, "loader/entries"), cnst.DirPerm)).ToNot(HaveOccurred())
	})

	writeNorole := func(cmdline string) {
		Expect(os.WriteFile(filepath.Join(dir, "loader/entries", UnassignedArtifactRole+".conf"),
			[]byte("title Kairos\nefi /EFI/kairos/"+UnassignedArtifactRole+".efi\ncmdline "+cmdline+"\n"),
			cnst.FilePerm)).ToNot(HaveOccurred())
	}

	It("drops the live-only install keywords when a role is assigned", func() {
		writeNorole("console=tty1 install-mode rd.immucore.debug")

		Expect(copyArtifactSetRole(vfs.OSFS, dir, UnassignedArtifactRole, "active", logger)).ToNot(HaveOccurred())

		Expect(entryFor("active")["cmdline"]).To(Equal("console=tty1 rd.immucore.debug"))
	})

	It("drops the interactive keywords too", func() {
		writeNorole("console=tty1 install-mode-interactive interactive-install")

		Expect(copyArtifactSetRole(vfs.OSFS, dir, UnassignedArtifactRole, "passive", logger)).ToNot(HaveOccurred())

		Expect(entryFor("passive")["cmdline"]).To(Equal("console=tty1"))
	})

	It("leaves a cmdline without live-only keywords alone", func() {
		writeNorole("console=tty1 rd.immucore.debug selinux=1")

		Expect(copyArtifactSetRole(vfs.OSFS, dir, UnassignedArtifactRole, "recovery", logger)).ToNot(HaveOccurred())

		Expect(entryFor("recovery")["cmdline"]).To(Equal("console=tty1 rd.immucore.debug selinux=1"))
	})

	It("does not match a keyword that is only a substring of another argument", func() {
		writeNorole("console=tty1 kairos.install-mode.debug=1 no-install-mode")

		Expect(copyArtifactSetRole(vfs.OSFS, dir, UnassignedArtifactRole, "active", logger)).ToNot(HaveOccurred())

		Expect(entryFor("active")["cmdline"]).To(Equal("console=tty1 kairos.install-mode.debug=1 no-install-mode"))
	})

	It("still rewrites the efi key and the title", func() {
		writeNorole("console=tty1 install-mode")

		Expect(copyArtifactSetRole(vfs.OSFS, dir, UnassignedArtifactRole, "recovery", logger)).ToNot(HaveOccurred())

		expectedTitle, err := cnst.BootTitleForRole("recovery", "Kairos")
		Expect(err).ToNot(HaveOccurred())

		conf := entryFor("recovery")
		Expect(conf["efi"]).To(Equal("/EFI/kairos/recovery.efi"))
		Expect(conf["title"]).To(Equal(expectedTitle))
	})
})
