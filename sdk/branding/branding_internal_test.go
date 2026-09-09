package branding

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// write drops a file with the given content under dir and returns its path.
func write(dir, name, content string) string {
	full := filepath.Join(dir, name)
	Expect(os.WriteFile(full, []byte(content), 0644)).To(Succeed())
	return full
}

var _ = Describe("File", func() {
	It("points at the branding directory", func() {
		Expect(File("banner")).To(Equal("/etc/kairos/branding/banner"))
	})
})

var _ = Describe("defaultTitle", func() {
	It("falls back when the image brands no title", func() {
		Expect(defaultTitle(GinkgoT().TempDir())).To(Equal(DefaultInteractiveInstallerTitle))
	})

	It("returns the branded title", func() {
		dir := GinkgoT().TempDir()
		write(dir, "interactive_install_text", "Hadron Installer")
		Expect(defaultTitle(dir)).To(Equal("Hadron Installer"))
	})
})

var _ = Describe("WebUI", func() {
	It("has no address by default", func() {
		Expect(WebUI{}.HasAddress()).To(BeFalse())
	})

	It("has an address once one is set", func() {
		Expect(WebUI{ListenAddress: ":9000"}.HasAddress()).To(BeTrue())
	})
})

var _ = Describe("loadConfig", func() {
	var brandingDir, configDir string

	BeforeEach(func() {
		brandingDir = GinkgoT().TempDir()
		configDir = GinkgoT().TempDir()
	})

	It("returns an empty config when the file is missing", func() {
		cfg, err := loadConfig(brandingDir, filepath.Join(configDir, "absent.yaml"))
		Expect(err).ToNot(HaveOccurred())
		Expect(*cfg).To(Equal(Config{}))
	})

	It("reads the webui and branding keys", func() {
		path := write(configDir, "agent.yaml", `
fast: true
webui:
  disable: true
  listen_address: ":9000"
branding:
  install: from the config
`)
		cfg, err := loadConfig(brandingDir, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Fast).To(BeTrue())
		Expect(cfg.WebUI.Disable).To(BeTrue())
		Expect(cfg.WebUI.ListenAddress).To(Equal(":9000"))
		Expect(cfg.Branding.Install).To(Equal("from the config"))
	})

	It("fills the screens the config leaves empty from the branding files", func() {
		path := write(configDir, "agent.yaml", "branding:\n  install: from the config\n")
		write(brandingDir, "install_text", "from the file")
		write(brandingDir, "interactive_install_text", "interactive")
		write(brandingDir, "recovery_text", "recovery")
		write(brandingDir, "reset_text", "reset")

		cfg, err := loadConfig(brandingDir, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Branding.Install).To(Equal("from the config"))
		Expect(cfg.Branding.InteractiveInstall).To(Equal("interactive"))
		Expect(cfg.Branding.Recovery).To(Equal("recovery"))
		Expect(cfg.Branding.Reset).To(Equal("reset"))
	})

	It("lets a later file override an earlier one", func() {
		first := write(configDir, "first.yaml", "webui:\n  listen_address: \":8080\"\n")
		second := write(configDir, "second.yaml", "webui:\n  listen_address: \":9000\"\n")
		cfg, err := loadConfig(brandingDir, first, second)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.WebUI.ListenAddress).To(Equal(":9000"))
	})

	It("ignores an unparseable file rather than failing the boot", func() {
		path := write(configDir, "agent.yaml", "webui: [not, a, map]\n")
		cfg, err := loadConfig(brandingDir, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.WebUI.ListenAddress).To(BeEmpty())
	})
})
