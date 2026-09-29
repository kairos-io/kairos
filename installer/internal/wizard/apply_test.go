package wizard

import (
	"context"

	sdkBus "github.com/kairos-io/kairos/v4/sdk/bus"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
)

type applyEnv struct{ prompts []sdkBus.YAMLPrompt }

func (applyEnv) Disks() ([]disks.Disk, error) {
	return []disks.Disk{{Path: "/dev/vda", Size: "40 GiB"}}, nil
}
func (applyEnv) Extensions(context.Context) ([]Choice, error) {
	return []Choice{{Value: "tailscale", Label: "tailscale"}, {Value: "/run/initramfs/live/k3s.sysext.raw", Label: "k3s"}}, nil
}
func (applyEnv) Timezones() []string                    { return []string{"UTC", "Europe/Rome"} }
func (applyEnv) Keymaps() []string                      { return []string{"it", "us"} }
func (e applyEnv) ProviderPrompts() []sdkBus.YAMLPrompt { return e.prompts }
func (applyEnv) AdvancedDisabled() bool                 { return false }

var _ = Describe("Apply", func() {
	var steps []Step
	BeforeEach(func() {
		old := passwordHasher
		passwordHasher = func(p string) (string, error) { return "$6$salt$" + p + "-hashed", nil }
		DeferCleanup(func() { passwordHasher = old })
		steps = Steps(context.Background(), applyEnv{prompts: []sdkBus.YAMLPrompt{
			{YAMLSection: "k3s.enabled", Bool: true}, {YAMLSection: "k3s.token", IfEmpty: "auto-token"},
		}})
	})

	errFor := func(errs []FieldError, field string) string {
		for _, e := range errs {
			if e.Field == field {
				return e.Message
			}
		}
		return ""
	}

	It("takes a disk the scan offered and refuses one it did not", func() {
		a, errs := Apply(steps, Answers{}, StepDisk, map[string]string{FieldDisk: "/dev/vda"})
		Expect(errs).To(BeEmpty())
		Expect(a.Disk).To(Equal("/dev/vda"))
		_, errs = Apply(steps, Answers{}, StepDisk, map[string]string{FieldDisk: "/dev/sdz"})
		Expect(errFor(errs, FieldDisk)).To(ContainSubstring("not one of the disks"))
		_, errs = Apply(steps, Answers{}, StepDisk, map[string]string{})
		Expect(errFor(errs, FieldDisk)).To(ContainSubstring("Pick a disk"))
	})

	It("hashes the password and never keeps the plaintext", func() {
		a, errs := Apply(steps, Answers{}, StepUser, map[string]string{FieldUsername: "kairos", FieldPassword: "pw", FieldPassword + ConfirmSuffix: "pw"})
		Expect(errs).To(BeEmpty())
		Expect(a.Username).To(Equal("kairos"))
		Expect(a.PasswordHash).To(Equal("$6$salt$pw-hashed"))
	})

	It("rejects a confirmation that does not match", func() {
		_, errs := Apply(steps, Answers{}, StepUser, map[string]string{FieldUsername: "kairos", FieldPassword: "pw", FieldPassword + ConfirmSuffix: "px"})
		Expect(errFor(errs, FieldPassword)).To(ContainSubstring("do not match"))
	})

	It("keeps the existing hash when the step is saved again without a new password", func() {
		a, errs := Apply(steps, Answers{Username: "kairos", PasswordHash: "$6$old"}, StepUser, map[string]string{FieldUsername: "kairos"})
		Expect(errs).To(BeEmpty())
		Expect(a.PasswordHash).To(Equal("$6$old"))
	})

	It("needs a password for a new user, and clears both when the username is emptied", func() {
		_, errs := Apply(steps, Answers{}, StepUser, map[string]string{FieldUsername: "kairos"})
		Expect(errFor(errs, FieldPassword)).To(ContainSubstring("Set a password"))
		a, errs := Apply(steps, Answers{Username: "kairos", PasswordHash: "$6$old"}, StepUser, map[string]string{FieldUsername: ""})
		Expect(errs).To(BeEmpty())
		Expect(a.Username).To(BeEmpty())
		Expect(a.PasswordHash).To(BeEmpty())
	})

	It("rejects a username the system would not accept", func() {
		_, errs := Apply(steps, Answers{}, StepUser, map[string]string{FieldUsername: "Bad Name", FieldPassword: "p", FieldPassword + ConfirmSuffix: "p"})
		Expect(errFor(errs, FieldUsername)).ToNot(BeEmpty())
	})

	It("splits SSH keys on newlines and drops blank lines", func() {
		a, errs := Apply(steps, Answers{}, StepSSHKeys, map[string]string{FieldSSHKeys: "github:a\n\n  ssh-ed25519 AAAA x  \n"})
		Expect(errs).To(BeEmpty())
		Expect(a.SSHKeys).To(Equal([]string{"github:a", "ssh-ed25519 AAAA x"}))
	})

	DescribeTable("hostname",
		func(v string, ok bool) {
			a, errs := Apply(steps, Answers{}, StepHostname, map[string]string{FieldHostname: v})
			if ok {
				Expect(errs).To(BeEmpty())
				Expect(a.Hostname).To(Equal(v))
			} else {
				Expect(errFor(errs, FieldHostname)).ToNot(BeEmpty())
			}
		},
		Entry("simple", "edge-01", true),
		Entry("dotted", "edge-01.lab.local", true),
		Entry("empty is allowed", "", true),
		Entry("leading hyphen", "-edge", false),
		Entry("underscore", "edge_01", false),
		Entry("shell", "a;reboot", false),
		Entry("too long label", "a123456789012345678901234567890123456789012345678901234567890123", false),
	)

	DescribeTable("timezone and keymap never reach Render unless they are safe",
		func(tz, km string, ok bool) {
			_, errs := Apply(steps, Answers{}, StepLocale, map[string]string{FieldTimezone: tz, FieldKeymap: km})
			Expect(errs == nil).To(Equal(ok), "%v", errs)
		},
		Entry("offered values", "Europe/Rome", "it", true),
		Entry("both empty", "", "", true),
		Entry("path traversal", "../../etc/shadow", "", false),
		Entry("absolute path", "/etc/passwd", "", false),
		Entry("shell in timezone", "UTC; rm -rf /", "", false),
		Entry("quote in keymap", "", `it"`, false),
		Entry("newline in keymap", "", "it\nKEYMAP=us", false),
		Entry("not offered by the image", "Mars/Olympus", "", false),
	)

	It("keeps only offered extensions, in offer order", func() {
		a, errs := Apply(steps, Answers{}, StepExtensions, map[string]string{FieldExtensions: "/run/initramfs/live/k3s.sysext.raw\ntailscale\nbogus"})
		Expect(errFor(errs, FieldExtensions)).To(ContainSubstring("bogus"))
		a, errs = Apply(steps, Answers{}, StepExtensions, map[string]string{FieldExtensions: "/run/initramfs/live/k3s.sysext.raw\ntailscale"})
		Expect(errs).To(BeEmpty())
		Expect(a.Extensions).To(HaveLen(2))
		Expect(a.Extensions[0].Name).To(Equal("tailscale"))
	})

	It("nests provider fields by section, with booleans and IfEmpty", func() {
		a, errs := Apply(steps, Answers{}, StepProvider, map[string]string{"k3s.enabled": "true", "k3s.token": ""})
		Expect(errs).To(BeEmpty())
		Expect(a.Provider).To(Equal(map[string]any{"k3s": map[string]any{"enabled": true, "token": "auto-token"}}))
	})

	It("takes only the offered finish actions", func() {
		a, errs := Apply(steps, Answers{}, StepFinish, map[string]string{FieldFinish: "reboot"})
		Expect(errs).To(BeEmpty())
		Expect(a.FinishAction).To(Equal(FinishReboot))
		_, errs = Apply(steps, Answers{}, StepFinish, map[string]string{FieldFinish: "halt"})
		Expect(errs).ToNot(BeEmpty())
	})

	It("does not change the answers when a step is rejected", func() {
		in := Answers{Hostname: "keep"}
		out, errs := Apply(steps, in, StepHostname, map[string]string{FieldHostname: "-bad"})
		Expect(errs).ToNot(BeEmpty())
		Expect(out).To(Equal(in))
	})

	It("rejects an unknown step", func() {
		_, errs := Apply(steps, Answers{}, "nope", nil)
		Expect(errs).To(HaveLen(1))
	})
})

type emptyEnv struct{}

func (emptyEnv) Disks() ([]disks.Disk, error)                 { return nil, nil }
func (emptyEnv) Extensions(context.Context) ([]Choice, error) { return nil, nil }
func (emptyEnv) Timezones() []string                          { return nil }
func (emptyEnv) Keymaps() []string                            { return nil }
func (emptyEnv) ProviderPrompts() []sdkBus.YAMLPrompt         { return nil }
func (emptyEnv) AdvancedDisabled() bool                       { return false }

var _ = Describe("Apply with nothing on offer", func() {
	It("accepts no extension and no disk when none is offered", func() {
		steps := Steps(context.Background(), emptyEnv{})
		_, errs := Apply(steps, Answers{}, StepExtensions, map[string]string{FieldExtensions: "bogus"})
		Expect(errs).ToNot(BeEmpty())
		_, errs = Apply(steps, Answers{}, StepDisk, map[string]string{FieldDisk: "/dev/sda"})
		Expect(errs).ToNot(BeEmpty())
	})
})

var _ = Describe("Apply locale on an image without lists", func() {
	var steps []Step
	BeforeEach(func() { steps = Steps(context.Background(), emptyEnv{}) })

	It("offers text fields, so the charset checks are the only guard", func() {
		step, _ := StepByID(steps, StepLocale)
		Expect(step.Fields[0].Kind).To(Equal(KindText))
		Expect(step.Fields[1].Kind).To(Equal(KindText))
	})

	DescribeTable("timezone and keymap as free text",
		func(tz, km string, ok bool) {
			_, errs := Apply(steps, Answers{}, StepLocale, map[string]string{FieldTimezone: tz, FieldKeymap: km})
			Expect(errs == nil).To(Equal(ok), "%v", errs)
		},
		Entry("zoneinfo name with plus", "Etc/GMT+5", "", true),
		Entry("keymap with hyphen", "", "de-latin1", true),
		Entry("both plain", "Europe/Rome", "us", true),
		Entry("path traversal", "../../etc/shadow", "", false),
		Entry("dot dot inside", "Europe/../Rome", "", false),
		Entry("absolute path", "/etc/passwd", "", false),
		Entry("shell in timezone", "UTC; rm -rf /", "", false),
		Entry("quote in keymap", "", `it"`, false),
		Entry("newline in keymap", "", "it\nKEYMAP=us", false),
		Entry("space in keymap", "", "it us", false),
	)

	It("returns the answers unchanged when one locale value is bad", func() {
		in := Answers{Timezone: "UTC", Keymap: "us"}
		out, errs := Apply(steps, in, StepLocale, map[string]string{FieldTimezone: "Europe/Rome", FieldKeymap: "it;x"})
		Expect(errs).ToNot(BeEmpty())
		Expect(out).To(Equal(in))
	})
})
