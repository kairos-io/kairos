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

// writeDispatchingInfoProvider drops a plugin that answers according to the
// provider the event asks about, which is what a real plugin that serves more
// than one provider does. answers maps a provider name to the EventResponse
// the plugin sends when that provider is the one named; the empty key is the
// answer to an event that names no provider. Anything the plugin was not
// given an answer for gets non-applicable.
func writeDispatchingInfoProvider(t *testing.T, dir, name string, answers map[string]string) {
	t.Helper()
	script := "#!/bin/sh\nevent=$(cat)\n"
	for provider, body := range answers {
		if provider == "" {
			continue
		}
		// The event carries the payload as an escaped JSON string, so the
		// provider name appears in it as \"provider\":\"name\".
		script += fmt.Sprintf("case \"$event\" in\n*'\\\"provider\\\":\\\"%s\\\"'*)\ncat <<'RESP'\n%s\nRESP\nexit 0\n;;\nesac\n", provider, body)
	}
	fallback, ok := answers[""]
	if !ok {
		fallback = `{"state":"non-applicable","data":"","error":""}`
	}
	script += fmt.Sprintf("cat <<'RESP'\n%s\nRESP\n", fallback)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// TestGetProviderInfoAsksAboutEveryConfiguredProvider covers the case the
// response map alone cannot fix: one plugin serving both k3s and k0s.
//
// A broadcast brings back one response per plugin, so a single untargeted
// event lets such a plugin report only one of the providers installed, and the
// other one is lost no matter how carefully the answers are kept apart. The
// install event is already published once per configured provider. The info
// event has to be too, naming the provider, which is the only thing that lets
// the plugin answer about each of them in turn.
func TestGetProviderInfoAsksAboutEveryConfiguredProvider(t *testing.T) {
	const (
		alphaFull = `{"provider":"alpha","version":"v1.0.0"}`
		betaFull  = `{"provider":"beta","version":"v2.0.0"}`
	)

	// One plugin, both providers installed, and it answers only about the
	// provider it is asked about.
	servesBoth := map[string]string{
		"alpha": infoResponse(alphaFull),
		"beta":  infoResponse(betaFull),
	}

	for _, tc := range []struct {
		name        string
		answers     map[string]string
		providers   []string
		wantName    string
		wantVersion string
	}{
		{
			name:        "the shared plugin is asked about the configured provider",
			answers:     servesBoth,
			providers:   []string{"alpha"},
			wantName:    "alpha",
			wantVersion: "v1.0.0",
		},
		{
			name:        "the other provider is reachable on the same plugin",
			answers:     servesBoth,
			providers:   []string{"beta"},
			wantName:    "beta",
			wantVersion: "v2.0.0",
		},
		{
			name:        "with both configured the first one still decides",
			answers:     servesBoth,
			providers:   []string{"beta", "alpha"},
			wantName:    "beta",
			wantVersion: "v2.0.0",
		},
		{
			name: "a build that configured no provider still asks once",
			// The plugin answers the request that names no provider, which is
			// how an image reports a provider binary it inherited rather than
			// installed in this build.
			answers:     map[string]string{"": infoResponse(alphaFull)},
			providers:   nil,
			wantName:    "alpha",
			wantVersion: "v1.0.0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeDispatchingInfoProvider(t, dir, "agent-provider-shared", tc.answers)
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
