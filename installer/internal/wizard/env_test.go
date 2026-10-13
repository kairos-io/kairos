package wizard

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SystemEnv", func() {
	It("reads timezones from the zone table, UTC first", func() {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "zone1970.tab"), []byte("# comment\nIT\t+4154+01229\tEurope/Rome\nJP\t+353916+1394441\tAsia/Tokyo\n"), 0o644)).To(Succeed())
		old := zoneinfoDir
		zoneinfoDir = dir
		DeferCleanup(func() { zoneinfoDir = old })
		Expect((&SystemEnv{}).Timezones()).To(Equal([]string{"UTC", "Asia/Tokyo", "Europe/Rome"}))
	})

	It("returns no timezones when the image has no zone table", func() {
		old := zoneinfoDir
		zoneinfoDir = GinkgoT().TempDir()
		DeferCleanup(func() { zoneinfoDir = old })
		Expect((&SystemEnv{}).Timezones()).To(BeEmpty())
	})

	It("lists keymaps by name across layouts, without duplicates", func() {
		dir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(dir, "i386", "qwerty"), 0o755)).To(Succeed())
		for _, f := range []string{"i386/qwerty/it.map.gz", "i386/qwerty/us.map.gz", "i386/qwerty/us.map"} {
			Expect(os.WriteFile(filepath.Join(dir, f), nil, 0o644)).To(Succeed())
		}
		old := keymapDirs
		keymapDirs = []string{dir}
		DeferCleanup(func() { keymapDirs = old })
		Expect((&SystemEnv{}).Keymaps()).To(Equal([]string{"it", "us"}))
	})
})
