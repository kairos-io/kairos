package tui

import (
	"os"
	"path/filepath"

	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RenderCloudConfig", func() {
	It("renders a user stage with ssh keys at the network stage", func() {
		m := &Model{
			disk:         "/dev/sda",
			username:     "kairos",
			password:     "hashhash",
			sshKeys:      []string{"ssh-ed25519 AAAA"},
			finishAction: "reboot",
		}
		out, err := RenderCloudConfig(m)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(HavePrefix("#cloud-config\n"))
		Expect(out).To(ContainSubstring("network:"))
		Expect(out).To(ContainSubstring("kairos"))
		Expect(out).To(ContainSubstring("ssh-ed25519 AAAA"))
		Expect(out).To(ContainSubstring("reboot: true"))
	})

	It("sets no_users and no user stage when no username is given", func() {
		m := &Model{disk: "/dev/sda", finishAction: "nothing"}
		out, err := RenderCloudConfig(m)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("nousers: true"))
		Expect(out).ToNot(ContainSubstring("network:"))
	})

	It("uses the initramfs stage when there are no ssh keys", func() {
		m := &Model{disk: "/dev/sda", username: "kairos", password: "x"}
		out, err := RenderCloudConfig(m)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("initramfs:"))
	})
})

var _ = Describe("RenderRedactedCloudConfig", func() {
	It("replaces the password and never leaks the original hash", func() {
		m := &Model{
			disk:     "/dev/sda",
			username: "kairos",
			password: "supersecrethash",
			sshKeys:  []string{"ssh-ed25519 AAAA"},
		}
		out, err := RenderRedactedCloudConfig(m)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("***REDACTED***"))
		Expect(out).ToNot(ContainSubstring("supersecrethash"))
	})

	It("leaves the original model untouched", func() {
		m := &Model{disk: "/dev/sda", username: "kairos", password: "supersecrethash"}
		_, err := RenderRedactedCloudConfig(m)
		Expect(err).ToNot(HaveOccurred())
		Expect(m.password).To(Equal("supersecrethash"))
	})
})

var _ = Describe("installProcessPage agent resolution", func() {
	It("reports an error and leaves no temp config when kairos-agent is absent", func() {
		// No agent anywhere.
		GinkgoT().Setenv("KAIROS_AGENT_BIN", "")
		GinkgoT().Setenv("PATH", GinkgoT().TempDir())

		// Minimal valid model for RenderCloudConfig.
		logger := sdkLogger.NewKairosLogger("installer", "info", true)
		mainModel = InitialModel(&logger, "") // resets global mainModel
		mainModel.disk = "/dev/sda"
		mainModel.finishAction = "nothing"

		before := tmpConfigCount()
		p := newInstallProcessPage()
		p.Init()
		Expect(p.errorMsg).To(ContainSubstring("kairos-agent not found"))
		Expect(tmpConfigCount()).To(Equal(before)) // no kairos-install-*.yaml leaked
	})
})

// tmpConfigCount counts leftover temp cloud-config files.
func tmpConfigCount() int {
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "kairos-install-*.yaml"))
	return len(matches)
}
