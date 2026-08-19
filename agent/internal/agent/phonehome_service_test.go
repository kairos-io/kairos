package agent

import (
	"strings"
	"testing"

	"github.com/kairos-io/kairos-sdk/constants"
)

func TestPhoneHomeServiceUnit(t *testing.T) {
	unit := phoneHomeServiceUnit(constants.AgentDefaultPath)

	if !strings.Contains(unit, "ExecStart="+constants.AgentDefaultPath+" phone-home") {
		t.Fatalf("ExecStart should use AgentDefaultPath, got:\n%s", unit)
	}
	if strings.Contains(unit, "/usr/sbin/kairos-agent") {
		t.Fatalf("unit should not hardcode /usr/sbin/kairos-agent, got:\n%s", unit)
	}
}
