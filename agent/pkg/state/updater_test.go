package state_test

import (
	"os"
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/state"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("state updater", func() {
	var fs vfs.FS
	var cleanup func()
	var clock state.Clock
	var frozen time.Time

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{
			persistentMount + "/.keep": "",
		})
		Expect(err).To(BeNil())
		frozen = time.Date(2026, 7, 21, 10, 15, 0, 0, time.UTC)
		clock = func() time.Time { return frozen }
	})

	AfterEach(func() {
		cleanup()
	})

	It("RecordInstall writes only the first-install fields", func() {
		Expect(state.RecordInstall(fs, persistentMount, "oci://quay.io/kairos/core:v3.0.0", clock)).To(Succeed())

		s, err := state.Load(fs, state.Path(persistentMount))
		Expect(err).To(BeNil())
		Expect(s.FirstInstall).To(Equal("2026-07-21T10:15:00Z"))
		Expect(s.FirstInstallSource).To(Equal("oci://quay.io/kairos/core:v3.0.0"))
		Expect(s.LastActiveUpgrade).To(Equal("never"))
		Expect(s.LastRecoveryUpgrade).To(Equal("never"))
		Expect(s.LastReset).To(Equal("never"))
	})

	It("RecordActiveUpgrade writes only the active fields", func() {
		Expect(state.RecordActiveUpgrade(fs, persistentMount, "oci://a:1", clock)).To(Succeed())

		s, err := state.Load(fs, state.Path(persistentMount))
		Expect(err).To(BeNil())
		Expect(s.LastActiveUpgrade).To(Equal("2026-07-21T10:15:00Z"))
		Expect(s.LastActiveSource).To(Equal("oci://a:1"))
		Expect(s.LastRecoveryUpgrade).To(Equal("never"))
		Expect(s.LastReset).To(Equal("never"))
	})

	It("RecordActiveUpgrade overwrites the previous active values in place", func() {
		Expect(state.RecordActiveUpgrade(fs, persistentMount, "oci://a:1", clock)).To(Succeed())
		frozen = frozen.Add(time.Hour)
		Expect(state.RecordActiveUpgrade(fs, persistentMount, "oci://a:2", clock)).To(Succeed())

		s, err := state.Load(fs, state.Path(persistentMount))
		Expect(err).To(BeNil())
		Expect(s.LastActiveUpgrade).To(Equal("2026-07-21T11:15:00Z"))
		Expect(s.LastActiveSource).To(Equal("oci://a:2"))
	})

	It("RecordRecoveryUpgrade writes only the recovery fields", func() {
		Expect(state.RecordRecoveryUpgrade(fs, persistentMount, "oci://recovery:1", clock)).To(Succeed())

		s, err := state.Load(fs, state.Path(persistentMount))
		Expect(err).To(BeNil())
		Expect(s.LastRecoveryUpgrade).To(Equal("2026-07-21T10:15:00Z"))
		Expect(s.LastRecoverySource).To(Equal("oci://recovery:1"))
		Expect(s.LastActiveUpgrade).To(Equal("never"))
		Expect(s.LastReset).To(Equal("never"))
	})

	It("RecordReset writes reset fields and leaves FirstInstall alone", func() {
		Expect(state.RecordInstall(fs, persistentMount, "oci://core:v3.0.0", clock)).To(Succeed())
		frozen = frozen.Add(time.Hour)
		Expect(state.RecordReset(fs, persistentMount, "oci://core:v3.0.0", clock)).To(Succeed())

		s, err := state.Load(fs, state.Path(persistentMount))
		Expect(err).To(BeNil())
		Expect(s.LastReset).To(Equal("2026-07-21T11:15:00Z"))
		Expect(s.LastResetSource).To(Equal("oci://core:v3.0.0"))
		Expect(s.FirstInstall).To(Equal("2026-07-21T10:15:00Z"))
		Expect(s.FirstInstallSource).To(Equal("oci://core:v3.0.0"))
	})

	It("Record* renames a corrupt file to .bak and writes a fresh one", func() {
		p := state.Path(persistentMount)
		raw, err := fs.RawPath(p)
		Expect(err).To(BeNil())
		Expect(os.WriteFile(raw, []byte("nope: [unterminated"), 0644)).To(Succeed())

		Expect(state.RecordInstall(fs, persistentMount, "oci://x:1", clock)).To(Succeed())

		bakRaw, err := fs.RawPath(p + ".bak")
		Expect(err).To(BeNil())
		_, statErr := os.Stat(bakRaw)
		Expect(statErr).To(BeNil())

		s, err := state.Load(fs, p)
		Expect(err).To(BeNil())
		Expect(s.FirstInstall).To(Equal("2026-07-21T10:15:00Z"))
		Expect(s.FirstInstallSource).To(Equal("oci://x:1"))
	})
})
