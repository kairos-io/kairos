package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// There is no other off-switch on a real boot: kairos-agent execs the
// installer with a fixed argument list, so an operator only ever reaches this
// through the config. The key an operator writes has to be the key that is
// read, so this goes through a real agent.yaml rather than the struct.
func TestEnabledFromAnAgentConfigFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{name: "disable", body: "mcp:\n  disable: true\n", want: false},
		{name: "disable written as false", body: "mcp:\n  disable: false\n", want: true},
		{name: "an mcp block that says nothing", body: "mcp: {}\n", want: true},
		{name: "a config about something else entirely", body: "webui:\n  disable: true\n", want: true},
		// The whole point of sharing the web UI's listener: an operator who
		// turned the unauthenticated network installer off has turned this off
		// too, because there is no server left to hang the route on. So this
		// resolving to "enabled" is correct and costs that machine nothing.
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}

			if got := EnabledFromConfig(path); got != tc.want {
				t.Errorf("enabled = %v, want %v for:\n%s", got, tc.want, tc.body)
			}
		})
	}
}

// An unbranded live image has no agent.yaml at all, which is the normal case
// and must not be read as "do not serve".
func TestEnabledWithNoConfigFileAtAll(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "there-is-no-agent.yaml")

	if !EnabledFromConfig(missing) {
		t.Error("a missing agent.yaml switched MCP off, so an unbranded image loses it")
	}
}
