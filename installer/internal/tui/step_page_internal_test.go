package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"gopkg.in/yaml.v3"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
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

	// Ported from the disk selection page (#4260): a prerequisites plugin
	// such as wipefs can change the disks between startup and the moment
	// the operator reaches the disk step.
	Describe("the disk step", func() {
		sda := disks.Disk{Path: "/dev/sda", Size: "10.00 GiB"}
		sdb := disks.Disk{Path: "/dev/sdb", Size: "20.00 GiB"}
		sdc := disks.Disk{Path: "/dev/sdc", Size: "30.00 GiB"}
		nvme := disks.Disk{Path: "/dev/nvme0n1", Size: "500.00 GiB"}

		diskPage := func(env fakeWizardEnv) *stepPage {
			useFakeWizardEnv(env)
			mainModel.steps = wizard.Steps(context.Background(), env)
			s, ok := wizard.StepByID(mainModel.steps, wizard.StepDisk)
			Expect(ok).To(BeTrue())
			return newStepPage(s)
		}
		offered := func(p *stepPage) []string {
			var out []string
			for _, c := range p.widgets[0].(*choiceWidget).options {
				out = append(out, c.Value)
			}
			return out
		}

		It("re-scans on every Init and applies against the fresh list", func() {
			env := newFakeWizardEnv()
			env.disks = [][]disks.Disk{{sda, sdb, sdc}, {sda, sdc}}
			p := diskPage(env)
			Expect(offered(p)).To(Equal([]string{"/dev/sda", "/dev/sdb", "/dev/sdc"}))
			Expect(*env.diskCalls).To(Equal(1))

			Expect(p.Init()).To(BeNil())
			Expect(*env.diskCalls).To(Equal(2))
			Expect(offered(p)).To(Equal([]string{"/dev/sda", "/dev/sdc"}))
			Expect(p.View()).ToNot(ContainSubstring("/dev/sdb"))

			// The model's copy of the step is what Apply checks against.
			_, errs := wizard.Apply(mainModel.steps, mainModel.answers, wizard.StepDisk, map[string]string{wizard.FieldDisk: "/dev/sdb"})
			Expect(errs).ToNot(BeEmpty())
			p.Update(key("down"))
			_, cmd := p.Update(key("enter"))
			Expect(cmd).ToNot(BeNil())
			Expect(mainModel.answers.Disk).To(Equal("/dev/sdc"))
		})

		It("picks up a disk that appeared since the last scan", func() {
			env := newFakeWizardEnv()
			env.disks = [][]disks.Disk{{sda}, {sda, nvme}}
			p := diskPage(env)
			p.Init()
			Expect(offered(p)).To(Equal([]string{"/dev/sda", "/dev/nvme0n1"}))
		})

		It("keeps the previous list when a re-scan fails", func() {
			env := newFakeWizardEnv()
			env.disks = [][]disks.Disk{{sda, sdb}}
			env.diskErr = errors.New("simulated ghw failure")
			p := diskPage(env)
			p.Init()
			Expect(offered(p)).To(Equal([]string{"/dev/sda", "/dev/sdb"}))
		})

		It("keeps a long disk list inside an 80x24 console", func() {
			var many []disks.Disk
			for i := 0; i < 30; i++ {
				many = append(many, disks.Disk{Path: fmt.Sprintf("/dev/vd%02d", i), Size: "10.00 GiB"})
			}
			env := newFakeWizardEnv()
			env.disks = [][]disks.Disk{many}
			p := diskPage(env)
			p.Init()
			for i := 0; i < 15; i++ {
				p.Update(key("down"))
			}
			budget := defaultTermHeight - 8 - 2
			view := p.View()
			Expect(len(splitLines(strings.TrimRight(view, "\n")))).To(BeNumerically("<=", budget))
			Expect(view).To(ContainSubstring("more above"))
			Expect(view).To(ContainSubstring("more below"))
			for i := 0; i < 30; i++ {
				p.Update(key("down"))
			}
			Expect(p.View()).To(ContainSubstring("/dev/vd29"))
		})

		It("advertises the debug-log hotkey", func() {
			p := diskPage(newFakeWizardEnv())
			p.Init()
			Expect(p.Help()).To(ContainSubstring("ctrl+d"))
		})
	})

	// Ported from the extensions page.
	Describe("the extensions step", func() {
		extPage := func(env fakeWizardEnv) *stepPage {
			useFakeWizardEnv(env)
			mainModel.steps = wizard.Steps(context.Background(), env)
			s, ok := wizard.StepByID(mainModel.steps, wizard.StepExtensions)
			Expect(ok).To(BeTrue())
			p := newStepPage(s)
			p.Init()
			return p
		}

		It("writes the selection into install.extensions of the rendered config", func() {
			live := filepath.Join("/run/initramfs/live", "tools.sysext.raw")
			env := newFakeWizardEnv()
			env.exts = []wizard.Choice{{Value: "nvidia", Label: "nvidia"}, {Value: "tailscale", Label: "tailscale"}, {Value: live, Label: "tools"}}
			p := extPage(env)
			p.Update(key("down"))
			p.Update(key("space"))
			p.Update(key("down"))
			p.Update(key("space"))
			_, cmd := p.Update(key("enter"))
			Expect(cmd).ToNot(BeNil())
			Expect(cmd()).To(Equal(GoToPageMsg{PageID: "customization"}))

			mainModel.answers.Disk = "/dev/sda"
			out, err := currentCloudConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("extensions:"))
			Expect(out).To(ContainSubstring("- tailscale\n"))
			Expect(out).To(ContainSubstring("- " + live + "\n"))
			Expect(out).ToNot(ContainSubstring("nvidia"))

			// The agent reads the file back as a Config.
			var decoded sdkConfig.Config
			Expect(yaml.Unmarshal([]byte(out), &decoded)).To(Succeed())
			Expect(decoded.Install).ToNot(BeNil())
			Expect(decoded.Install.Extensions).To(Equal(mainModel.answers.Extensions))
		})

		It("leaves install.extensions out when nothing is picked", func() {
			p := extPage(newFakeWizardEnv())
			p.Update(key("enter"))
			mainModel.answers.Disk = "/dev/sda"
			out, err := currentCloudConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(out).ToNot(ContainSubstring("extensions:"))
		})

		It("fits the body an 80x24 console leaves for a page", func() {
			env := newFakeWizardEnv()
			env.exts = nil
			for i := 0; i < 30; i++ {
				env.exts = append(env.exts, wizard.Choice{Value: fmt.Sprintf("layer%02d", i), Label: fmt.Sprintf("layer%02d", i)})
			}
			p := extPage(env)
			for i := 0; i < 15; i++ {
				p.Update(key("down"))
			}
			budget := defaultTermHeight - 8 - 2
			view := p.View()
			Expect(len(splitLines(strings.TrimRight(view, "\n")))).To(BeNumerically("<=", budget),
				"a body the model truncates loses rows with nothing on screen to say so")
			Expect(view).To(ContainSubstring("more above"))
			Expect(view).To(ContainSubstring("more below"))
			Expect(view).To(ContainSubstring("layer15"))
			for i := 0; i < 30; i++ {
				p.Update(key("down"))
			}
			Expect(p.View()).To(ContainSubstring("layer29"))
		})

		It("is on the customization menu and ticks once something is picked", func() {
			l := sdkLogger.NewKairosLogger("test", "error", false)
			mainModel = InitialModel(&l, "")
			c := newCustomizationPage()
			c.Init()
			Expect(c.ids).To(ContainElement(wizard.StepExtensions))
			Expect(c.isConfigured(wizard.StepExtensions)).To(BeFalse())
			mainModel.answers.Extensions = nil
			a, errs := wizard.Apply(mainModel.steps, mainModel.answers, wizard.StepExtensions, map[string]string{wizard.FieldExtensions: "tailscale"})
			Expect(errs).To(BeEmpty())
			mainModel.answers = a
			Expect(c.isConfigured(wizard.StepExtensions)).To(BeTrue())
		})
	})

	It("keeps the visible window on the cursor without scrolling past either end", func() {
		first, last := visibleWindow(0, 20, 8)
		Expect([]int{first, last}).To(Equal([]int{0, 8}))
		first, last = visibleWindow(10, 20, 8)
		Expect(first).To(BeNumerically("<=", 10))
		Expect(last).To(BeNumerically(">", 10))
		first, last = visibleWindow(19, 20, 8)
		Expect([]int{first, last}).To(Equal([]int{12, 20}))
		first, last = visibleWindow(2, 3, 8)
		Expect([]int{first, last}).To(Equal([]int{0, 3}))
	})
})
