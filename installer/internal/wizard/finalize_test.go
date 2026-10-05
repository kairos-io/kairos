package wizard_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

func installOf(out string) map[string]any {
	doc := map[string]any{}
	Expect(yaml.Unmarshal([]byte(out), &doc)).To(Succeed())
	install, _ := doc["install"].(map[string]any)
	return install
}

var _ = Describe("Finalize", func() {
	It("puts the confirmed device back over one edited into the YAML", func() {
		out, err := wizard.Finalize("#cloud-config\ninstall:\n  device: /dev/vdb\n  auto: true\n", wizard.Overrides{Device: "/dev/sda"})
		Expect(err).ToNot(HaveOccurred())
		Expect(installOf(out)).To(HaveKeyWithValue("device", "/dev/sda"))
		Expect(installOf(out)).To(HaveKeyWithValue("auto", true))
	})

	It("keeps the document's device when none is given", func() {
		out, err := wizard.Finalize("#cloud-config\ninstall:\n  device: /dev/vdb\n", wizard.Overrides{})
		Expect(err).ToNot(HaveOccurred())
		Expect(installOf(out)).To(HaveKeyWithValue("device", "/dev/vdb"))
	})

	It("writes the finish action back, zero values included", func() {
		out, err := wizard.Finalize("#cloud-config\ninstall:\n  poweroff: true\n", wizard.Overrides{Device: "/dev/sda"})
		Expect(err).ToNot(HaveOccurred())
		Expect(installOf(out)).To(HaveKeyWithValue("poweroff", false))
		Expect(installOf(out)).To(HaveKeyWithValue("reboot", false))

		out, err = wizard.Finalize("#cloud-config\n", wizard.Overrides{Device: "/dev/sda", FinishAction: wizard.FinishReboot})
		Expect(err).ToNot(HaveOccurred())
		Expect(installOf(out)).To(HaveKeyWithValue("reboot", true))
	})

	It("writes the source only when one is given", func() {
		out, err := wizard.Finalize("#cloud-config\ninstall:\n  source: oci:mine\n", wizard.Overrides{Device: "/dev/sda"})
		Expect(err).ToNot(HaveOccurred())
		Expect(installOf(out)).To(HaveKeyWithValue("source", "oci:mine"))
		out, err = wizard.Finalize("#cloud-config\n", wizard.Overrides{Device: "/dev/sda", Source: "oci:boot"})
		Expect(err).ToNot(HaveOccurred())
		Expect(installOf(out)).To(HaveKeyWithValue("source", "oci:boot"))
	})

	It("sets nousers only when asked to and the document creates no users", func() {
		out, err := wizard.Finalize("#cloud-config\n", wizard.Overrides{Device: "/dev/sda", DefaultNoUsers: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(installOf(out)).To(HaveKeyWithValue("nousers", true))
		out, err = wizard.Finalize("#cloud-config\nstages:\n  boot: []\n", wizard.Overrides{Device: "/dev/sda", DefaultNoUsers: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(installOf(out)).ToNot(HaveKey("nousers"))
		out, err = wizard.Finalize("#cloud-config\n", wizard.Overrides{Device: "/dev/sda"})
		Expect(err).ToNot(HaveOccurred())
		Expect(installOf(out)).ToNot(HaveKey("nousers"))
	})

	It("keeps every key it does not own", func() {
		out, err := wizard.Finalize("#cloud-config\nk3s:\n  enabled: true\nstages:\n  boot:\n    - commands: [\"echo hi\"]\n", wizard.Overrides{Device: "/dev/sda"})
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("k3s:"))
		Expect(out).To(ContainSubstring("echo hi"))
	})

	It("treats an empty document as an empty config", func() {
		out, err := wizard.Finalize("", wizard.Overrides{Device: "auto"})
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(HavePrefix("#cloud-config\n"))
		Expect(installOf(out)).To(HaveKeyWithValue("device", "auto"))
	})

	It("rejects YAML that does not parse, naming the problem", func() {
		_, err := wizard.Finalize("#cloud-config\nusers: [unterminated\n", wizard.Overrides{Device: "/dev/sda"})
		Expect(err).To(MatchError(ContainSubstring("cloud-config is not valid YAML")))
	})

	It("rejects an install key that is not a mapping", func() {
		_, err := wizard.Finalize("#cloud-config\ninstall: yes\n", wizard.Overrides{Device: "/dev/sda"})
		Expect(err).To(MatchError(ContainSubstring("install must be a mapping")))
	})
})
