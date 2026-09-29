package tui

import (
	"context"
	"os"
	"testing"

	sdkBus "github.com/kairos-io/kairos/v4/sdk/bus"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// fakeWizardEnv answers the wizard's questions without touching the machine:
// no disk scan, no branding directory, no catalog fetch, no provider bus.
type fakeWizardEnv struct {
	// disks is returned by Disks, one entry per call; the last entry repeats.
	disks      [][]disks.Disk
	diskErr    error
	diskCalls  *int
	exts       []wizard.Choice
	zones      []string
	keymaps    []string
	prompts    []sdkBus.YAMLPrompt
	noAdvanced bool
}

func newFakeWizardEnv() fakeWizardEnv {
	return fakeWizardEnv{
		disks:     [][]disks.Disk{{{Path: "/dev/vda", Size: "40.00 GiB"}, {Path: "/dev/vdb", Size: "80.00 GiB"}}},
		diskCalls: new(int),
		exts:      []wizard.Choice{{Value: "tailscale", Label: "tailscale"}, {Value: "/run/initramfs/live/tools.sysext.raw", Label: "tools"}},
		zones:     []string{"UTC", "Europe/Rome"},
		keymaps:   []string{"it", "us"},
	}
}

func (f fakeWizardEnv) Disks() ([]disks.Disk, error) {
	i := *f.diskCalls
	*f.diskCalls++
	if f.diskErr != nil && i > 0 {
		return nil, f.diskErr
	}
	if len(f.disks) == 0 {
		return nil, nil
	}
	if i >= len(f.disks) {
		i = len(f.disks) - 1
	}
	return f.disks[i], nil
}
func (f fakeWizardEnv) Extensions(context.Context) ([]wizard.Choice, error) { return f.exts, nil }
func (f fakeWizardEnv) Timezones() []string                                 { return f.zones }
func (f fakeWizardEnv) Keymaps() []string                                   { return f.keymaps }
func (f fakeWizardEnv) ProviderPrompts() []sdkBus.YAMLPrompt                { return f.prompts }
func (f fakeWizardEnv) AdvancedDisabled() bool                              { return f.noAdvanced }

// useFakeWizardEnv installs env for the rest of the test.
func useFakeWizardEnv(env wizard.Env) {
	old := wizardEnv
	wizardEnv = env
	DeferCleanup(func() { wizardEnv = old })
}

// TestMain keeps the whole package offline, including the plain testing.T
// tests that no ginkgo BeforeEach reaches: the fake env stands in for the
// disk scan, the advanced-disabled branding switch, the extension catalog and
// the provider bus.
func TestMain(m *testing.M) {
	wizardEnv = newFakeWizardEnv()
	os.Exit(m.Run())
}

var _ = BeforeEach(func() {
	useFakeWizardEnv(newFakeWizardEnv())
})

func TestTUI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TUI Suite")
}
