package uki

import (
	"bytes"
	"os"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"

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
