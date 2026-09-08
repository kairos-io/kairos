package services

import (
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/machine"
)

// The path the openrc script sources must be the one OverrideCmd writes to. A
// literal path in the script would drift from machine.K0sEnvUnit and fail
// silently at boot, so the script carries a placeholder and nothing else.
func TestK0sOpenrcScriptsCarryNoLiteralEnvPath(t *testing.T) {
	for name, script := range map[string]string{
		"controller": K0sControllerOpenrc,
		"worker":     K0sWorkerOpenrc,
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(script, k0sEnvFilePlaceholder) {
				t.Errorf("script does not source %s, so the provider's arguments never reach k0s", k0sEnvFilePlaceholder)
			}
			if strings.Contains(script, "/etc/k0s/") || strings.Contains(script, "/etc/sysconfig/") {
				t.Error("script hardcodes an env file path instead of using the placeholder")
			}
		})
	}
}

func TestK0sOpenrcScriptRendersTheEnvFile(t *testing.T) {
	got := k0sOpenrcScript(K0sControllerOpenrc, "/etc/k0s/k0scontroller.env")

	if strings.Contains(got, k0sEnvFilePlaceholder) {
		t.Error("the placeholder survived rendering")
	}
	if !strings.Contains(got, "if [ -f /etc/k0s/k0scontroller.env ]; then . /etc/k0s/k0scontroller.env; fi") {
		t.Errorf("rendered script does not source the env file:\n%s", got)
	}
}

// K0sServices renders the scripts with machine.K0sEnvUnit, so whatever that
// helper returns has to end up in the script verbatim.
func TestK0sOpenrcScriptTracksK0sEnvUnit(t *testing.T) {
	for _, unit := range []string{"k0scontroller", "k0sworker"} {
		envFile := machine.K0sEnvUnit(unit)
		got := k0sOpenrcScript(K0sControllerOpenrc, envFile)

		if !strings.Contains(got, "[ -f "+envFile+" ]") {
			t.Errorf("%s: rendered script does not source %q", unit, envFile)
		}
	}
}
