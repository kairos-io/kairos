package tui

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-installer/prereqs"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/mudler/go-pluggable"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests exercise the prerequisites round-trip end to end against a real
// tui-check plugin (the bash fixture under testdata/tui-checks). They build a
// dedicated go-pluggable manager loaded only with the fixture, exactly like the
// TUI page does — completely separate from the global agent-provider bus.
var _ = Describe("Prerequisites round-trip", Label("prerequisites"), func() {
	var m *pluggable.Manager
	var log sdkLogger.KairosLogger
	var applyLog string
	var fixtureDir string

	BeforeEach(func() {
		var err error
		fixtureDir, err = filepath.Abs("testdata/tui-checks")
		Expect(err).ToNot(HaveOccurred())
		// git should preserve the exec bit, but make sure.
		Expect(os.Chmod(filepath.Join(fixtureDir, "tui-check-prereqtest"), 0o755)).To(Succeed())

		f, err := os.CreateTemp("", "prereq-apply-*.log")
		Expect(err).ToNot(HaveOccurred())
		applyLog = f.Name()
		Expect(f.Close()).To(Succeed())
		Expect(os.Setenv("PREREQTEST_APPLY_LOG", applyLog)).To(Succeed())

		log = sdkLogger.NewNullLogger()
		m = pluggable.NewManager([]pluggable.EventType{prereqs.EventChecks, prereqs.EventChecksApply})
		m.Autoload(CheckPluginPrefix, fixtureDir)
		m.Register()
	})

	AfterEach(func() {
		_ = os.Unsetenv("PREREQTEST_APPLY_LOG")
		_ = os.Remove(applyLog)
	})

	It("loads the tui-check fixture plugin", func() {
		Expect(m.Plugins).ToNot(BeEmpty())
		Expect(m.Plugins[0].Name).To(Equal("prereqtest"))
	})

	It("gathers checks of every prompt type from the plugin", func() {
		checks, err := gatherChecks(m, log, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(checks).To(HaveLen(5))

		byID := map[string]prereqs.Check{}
		for _, c := range checks {
			byID[c.ID] = c
		}
		Expect(byID["t-info"].Interactive()).To(BeFalse())
		Expect(byID["t-confirm"].Prompt).To(Equal(prereqs.PromptConfirm))
		Expect(byID["t-text"].Prompt).To(Equal(prereqs.PromptText))
		Expect(byID["t-select"].Prompt).To(Equal(prereqs.PromptSelect))
		Expect(byID["t-select"].Default).To(Equal("a"))
		Expect(byID["t-multi"].Prompt).To(Equal(prereqs.PromptMultiSelect))
		Expect(byID["t-multi"].Options).To(HaveLen(2))
	})

	It("sends the user's answers on apply and receives results", func() {
		checks, err := gatherChecks(m, log, "")
		Expect(err).ToNot(HaveOccurred())

		answers := map[string]prereqs.Answer{
			"t-confirm": {Confirmed: true},
			"t-text":    {Text: "node1"},
			"t-select":  {Selected: []string{"b"}},
			"t-multi":   {Selected: []string{"/dev/sdb", "/dev/sdc"}},
		}
		decisions := prereqs.BuildDecisions(checks, answers)
		// 4 interactive checks (info is display-only).
		Expect(decisions).To(HaveLen(4))

		results, err := applyDecisions(m, log, decisions, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].ID).To(Equal("t-multi"))
		Expect(results[0].Success).To(BeTrue())

		// The plugin recorded the payload it received: assert the user's
		// answers crossed the bus intact.
		logged, err := os.ReadFile(applyLog)
		Expect(err).ToNot(HaveOccurred())
		content := string(logged)
		Expect(content).To(ContainSubstring("t-confirm"))
		Expect(content).To(ContainSubstring("node1"))
		Expect(content).To(ContainSubstring("/dev/sdb"))
		Expect(content).To(ContainSubstring("/dev/sdc"))
	})

	It("surfaces apply failures and classifies them required vs optional", func() {
		Expect(os.Setenv("PREREQTEST_FAIL", "1")).To(Succeed())
		defer func() { _ = os.Unsetenv("PREREQTEST_FAIL") }()

		checks, err := gatherChecks(m, log, "")
		Expect(err).ToNot(HaveOccurred())
		decisions := prereqs.BuildDecisions(checks, map[string]prereqs.Answer{
			"t-multi": {Selected: []string{"/dev/sdb"}},
		})
		results, err := applyDecisions(m, log, decisions, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].Success).To(BeFalse())
		Expect(results[0].Message).To(Equal("boom"))

		// Same failure classifies as required or optional purely by the
		// check's Required flag.
		rf, of := prereqs.ClassifyFailures([]prereqs.Check{{ID: "t-multi", Required: true}}, results)
		Expect(rf).To(HaveLen(1))
		Expect(of).To(BeEmpty())

		rf, of = prereqs.ClassifyFailures([]prereqs.Check{{ID: "t-multi"}}, results)
		Expect(rf).To(BeEmpty())
		Expect(of).To(HaveLen(1))
	})

	It("a blocking unsatisfied check stops the install", func() {
		checks := []prereqs.Check{
			{ID: "ram", Title: "RAM", Severity: prereqs.SeverityError, Blocking: true},
		}
		_, blocked := prereqs.Blocker(checks, map[string]prereqs.Answer{})
		Expect(blocked).To(BeTrue())
	})
})
