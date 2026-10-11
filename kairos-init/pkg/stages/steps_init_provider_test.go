package stages

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// writeFakeInfoProvider drops a binary that speaks the go-pluggable protocol:
// it is run with the event name as argv[1], reads the event from stdin and
// writes an EventResponse to stdout. body is the raw EventResponse JSON it
// answers the info probe with.
func writeFakeInfoProvider(t *testing.T, dir, name, body string) {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\ncat > /dev/null\ncat <<'RESP'\n%s\nRESP\n", body)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// infoResponse builds the EventResponse a healthy provider sends back. data is
// embedded as a JSON string, which is how pluggable carries the payload.
func infoResponse(data string) string {
	return fmt.Sprintf(`{"state":"success","data":%q,"error":""}`, data)
}

// setConfiguredProviders installs a provider list for the duration of the
// test. The config is a package global, so it has to be restored.
func setConfiguredProviders(t *testing.T, names ...string) {
	t.Helper()
	previous := config.DefaultConfig.Providers
	t.Cleanup(func() { config.DefaultConfig.Providers = previous })
	config.DefaultConfig.Providers = nil
	for _, n := range names {
		config.DefaultConfig.Providers = append(config.DefaultConfig.Providers, config.Provider{Name: n})
	}
}

// TestGetProviderInfoPicksTheConfiguredProvider covers an image that carries
// more than one provider plugin.
//
// The bus broadcasts, so one Publish of init.provider.info reaches every
// registered plugin and the response handler runs once per plugin. Decoding
// every one of those responses into a single shared struct means the provider
// written into /etc/kairos-release is whichever plugin the bus happened to
// enumerate last, and json.Unmarshal into an already populated struct only
// overwrites the keys a response actually carries, so a provider that answers
// without a version inherits the previous provider's version.
//
// The single-plugin cases are the ones that happen to balance, so they are
// pinned here too to keep the fix honest.
func TestGetProviderInfoPicksTheConfiguredProvider(t *testing.T) {
	const (
		alphaFull   = `{"provider":"alpha","version":"v1.0.0"}`
		betaFull    = `{"provider":"beta","version":"v2.0.0"}`
		betaNoVer   = `{"provider":"beta"}`
		notThisOne  = `{"state":"non-applicable","data":"","error":""}`
		brokenReply = `{"state":"success","data":"{not json","error":""}`
	)

	for _, tc := range []struct {
		name        string
		plugins     map[string]string
		providers   []string
		wantName    string
		wantVersion string
	}{
		{
			name:        "one plugin answers for the one configured provider",
			plugins:     map[string]string{"agent-provider-alpha": infoResponse(alphaFull)},
			providers:   []string{"alpha"},
			wantName:    "alpha",
			wantVersion: "v1.0.0",
		},
		{
			name: "the first configured provider wins, not the last to answer",
			plugins: map[string]string{
				"agent-provider-alpha": infoResponse(alphaFull),
				"agent-provider-beta":  infoResponse(betaFull),
			},
			providers:   []string{"alpha", "beta"},
			wantName:    "alpha",
			wantVersion: "v1.0.0",
		},
		{
			name: "the configured order decides, not the plugin file name",
			plugins: map[string]string{
				"agent-provider-alpha": infoResponse(alphaFull),
				"agent-provider-beta":  infoResponse(betaFull),
			},
			providers:   []string{"beta", "alpha"},
			wantName:    "beta",
			wantVersion: "v2.0.0",
		},
		{
			name: "a provider that reports no version does not inherit another one",
			plugins: map[string]string{
				"agent-provider-alpha": infoResponse(alphaFull),
				"agent-provider-beta":  infoResponse(betaNoVer),
			},
			providers: []string{"beta", "alpha"},
			// beta is the configured provider and it reports no version, so
			// there is no software version to write. It must not be handed
			// alpha's.
			wantName:    "beta",
			wantVersion: "",
		},
		{
			name: "a plugin that does not handle the event does not clear the answer",
			plugins: map[string]string{
				"agent-provider-alpha": infoResponse(alphaFull),
				"agent-provider-beta":  notThisOne,
			},
			providers:   []string{"alpha"},
			wantName:    "alpha",
			wantVersion: "v1.0.0",
		},
		{
			name: "a plugin that answers with junk does not corrupt the answer",
			plugins: map[string]string{
				"agent-provider-alpha": infoResponse(alphaFull),
				"agent-provider-beta":  brokenReply,
			},
			providers:   []string{"alpha"},
			wantName:    "alpha",
			wantVersion: "v1.0.0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tc.plugins {
				writeFakeInfoProvider(t, dir, name, body)
			}
			// Autoload scans the working directory, so this is what puts the
			// fake plugins on the bus.
			t.Chdir(dir)
			setConfiguredProviders(t, tc.providers...)

			got, err := getProviderInfo(logger.NewKairosLogger("test", "fatal", false))
			if err != nil {
				t.Fatalf("getProviderInfo: %v", err)
			}
			if got.Provider != tc.wantName || got.Version != tc.wantVersion {
				t.Fatalf("got provider %q version %q, want provider %q version %q",
					got.Provider, got.Version, tc.wantName, tc.wantVersion)
			}
		})
	}
}
