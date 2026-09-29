package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeInto(p *stepPage, s string) {
	for _, r := range s {
		p.Update(key(string(r)))
	}
}

var _ = Describe("the step page", func() {
	BeforeEach(func() {
		l := sdkLogger.NewKairosLogger("test", "error", false)
		mainModel = Model{log: &l}
	})

	It("has a widget for every field kind the wizard defines", func() {
		for _, k := range wizard.Kinds {
			Expect(widgetFor).To(HaveKey(k), "no TUI widget for kind %q", k)
		}
	})

	It("submits a choice through Apply and moves on", func() {
		steps := []wizard.Step{{ID: wizard.StepDisk, Fields: []wizard.Field{{ID: wizard.FieldDisk, Kind: wizard.KindChoice, Required: true,
			Choices: []wizard.Choice{{Value: "/dev/vda", Label: "/dev/vda"}, {Value: "/dev/vdb", Label: "/dev/vdb"}}}}}}
		mainModel.steps = steps
		p := newStepPage(steps[0])
		p.Update(key("down"))
		_, cmd := p.Update(key("enter"))
		Expect(mainModel.answers.Disk).To(Equal("/dev/vdb"))
		Expect(cmd).ToNot(BeNil())
		Expect(cmd()).To(Equal(GoToPageMsg{PageID: "install_options"}))
	})

	It("shows Apply's error under the field and stays", func() {
		steps := []wizard.Step{{ID: wizard.StepHostname, Optional: true, Fields: []wizard.Field{{ID: wizard.FieldHostname, Kind: wizard.KindText, Label: "Hostname"}}}}
		mainModel.steps = steps
		p := newStepPage(steps[0])
		typeInto(p, "-bad")
		_, cmd := p.Update(key("enter"))
		Expect(cmd).To(BeNil())
		Expect(p.View()).To(ContainSubstring("do not start or end"))
	})

	It("toggles multichoice entries with space", func() {
		f := wizard.Field{ID: wizard.FieldExtensions, Kind: wizard.KindMultiChoice, Choices: []wizard.Choice{{Value: "a", Label: "a"}, {Value: "b", Label: "b"}}}
		w := widgetFor[wizard.KindMultiChoice](f)
		w.Update(key("space"))
		w.Update(key("down"))
		w.Update(key("space"))
		Expect(w.Value()).To(Equal("a\nb"))
	})

	It("collects list entries typed one per enter", func() {
		w := widgetFor[wizard.KindList](wizard.Field{ID: wizard.FieldSSHKeys, Kind: wizard.KindList})
		for _, k := range []string{"github:a", "gitlab:b"} {
			for _, r := range k {
				w.Update(key(string(r)))
			}
			w.Update(key("enter"))
		}
		Expect(w.Value()).To(Equal("github:a\ngitlab:b"))
	})

	It("sends a password and its confirmation under the two field names", func() {
		steps := []wizard.Step{{ID: wizard.StepUser, Fields: []wizard.Field{
			{ID: wizard.FieldUsername, Kind: wizard.KindText}, {ID: wizard.FieldPassword, Kind: wizard.KindPassword}}}}
		p := newStepPage(steps[0])
		typeInto(p, "kairos")
		p.Update(key("tab"))
		typeInto(p, "pw")
		p.Update(key("tab"))
		typeInto(p, "pw")
		v := p.values()
		Expect(v[wizard.FieldPassword]).To(Equal("pw"))
		Expect(v[wizard.FieldPassword+wizard.ConfirmSuffix]).To(Equal("pw"))
		Expect(p.View()).ToNot(ContainSubstring("pw"))
	})

	It("keeps a long choice list inside an 80x24 console and filters by typing", func() {
		var choices []wizard.Choice
		for _, z := range []string{"Africa/Abidjan", "America/New_York", "Asia/Tokyo", "Europe/Berlin", "Europe/Rome", "Pacific/Auckland", "UTC"} {
			for i := 0; i < 60; i++ {
				choices = append(choices, wizard.Choice{Value: z, Label: z})
			}
		}
		w := widgetFor[wizard.KindChoice](wizard.Field{ID: wizard.FieldTimezone, Kind: wizard.KindChoice, Choices: choices})
		for i := 0; i < 200; i++ {
			w.Update(key("down"))
		}
		view := w.View(true, 76)
		Expect(strings.Count(view, "\n")).To(BeNumerically("<=", visibleChoiceRows+3))
		Expect(view).To(ContainSubstring(">"))
		typeInto2 := func(s string) {
			for _, r := range s {
				w.Update(key(string(r)))
			}
		}
		w.Update(key("/"))
		typeInto2("rome")
		Expect(w.View(true, 76)).To(ContainSubstring("Europe/Rome"))
		Expect(w.View(true, 76)).ToNot(ContainSubstring("Asia/Tokyo"))
		Expect(w.Value()).To(Equal("Europe/Rome"))
	})

	It("loads the current answer so a revisited step shows it", func() {
		mainModel.answers = wizard.Answers{Hostname: "edge-01"}
		p := newStepPage(wizard.Step{ID: wizard.StepHostname, Fields: []wizard.Field{{ID: wizard.FieldHostname, Kind: wizard.KindText}}})
		p.Init()
		Expect(p.values()[wizard.FieldHostname]).To(Equal("edge-01"))
	})
})
