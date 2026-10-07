package wizard

import (
	"context"
	"fmt"

	sdkBus "github.com/kairos-io/kairos/v4/sdk/bus"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
)

// Env is where the steps get their choices from. The production one is
// SystemEnv; tests pass a fake.
type Env interface {
	// Disks re-scans on every call, because a prerequisites plugin can change
	// the answer between two screens (#4260).
	Disks() ([]disks.Disk, error)
	// Extensions returns what the live media carries and the catalogs
	// publish. A non-nil error means no catalog could be read; the returned
	// choices are still valid, because an airgapped install has to work.
	Extensions(ctx context.Context) ([]Choice, error)
	Timezones() []string
	Keymaps() []string
	ProviderPrompts() []sdkBus.YAMLPrompt
	// AdvancedDisabled is the branding switch that hides everything but the
	// disk and the finish action.
	AdvancedDisabled() bool
}

// Steps returns the installer's questions, in order.
func Steps(ctx context.Context, env Env) []Step {
	steps := []Step{diskStep(env)}
	if !env.AdvancedDisabled() {
		steps = append(steps, userStep(), sshKeysStep(), hostnameStep(), localeStep(env), ExtensionsStep(ctx, env))
		if p, ok := providerStep(env); ok {
			steps = append(steps, p)
		}
	}
	return append(steps, finishStep())
}

// StepByID finds one step.
func StepByID(steps []Step, id string) (Step, bool) {
	for _, s := range steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

func diskStep(env Env) Step {
	s := Step{
		ID: StepDisk, Title: "Disk",
		Help: "Pick the disk to install to. Everything on it is erased.",
	}
	found, err := env.Disks()
	var choices []Choice
	for _, d := range found {
		detail := d.Size
		if d.Model != "" {
			detail += ", " + d.Model
		}
		choices = append(choices, Choice{Value: d.Path, Label: d.Path, Detail: detail})
	}
	switch {
	case err != nil:
		s.Notice = fmt.Sprintf("The disks could not be listed: %v", err)
	case len(choices) == 0:
		s.Notice = fmt.Sprintf("No disk of at least %s was found.", disks.HumanSize(disks.MinSizeBytes))
	}
	s.Fields = []Field{{
		ID: FieldDisk, Kind: KindChoice, Label: "Target disk", Required: true, Choices: choices,
		Confirm: "Everything on {value} will be erased.",
	}}
	return s
}

func userStep() Step {
	return Step{
		ID: StepUser, Title: "User and password", Optional: true,
		Help: "The account you log in with. Leave it empty to install with no users.",
		Fields: []Field{
			{ID: FieldUsername, Kind: KindText, Label: "Username", Placeholder: "kairos"},
			{ID: FieldPassword, Kind: KindPassword, Label: "Password", Help: "Hashed by the installer before it goes into the configuration."},
		},
	}
}

func sshKeysStep() Step {
	return Step{
		ID: StepSSHKeys, Title: "SSH keys", Optional: true,
		Help:   "Public keys allowed to log in as the user above.",
		Fields: []Field{{ID: FieldSSHKeys, Kind: KindList, Label: "Keys", Placeholder: "github:USERNAME, gitlab:USERNAME or ssh-ed25519 AAAA..."}},
	}
}

func hostnameStep() Step {
	return Step{
		ID: StepHostname, Title: "Hostname", Optional: true,
		Help: "Leave it empty to keep the generated name.",
		Fields: []Field{{ID: FieldHostname, Kind: KindText, Label: "Hostname", Placeholder: "kairos-edge-01",
			Help: "Letters, digits and hyphens, dot-separated, up to 63 characters per part."}},
	}
}

func localeStep(env Env) Step {
	s := Step{
		ID: StepLocale, Title: "Timezone and keyboard", Optional: true,
		Help: "Both apply from the first boot of the installed system.",
	}
	// The timezone is applied by linking /etc/localtime at a zoneinfo file,
	// so an image that ships no time zone database cannot honour any answer:
	// the link would dangle and the system would stay on UTC while the
	// summary claimed otherwise. Offer the field only when the database is
	// there, and say why when it is not.
	if zones := env.Timezones(); len(zones) > 0 {
		s.Fields = append(s.Fields, choiceOf(FieldTimezone, "Timezone", zones))
	} else {
		s.Title = "Keyboard"
		s.Help = "It applies from the first boot of the installed system."
		s.Notice = "This image ships no time zone database, so the system runs on UTC and the timezone cannot be set here."
	}
	// A keymap is written to a file that is inert when nothing reads it, so
	// free text stays useful on an image whose keymaps this installer cannot
	// enumerate.
	s.Fields = append(s.Fields, choiceOrText(FieldKeymap, "Keyboard layout", "us", env.Keymaps()))
	return s
}

// choiceOf offers values as a list.
func choiceOf(id, label string, values []string) Field {
	f := Field{ID: id, Kind: KindChoice, Label: label}
	for _, v := range values {
		f.Choices = append(f.Choices, Choice{Value: v, Label: v})
	}
	return f
}

// choiceOrText offers values as a list when the image has them, and as free
// text when it does not, so an image whose keymaps cannot be enumerated can
// still take one.
func choiceOrText(id, label, placeholder string, values []string) Field {
	if len(values) == 0 {
		return Field{ID: id, Kind: KindText, Label: label, Placeholder: placeholder}
	}
	return choiceOf(id, label, values)
}

// ExtensionsStep builds the extensions step on its own, so a frontend can
// fetch the catalog when the step is shown rather than before anything is.
func ExtensionsStep(ctx context.Context, env Env) Step {
	s := Step{
		ID: StepExtensions, Title: "System extensions", Optional: true,
		Help: "Merged into the system on the first boot after install.",
	}
	choices, err := env.Extensions(ctx)
	if err != nil {
		s.Notice = "No catalog could be read, offering only what the live media carries."
	}
	if len(choices) == 0 && err == nil {
		s.Notice = "No extension was found on the live media or in the catalog."
	}
	s.Fields = []Field{{ID: FieldExtensions, Kind: KindMultiChoice, Label: "Extensions", Choices: choices}}
	return s
}

func providerStep(env Env) (Step, bool) {
	prompts := env.ProviderPrompts()
	if len(prompts) == 0 {
		return Step{}, false
	}
	s := Step{ID: StepProvider, Title: "Provider settings", Optional: true, Help: "Asked by the installed provider plugin."}
	seen := map[string]bool{}
	for _, p := range prompts {
		if p.YAMLSection == "" || seen[p.YAMLSection] {
			continue
		}
		seen[p.YAMLSection] = true
		f := Field{ID: p.YAMLSection, Label: p.Prompt, Placeholder: p.PlaceHolder, Default: p.Default, IfEmpty: p.IfEmpty, Kind: KindText}
		if f.Label == "" {
			f.Label = p.YAMLSection
		}
		if p.Bool {
			f.Kind = KindBool
		}
		// A prompt the plugin asks only after a yes gets that yes or no as a
		// field of its own, in front of it. Apply writes the section, and
		// its IfEmpty, only when the answer is yes.
		if p.AskFirst {
			label := p.AskPrompt
			if label == "" {
				label = "Set " + p.YAMLSection + "?"
			}
			s.Fields = append(s.Fields, Field{ID: p.YAMLSection + AskSuffix, Kind: KindBool, Label: label, Default: "false"})
			f.Help = "Used only when the answer above is yes."
		}
		s.Fields = append(s.Fields, f)
	}
	if len(s.Fields) == 0 {
		return Step{}, false
	}
	return s, true
}

func finishStep() Step {
	return Step{
		ID: StepFinish, Title: "When the install finishes",
		Fields: []Field{{ID: FieldFinish, Kind: KindChoice, Label: "Action", Default: "", Choices: []Choice{
			{Value: "", Label: "Nothing", Detail: "stay on the live system"},
			{Value: FinishReboot, Label: "Reboot", Detail: "boot into the installed system"},
			{Value: FinishPoweroff, Label: "Power off", Detail: "shut down"},
		}}},
	}
}
