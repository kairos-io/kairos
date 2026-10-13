package tui

import (
	"os"
	"path/filepath"

	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("installProcessPage agent resolution", func() {
	It("reports an error and leaves no temp config when kairos-agent is absent", func() {
		// No agent anywhere.
		GinkgoT().Setenv("KAIROS_AGENT_BIN", "")
		GinkgoT().Setenv("PATH", GinkgoT().TempDir())

		logger := sdkLogger.NewKairosLogger("installer", "info", true)
		mainModel = InitialModel(&logger, "") // resets global mainModel
		mainModel.answers.Disk = "/dev/sda"

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
