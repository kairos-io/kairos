package installer_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-sdk/constants"
	"github.com/kairos-io/kairos-sdk/installer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// seed creates a fake installer binary at path (relative to root) and returns
// its absolute path.
func seed(root, path string) string {
	full := filepath.Join(root, path)
	Expect(os.MkdirAll(filepath.Dir(full), 0755)).To(Succeed())
	Expect(os.WriteFile(full, []byte("installer"), 0755)).To(Succeed())
	return full
}

var _ = Describe("Existing", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
	})

	It("reports no installer when none is present", func() {
		path, found := installer.Existing(root)
		Expect(found).To(BeFalse())
		Expect(path).To(BeEmpty())
	})

	It("finds the default path", func() {
		want := seed(root, constants.InstallerDefaultPath)
		path, found := installer.Existing(root)
		Expect(found).To(BeTrue())
		Expect(path).To(Equal(want))
	})

	It("finds the override path", func() {
		want := seed(root, constants.InstallerOverridePath)
		path, found := installer.Existing(root)
		Expect(found).To(BeTrue())
		Expect(path).To(Equal(want))
	})

	It("prefers the override path over the default", func() {
		seed(root, constants.InstallerDefaultPath)
		want := seed(root, constants.InstallerOverridePath)
		path, found := installer.Existing(root)
		Expect(found).To(BeTrue())
		Expect(path).To(Equal(want))
	})
})

var _ = Describe("Resolve", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		// Ensure no ambient env override leaks into the cases that don't set it.
		GinkgoT().Setenv(constants.InstallerEnvVar, "")
	})

	It("returns empty when nothing is present", func() {
		Expect(installer.Resolve(root)).To(BeEmpty())
	})

	It("falls back to the override path", func() {
		want := seed(root, constants.InstallerOverridePath)
		Expect(installer.Resolve(root)).To(Equal(want))
	})

	It("falls back to the default path", func() {
		want := seed(root, constants.InstallerDefaultPath)
		Expect(installer.Resolve(root)).To(Equal(want))
	})

	It("honors KAIROS_INSTALLER over the fixed paths", func() {
		// A fixed path exists, but the env override points elsewhere and wins.
		seed(root, constants.InstallerDefaultPath)
		envPath := filepath.Join(GinkgoT().TempDir(), "custom-installer")
		Expect(os.WriteFile(envPath, []byte("installer"), 0755)).To(Succeed())
		GinkgoT().Setenv(constants.InstallerEnvVar, envPath)

		Expect(installer.Resolve(root)).To(Equal(envPath))
	})

	It("ignores KAIROS_INSTALLER when it points at a missing file", func() {
		want := seed(root, constants.InstallerDefaultPath)
		GinkgoT().Setenv(constants.InstallerEnvVar, filepath.Join(root, "does-not-exist"))

		Expect(installer.Resolve(root)).To(Equal(want))
	})
})
