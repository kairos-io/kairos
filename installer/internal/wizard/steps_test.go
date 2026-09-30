package wizard_test

import (
	"context"
	"errors"

	sdkBus "github.com/kairos-io/kairos/v4/sdk/bus"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

type fakeEnv struct {
	disks      []disks.Disk
	diskErr    error
	exts       []wizard.Choice
	extErr     error
	zones      []string
	keymaps    []string
	prompts    []sdkBus.YAMLPrompt
	noAdvanced bool
}

func (f fakeEnv) Disks() ([]disks.Disk, error)                        { return f.disks, f.diskErr }
func (f fakeEnv) Extensions(context.Context) ([]wizard.Choice, error) { return f.exts, f.extErr }
func (f fakeEnv) Timezones() []string                                 { return f.zones }
func (f fakeEnv) Keymaps() []string                                   { return f.keymaps }
func (f fakeEnv) ProviderPrompts() []sdkBus.YAMLPrompt                { return f.prompts }
func (f fakeEnv) AdvancedDisabled() bool                              { return f.noAdvanced }

func ids(steps []wizard.Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}

var _ = Describe("Steps", func() {
	env := fakeEnv{
		disks:   []disks.Disk{{Path: "/dev/vda", Size: "40.00 GiB", Model: "QEMU HARDDISK"}},
		exts:    []wizard.Choice{{Value: "tailscale", Label: "tailscale", Detail: "quay.io/kairos/extensions"}},
		zones:   []string{"Europe/Rome", "UTC"},
		keymaps: []string{"it", "us"},
	}

	It("returns the fixed steps in order, without provider when no plugin answers", func() {
		Expect(ids(wizard.Steps(context.Background(), env))).To(Equal([]string{
			wizard.StepDisk, wizard.StepUser, wizard.StepSSHKeys, wizard.StepHostname,
			wizard.StepLocale, wizard.StepExtensions, wizard.StepFinish,
		}))
	})

	It("offers the scanned disks with size and model, and a confirmation", func() {
		s, _ := wizard.StepByID(wizard.Steps(context.Background(), env), wizard.StepDisk)
		f := s.Fields[0]
		Expect(f.Kind).To(Equal(wizard.KindChoice))
		Expect(f.Required).To(BeTrue())
		Expect(f.Choices).To(Equal([]wizard.Choice{{Value: "/dev/vda", Label: "/dev/vda", Detail: "40.00 GiB, QEMU HARDDISK"}}))
		Expect(f.Confirm).To(ContainSubstring("{value}"))
	})

	It("says so when no disk was found", func() {
		s, _ := wizard.StepByID(wizard.Steps(context.Background(), fakeEnv{}), wizard.StepDisk)
		Expect(s.Notice).To(ContainSubstring("No disk"))
		Expect(s.Fields[0].Choices).To(BeEmpty())
	})

	It("adds a provider step with one field per prompt", func() {
		e := env
		e.prompts = []sdkBus.YAMLPrompt{
			{YAMLSection: "k3s.enabled", Bool: true, Prompt: "Enable k3s"},
			{YAMLSection: "k3s.token", Prompt: "Cluster token", PlaceHolder: "token", IfEmpty: "generated"},
		}
		steps := wizard.Steps(context.Background(), e)
		Expect(ids(steps)).To(ContainElement(wizard.StepProvider))
		s, _ := wizard.StepByID(steps, wizard.StepProvider)
		Expect(s.Fields).To(HaveLen(2))
		Expect(s.Fields[0]).To(MatchFields(IgnoreExtras, Fields{"ID": Equal("k3s.enabled"), "Kind": Equal(wizard.KindBool)}))
		Expect(s.Fields[1]).To(MatchFields(IgnoreExtras, Fields{"Kind": Equal(wizard.KindText), "IfEmpty": Equal("generated"), "Placeholder": Equal("token")}))
	})

	It("asks an AskFirst prompt as a gate before its value, and titles the step Provider settings", func() {
		e := env
		e.prompts = []sdkBus.YAMLPrompt{
			{YAMLSection: "p2p.network_token", Prompt: "Insert a network token, leave empty to autogenerate",
				AskFirst: true, AskPrompt: "Do you want to setup a full mesh-support?", IfEmpty: "generated"},
			{YAMLSection: "k3s.enabled", Bool: true, Prompt: "Do you want to enable k3s?"},
		}
		s, _ := wizard.StepByID(wizard.Steps(context.Background(), e), wizard.StepProvider)
		Expect(s.Title).To(Equal("Provider settings"))
		Expect(s.Fields).To(HaveLen(3))
		Expect(s.Fields[0]).To(MatchFields(IgnoreExtras, Fields{
			"ID": Equal("p2p.network_token" + wizard.AskSuffix), "Kind": Equal(wizard.KindBool),
			"Label": Equal("Do you want to setup a full mesh-support?"), "Default": Equal("false"),
		}))
		Expect(s.Fields[1]).To(MatchFields(IgnoreExtras, Fields{"ID": Equal("p2p.network_token"), "Kind": Equal(wizard.KindText), "Help": Not(BeEmpty())}))
		Expect(s.Fields[2]).To(MatchFields(IgnoreExtras, Fields{"ID": Equal("k3s.enabled"), "Kind": Equal(wizard.KindBool)}))
	})

	It("keeps only disk and finish when branding disables advanced customization", func() {
		e := env
		e.noAdvanced = true
		Expect(ids(wizard.Steps(context.Background(), e))).To(Equal([]string{wizard.StepDisk, wizard.StepFinish}))
	})

	It("drops the timezone and falls back to free text for the keymap when the image has no zoneinfo or keymaps", func() {
		s, _ := wizard.StepByID(wizard.Steps(context.Background(), fakeEnv{}), wizard.StepLocale)
		Expect(s.Fields).To(HaveLen(1))
		Expect(s.Fields[0].ID).To(Equal(wizard.FieldKeymap))
		Expect(s.Fields[0].Kind).To(Equal(wizard.KindText))
		Expect(s.Notice).To(ContainSubstring("no time zone database"))
	})

	It("keeps live media extensions and carries a notice when no catalog was read", func() {
		e := env
		e.exts = []wizard.Choice{{Value: "/run/initramfs/live/k3s.sysext.raw", Label: "k3s", Detail: "live media"}}
		e.extErr = errors.New("no extension catalog could be read")
		s, _ := wizard.StepByID(wizard.Steps(context.Background(), e), wizard.StepExtensions)
		Expect(s.Fields[0].Choices).To(HaveLen(1))
		Expect(s.Notice).To(ContainSubstring("No catalog could be read"))
	})

	It("offers nothing, reboot and power off, defaulting to nothing", func() {
		s, _ := wizard.StepByID(wizard.Steps(context.Background(), env), wizard.StepFinish)
		var values []string
		for _, c := range s.Fields[0].Choices {
			values = append(values, c.Value)
		}
		Expect(values).To(Equal([]string{"", wizard.FinishReboot, wizard.FinishPoweroff}))
		Expect(s.Fields[0].Default).To(Equal(""))
	})
})
