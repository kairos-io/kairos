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

	Describe("keyboard fixes", func() {
		userSteps := func() []wizard.Step {
			return []wizard.Step{{ID: wizard.StepUser, Optional: true, Fields: []wizard.Field{
				{ID: wizard.FieldUsername, Kind: wizard.KindText}, {ID: wizard.FieldPassword, Kind: wizard.KindPassword}}}}
		}
		zones := []wizard.Choice{{Value: "Asia/Tokyo", Label: "Asia/Tokyo"}, {Value: "Europe/Berlin", Label: "Europe/Berlin"},
			{Value: "Europe/Rome", Label: "Europe/Rome"}, {Value: "UTC", Label: "UTC"}}

		It("moves from the password to its confirmation on enter, then submits", func() {
			mainModel.steps = userSteps()
			p := newStepPage(mainModel.steps[0])
			p.Init()
			typeInto(p, "kairos")
			p.Update(key("enter"))
			typeInto(p, "pw")
			p.Update(key("enter"))
			Expect(p.errs).To(BeEmpty(), "enter in the password submitted the step")
			Expect(mainModel.answers.Username).To(BeEmpty())
			typeInto(p, "pw")
			_, cmd := p.Update(key("enter"))
			Expect(p.errs).To(BeEmpty())
			Expect(mainModel.answers.Username).To(Equal("kairos"))
			Expect(mainModel.answers.PasswordHash).ToNot(BeEmpty())
			Expect(cmd).ToNot(BeNil())
			Expect(cmd()).To(Equal(GoToPageMsg{PageID: "customization"}))
		})

		It("clears the typed password once the step is saved", func() {
			mainModel.steps = userSteps()
			p := newStepPage(mainModel.steps[0])
			p.Init()
			typeInto(p, "kairos")
			p.Update(key("tab"))
			typeInto(p, "pw")
			p.Update(key("tab"))
			typeInto(p, "pw")
			_, cmd := p.Update(key("enter"))
			Expect(cmd).ToNot(BeNil())
			Expect(p.widgets[1].Value()).To(BeEmpty())
			Expect(p.values()[wizard.FieldPassword]).To(BeEmpty())
			Expect(p.values()[wizard.FieldPassword+wizard.ConfirmSuffix]).To(BeEmpty())
		})

		It("moves the cursor with the arrows while filtering", func() {
			w := widgetFor[wizard.KindChoice](wizard.Field{ID: wizard.FieldTimezone, Kind: wizard.KindChoice, Required: true, Choices: zones})
			w.Update(key("/"))
			for _, r := range "europe" {
				w.Update(key(string(r)))
			}
			Expect(w.Value()).To(Equal("Europe/Berlin"))
			w.Update(key("down"))
			Expect(w.Value()).To(Equal("Europe/Rome"))
			w.Update(key("up"))
			Expect(w.Value()).To(Equal("Europe/Berlin"))
		})

		It("toggles a multichoice entry with space while filtering", func() {
			w := widgetFor[wizard.KindMultiChoice](wizard.Field{ID: wizard.FieldExtensions, Kind: wizard.KindMultiChoice, Choices: zones})
			w.Update(key("/"))
			for _, r := range "rome" {
				w.Update(key(string(r)))
			}
			w.Update(key("space"))
			Expect(w.Value()).To(Equal("Europe/Rome"))
			Expect(w.View(true, 76)).To(ContainSubstring("/rome"))
		})

		It("keeps the cursor on the filtered entry when esc leaves the filter", func() {
			w := widgetFor[wizard.KindChoice](wizard.Field{ID: wizard.FieldTimezone, Kind: wizard.KindChoice, Required: true, Choices: zones})
			w.Update(key("/"))
			for _, r := range "rome" {
				w.Update(key(string(r)))
			}
			w.Update(tea.KeyMsg{Type: tea.KeyEsc})
			Expect(w.Value()).To(Equal("Europe/Rome"))
			Expect(w.View(true, 76)).To(ContainSubstring("Asia/Tokyo"))
		})

		It("still shows the filter when it matches nothing", func() {
			w := widgetFor[wizard.KindChoice](wizard.Field{ID: wizard.FieldTimezone, Kind: wizard.KindChoice, Required: true, Choices: zones})
			w.Update(key("/"))
			for _, r := range "zzz" {
				w.Update(key(string(r)))
			}
			Expect(w.View(true, 76)).To(ContainSubstring("/zzz"))
		})

		It("clears a leftover filter when the step is loaded again", func() {
			w := widgetFor[wizard.KindChoice](wizard.Field{ID: wizard.FieldTimezone, Kind: wizard.KindChoice, Required: true, Choices: zones})
			w.Update(key("/"))
			for _, r := range "rome" {
				w.Update(key(string(r)))
			}
			w.Load(wizard.Answers{Timezone: "UTC"})
			Expect(w.Value()).To(Equal("UTC"))
			Expect(w.View(true, 76)).To(ContainSubstring("Asia/Tokyo"))
		})

		It("types into the list again after deleting the last entry", func() {
			w := widgetFor[wizard.KindList](wizard.Field{ID: wizard.FieldSSHKeys, Kind: wizard.KindList})
			w.Load(wizard.Answers{SSHKeys: []string{"github:a", "github:b"}})
			w.Focus()
			w.Update(key("up"))
			w.Update(tea.KeyMsg{Type: tea.KeyDelete})
			Expect(w.Value()).To(Equal("github:a"))
			for _, r := range "github:c" {
				w.Update(key(string(r)))
			}
			w.Update(key("enter"))
			Expect(w.Value()).To(Equal("github:a\ngithub:c"))
		})

		It("sends forward delete on the input row to the input", func() {
			w := widgetFor[wizard.KindList](wizard.Field{ID: wizard.FieldSSHKeys, Kind: wizard.KindList})
			w.Load(wizard.Answers{SSHKeys: []string{"github:a"}})
			w.Focus()
			for _, r := range "xy" {
				w.Update(key(string(r)))
			}
			w.Update(tea.KeyMsg{Type: tea.KeyLeft})
			w.Update(tea.KeyMsg{Type: tea.KeyDelete})
			w.Update(key("enter"))
			Expect(w.Value()).To(Equal("github:a\nx"))
		})

		It("moves to the next field on enter, and leaves optional choices unset", func() {
			steps := []wizard.Step{{ID: wizard.StepLocale, Optional: true, Fields: []wizard.Field{
				{ID: wizard.FieldTimezone, Kind: wizard.KindChoice, Label: "Timezone", Choices: zones},
				{ID: wizard.FieldKeymap, Kind: wizard.KindChoice, Label: "Keymap", Choices: []wizard.Choice{{Value: "us", Label: "us"}, {Value: "it", Label: "it"}}}}}}
			mainModel.steps = steps
			mainModel.answers = wizard.Answers{Timezone: "UTC", Keymap: "it"}
			p := newStepPage(steps[0])
			mainModel.answers = wizard.Answers{}
			p.Init()
			Expect(p.View()).To(ContainSubstring("(leave unset)"))
			_, cmd := p.Update(key("enter"))
			Expect(cmd).To(BeNil())
			Expect(p.focus).To(Equal(1))
			_, cmd = p.Update(key("enter"))
			Expect(cmd).ToNot(BeNil())
			Expect(cmd()).To(Equal(GoToPageMsg{PageID: "customization"}))
			Expect(mainModel.answers.Timezone).To(BeEmpty())
			Expect(mainModel.answers.Keymap).To(BeEmpty())
		})

		It("picks an optional choice below the unset row", func() {
			steps := []wizard.Step{{ID: wizard.StepLocale, Optional: true, Fields: []wizard.Field{
				{ID: wizard.FieldTimezone, Kind: wizard.KindChoice, Choices: zones},
				{ID: wizard.FieldKeymap, Kind: wizard.KindChoice, Choices: []wizard.Choice{{Value: "us", Label: "us"}}}}}}
			mainModel.steps = steps
			p := newStepPage(steps[0])
			p.Init()
			p.Update(key("down"))
			p.Update(key("enter"))
			p.Update(key("enter"))
			Expect(mainModel.answers.Timezone).To(Equal("Asia/Tokyo"))
			Expect(mainModel.answers.Keymap).To(BeEmpty())
		})
	})
})
