package state_test

import (
	"os"
	"path/filepath"
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/state"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

const persistentMount = "/run/cos/persistent"

var _ = Describe("state.KairosState", func() {
	var fs vfs.FS
	var cleanup func()

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{
			persistentMount + "/.keep": "",
		})
		Expect(err).To(BeNil())
	})

	AfterEach(func() {
		cleanup()
	})

	It("returns a Default() with all timestamps set to 'never' and empty sources", func() {
		s := state.Default()
		Expect(s.FirstInstall).To(Equal("never"))
		Expect(s.LastActiveUpgrade).To(Equal("never"))
		Expect(s.LastRecoveryUpgrade).To(Equal("never"))
		Expect(s.LastReset).To(Equal("never"))
		Expect(s.FirstInstallSource).To(BeEmpty())
		Expect(s.LastActiveSource).To(BeEmpty())
		Expect(s.LastRecoverySource).To(BeEmpty())
		Expect(s.LastResetSource).To(BeEmpty())
	})

	It("resolves Path() to <mount>/.kairos-state", func() {
		Expect(state.Path(persistentMount)).To(Equal(filepath.Join(persistentMount, ".kairos-state")))
	})

	It("Load returns Default() when the file does not exist", func() {
		s, err := state.Load(fs, state.Path(persistentMount))
		Expect(err).To(BeNil())
		Expect(s.FirstInstall).To(Equal("never"))
	})

	It("Save + Load round-trips the struct", func() {
		orig := state.Default()
		orig.FirstInstall = time.Date(2026, 7, 21, 9, 51, 0, 0, time.UTC).Format(time.RFC3339)
		orig.FirstInstallSource = "oci://quay.io/kairos/core:v3.0.0"
		Expect(orig.Save(fs, state.Path(persistentMount))).To(Succeed())

		got, err := state.Load(fs, state.Path(persistentMount))
		Expect(err).To(BeNil())
		Expect(got).To(Equal(orig))
	})

	It("Load returns a parse error on a corrupt file", func() {
		p := state.Path(persistentMount)
		raw, err := fs.RawPath(p)
		Expect(err).To(BeNil())
		Expect(os.WriteFile(raw, []byte("this: is: not: valid: yaml: [broken"), 0644)).To(Succeed())

		_, err = state.Load(fs, p)
		Expect(err).NotTo(BeNil())
	})
})
