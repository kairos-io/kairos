package prereqs_test

import (
	"github.com/kairos-io/kairos-installer/prereqs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Prereqs contract", func() {
	Describe("ParseChecks", func() {
		It("returns nil for an empty payload", func() {
			checks, err := prereqs.ParseChecks("")
			Expect(err).ToNot(HaveOccurred())
			Expect(checks).To(BeNil())
		})
		It("parses a list of checks with prompt metadata", func() {
			data := `[{"id":"diskwipe","title":"Leftover data","message":"found disks","severity":"warning","prompt":"multiselect","promptText":"Select disks","options":[{"id":"/dev/sdb","label":"/dev/sdb"}]}]`
			checks, err := prereqs.ParseChecks(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(checks).To(HaveLen(1))
			Expect(checks[0].ID).To(Equal("diskwipe"))
			Expect(checks[0].Severity).To(Equal(prereqs.SeverityWarning))
			Expect(checks[0].Prompt).To(Equal(prereqs.PromptMultiSelect))
			Expect(checks[0].Options).To(HaveLen(1))
			Expect(checks[0].Interactive()).To(BeTrue())
		})
		It("errors on malformed JSON", func() {
			_, err := prereqs.ParseChecks("{not json")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ParseApplyResults", func() {
		It("returns nil for an empty payload", func() {
			res, err := prereqs.ParseApplyResults("")
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeNil())
		})
		It("parses results", func() {
			res, err := prereqs.ParseApplyResults(`[{"id":"diskwipe","success":true,"message":"wiped"}]`)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(HaveLen(1))
			Expect(res[0].Success).To(BeTrue())
		})
	})

	Describe("Interactive / IsError", func() {
		It("classifies prompt and severity", func() {
			Expect(prereqs.Check{Prompt: prereqs.PromptNone}.Interactive()).To(BeFalse())
			Expect(prereqs.Check{Prompt: prereqs.PromptConfirm}.Interactive()).To(BeTrue())
			Expect(prereqs.Check{Severity: prereqs.SeverityError}.IsError()).To(BeTrue())
			Expect(prereqs.Check{Severity: prereqs.SeverityInfo}.IsError()).To(BeFalse())
		})
	})

	Describe("Satisfied", func() {
		It("display-only checks are never satisfied", func() {
			Expect(prereqs.Satisfied(prereqs.Check{Prompt: prereqs.PromptNone}, prereqs.Answer{})).To(BeFalse())
		})
		It("confirm is satisfied when confirmed", func() {
			c := prereqs.Check{Prompt: prereqs.PromptConfirm}
			Expect(prereqs.Satisfied(c, prereqs.Answer{Confirmed: false})).To(BeFalse())
			Expect(prereqs.Satisfied(c, prereqs.Answer{Confirmed: true})).To(BeTrue())
		})
		It("text is satisfied when non-empty", func() {
			c := prereqs.Check{Prompt: prereqs.PromptText}
			Expect(prereqs.Satisfied(c, prereqs.Answer{})).To(BeFalse())
			Expect(prereqs.Satisfied(c, prereqs.Answer{Text: "x"})).To(BeTrue())
		})
		It("select/multiselect satisfied when something is chosen", func() {
			Expect(prereqs.Satisfied(prereqs.Check{Prompt: prereqs.PromptSelect}, prereqs.Answer{})).To(BeFalse())
			Expect(prereqs.Satisfied(prereqs.Check{Prompt: prereqs.PromptSelect}, prereqs.Answer{Selected: []string{"a"}})).To(BeTrue())
			Expect(prereqs.Satisfied(prereqs.Check{Prompt: prereqs.PromptMultiSelect}, prereqs.Answer{Selected: []string{"a", "b"}})).To(BeTrue())
		})
		It("multiselect with an empty selection is not satisfied", func() {
			Expect(prereqs.Satisfied(prereqs.Check{Prompt: prereqs.PromptMultiSelect}, prereqs.Answer{Selected: []string{}})).To(BeFalse())
			Expect(prereqs.Satisfied(prereqs.Check{Prompt: prereqs.PromptMultiSelect}, prereqs.Answer{})).To(BeFalse())
		})
	})

	Describe("Blocker", func() {
		It("returns false when nothing blocks", func() {
			checks := []prereqs.Check{
				{ID: "a", Severity: prereqs.SeverityInfo},
				{ID: "b", Severity: prereqs.SeverityWarning},
			}
			_, blocked := prereqs.Blocker(checks, nil)
			Expect(blocked).To(BeFalse())
		})
		It("blocks on a non-interactive blocking check forever", func() {
			checks := []prereqs.Check{{ID: "arch", Severity: prereqs.SeverityError, Blocking: true}}
			c, blocked := prereqs.Blocker(checks, map[string]prereqs.Answer{})
			Expect(blocked).To(BeTrue())
			Expect(c.ID).To(Equal("arch"))
		})
		It("a blocking confirm unblocks once confirmed", func() {
			checks := []prereqs.Check{{ID: "ack", Blocking: true, Prompt: prereqs.PromptConfirm}}
			_, blocked := prereqs.Blocker(checks, map[string]prereqs.Answer{})
			Expect(blocked).To(BeTrue())
			_, blocked = prereqs.Blocker(checks, map[string]prereqs.Answer{"ack": {Confirmed: true}})
			Expect(blocked).To(BeFalse())
		})
	})

	Describe("BuildDecisions", func() {
		It("emits one decision per interactive check carrying the answer", func() {
			checks := []prereqs.Check{
				{ID: "info", Prompt: prereqs.PromptNone},
				{ID: "ack", Prompt: prereqs.PromptConfirm},
				{ID: "name", Prompt: prereqs.PromptText},
				{ID: "disks", Prompt: prereqs.PromptMultiSelect},
			}
			answers := map[string]prereqs.Answer{
				"ack":   {Confirmed: true},
				"name":  {Text: "node1"},
				"disks": {Selected: []string{"/dev/sdb", "/dev/sdc"}},
			}
			decisions := prereqs.BuildDecisions(checks, answers)
			Expect(decisions).To(HaveLen(3))
			Expect(decisions).To(ContainElement(prereqs.Decision{ID: "ack", Prompt: prereqs.PromptConfirm, Confirmed: true}))
			Expect(decisions).To(ContainElement(prereqs.Decision{ID: "name", Prompt: prereqs.PromptText, Text: "node1"}))
			Expect(decisions).To(ContainElement(prereqs.Decision{ID: "disks", Prompt: prereqs.PromptMultiSelect, Selected: []string{"/dev/sdb", "/dev/sdc"}}))
		})
		It("returns nil when no check is interactive", func() {
			checks := []prereqs.Check{{ID: "info", Prompt: prereqs.PromptNone}}
			Expect(prereqs.BuildDecisions(checks, nil)).To(BeNil())
		})
	})

	Describe("ClassifyFailures", func() {
		checks := []prereqs.Check{
			{ID: "req", Required: true},
			{ID: "opt", Required: false},
		}
		It("ignores successful results", func() {
			rf, of := prereqs.ClassifyFailures(checks, []prereqs.ApplyResult{
				{ID: "req", Success: true},
				{ID: "opt", Success: true},
			})
			Expect(rf).To(BeEmpty())
			Expect(of).To(BeEmpty())
		})
		It("buckets failures by the check's Required flag", func() {
			rf, of := prereqs.ClassifyFailures(checks, []prereqs.ApplyResult{
				{ID: "req", Success: false, Message: "boom"},
				{ID: "opt", Success: false, Message: "meh"},
			})
			Expect(rf).To(HaveLen(1))
			Expect(rf[0].ID).To(Equal("req"))
			Expect(of).To(HaveLen(1))
			Expect(of[0].ID).To(Equal("opt"))
		})
		It("treats an unknown ID as optional", func() {
			rf, of := prereqs.ClassifyFailures(checks, []prereqs.ApplyResult{
				{ID: "ghost", Success: false},
			})
			Expect(rf).To(BeEmpty())
			Expect(of).To(HaveLen(1))
		})
	})

	Describe("DefaultAnswer", func() {
		It("seeds confirm from default yes", func() {
			Expect(prereqs.DefaultAnswer(prereqs.Check{Prompt: prereqs.PromptConfirm, Default: "yes"}).Confirmed).To(BeTrue())
			Expect(prereqs.DefaultAnswer(prereqs.Check{Prompt: prereqs.PromptConfirm, Default: "no"}).Confirmed).To(BeFalse())
		})
		It("seeds text and select from default", func() {
			Expect(prereqs.DefaultAnswer(prereqs.Check{Prompt: prereqs.PromptText, Default: "hi"}).Text).To(Equal("hi"))
			Expect(prereqs.DefaultAnswer(prereqs.Check{Prompt: prereqs.PromptSelect, Default: "opt1"}).Selected).To(Equal([]string{"opt1"}))
		})
		It("seeds multiselect from a comma-separated default", func() {
			a := prereqs.DefaultAnswer(prereqs.Check{Prompt: prereqs.PromptMultiSelect, Default: "/dev/sdb, /dev/sdc"})
			Expect(a.Selected).To(Equal([]string{"/dev/sdb", "/dev/sdc"}))
		})
		It("returns an empty answer for multiselect with no default", func() {
			Expect(prereqs.DefaultAnswer(prereqs.Check{Prompt: prereqs.PromptMultiSelect}).Selected).To(BeNil())
		})
		It("returns an empty answer for a display-only check", func() {
			Expect(prereqs.DefaultAnswer(prereqs.Check{Prompt: prereqs.PromptNone, Default: "x"})).To(Equal(prereqs.Answer{}))
		})
	})
})
