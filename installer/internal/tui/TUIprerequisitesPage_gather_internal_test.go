package tui

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kairos-io/kairos/v4/installer/internal/checks"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const crashingCheckPlugin = `#!/bin/sh
echo "tui-check-crash: something went wrong" >&2
exit 1
`

const quietCheckPlugin = `#!/bin/sh
echo '{}'
`

var _ = Describe("the prerequisites page with a crashing check plugin", func() {
	var pluginDir string
	var savedPaths []string

	writePlugin := func(body string) {
		Expect(os.WriteFile(filepath.Join(pluginDir, "tui-check-crash"), []byte(body), 0o755)).To(Succeed())
	}

	BeforeEach(func() {
		l := sdkLogger.NewKairosLogger("test", "error", false)
		mainModel = Model{log: &l, width: 80, height: 24}

		pluginDir = GinkgoT().TempDir()
		writePlugin(crashingCheckPlugin)
		savedPaths = checks.CheckPluginPaths
		checks.CheckPluginPaths = []string{pluginDir}
	})

	AfterEach(func() {
		checks.CheckPluginPaths = savedPaths
	})

	It("blocks on the gather error instead of skipping to the install mode", func() {
		p := newPrerequisitesPage()
		Expect(p.Init()).To(BeNil())
		Expect(p.Skipped()).To(BeFalse())
		Expect(p.View()).To(ContainSubstring("Failed gathering prerequisites"))
		Expect(p.View()).To(ContainSubstring("tui-check-crash"))
		Expect(p.Help()).To(ContainSubstring("retry"))

		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeySpace})
		Expect(cmd).To(BeNil())
		Expect(p.View()).To(ContainSubstring("Failed gathering prerequisites"))
	})

	It("stays blocked on a retry while the plugin still crashes", func() {
		p := newPrerequisitesPage()
		p.Init()

		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
		Expect(cmd).To(BeNil())
		Expect(p.Skipped()).To(BeFalse())
		Expect(p.View()).To(ContainSubstring("Failed gathering prerequisites"))
	})

	It("moves on once a retry gathers cleanly", func() {
		p := newPrerequisitesPage()
		p.Init()

		writePlugin(quietCheckPlugin)
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
		Expect(cmd).ToNot(BeNil())
		Expect(cmd()).To(Equal(GoToPageMsg{PageID: installModePageID}))
		Expect(p.Skipped()).To(BeTrue())
		Expect(p.View()).ToNot(ContainSubstring("Failed gathering prerequisites"))
	})
})
