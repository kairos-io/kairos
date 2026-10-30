package stages

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// writeFakeProvider drops a binary that speaks the go-pluggable protocol:
// it is run with the event name as argv[1], reads the event from stdin and
// writes an EventResponse to stdout.
func writeFakeProvider(t *testing.T, dir, name string) {
	t.Helper()
	const script = "#!/bin/sh\ncat > /dev/null\nprintf '{\"state\":\"success\",\"data\":\"\",\"error\":\"\"}'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// runProviderBuildInstallEvent runs the event with a deadline, because the
// failure mode under test is a goroutine that never finishes rather than an
// error.
func runProviderBuildInstallEvent(t *testing.T) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- ProviderBuildInstallEvent(values.System{Family: values.DebianFamily}, logger.NewKairosLogger("test", "fatal", false))
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(20 * time.Second):
		t.Fatal("ProviderBuildInstallEvent never returned")
		return nil
	}
}

// setProviders installs a provider list for the duration of the test. The
// config is a package global, so it has to be restored.
func setProviders(t *testing.T, names ...string) {
	t.Helper()
	previous := config.DefaultConfig.Providers
	t.Cleanup(func() { config.DefaultConfig.Providers = previous })
	config.DefaultConfig.Providers = nil
	for _, n := range names {
		config.DefaultConfig.Providers = append(config.DefaultConfig.Providers, config.Provider{Name: n})
	}
}

// TestProviderBuildInstallEventCountsEveryPluginResponse covers an image that
// carries more than one provider plugin.
//
// The bus broadcasts: one Publish reaches every registered plugin, and the
// response handler runs once per plugin per Publish. A plugin that is not the
// addressee is expected to answer EventResponseNotApplicable, which is a
// response like any other. Sizing the WaitGroup and the error channel by the
// number of *configured providers* alone therefore under-counts whenever more
// than one provider binary is installed: the extra responses fill the channel,
// nothing drains it until after wg.Wait(), and the publishing goroutine never
// returns. kairos-init hangs for the rest of the build.
//
// One plugin is the case that happens to balance, so it is pinned here too to
// keep the fix honest.
func TestProviderBuildInstallEventCountsEveryPluginResponse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		plugins   []string
		providers []string
	}{
		{name: "one plugin, one provider", plugins: []string{"agent-provider-alpha"}, providers: []string{"alpha"}},
		{name: "one plugin, two providers", plugins: []string{"agent-provider-alpha"}, providers: []string{"alpha", "beta"}},
		{name: "two plugins, one provider", plugins: []string{"agent-provider-alpha", "agent-provider-beta"}, providers: []string{"alpha"}},
		{name: "two plugins, two providers", plugins: []string{"agent-provider-alpha", "agent-provider-beta"}, providers: []string{"alpha", "beta"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, p := range tc.plugins {
				writeFakeProvider(t, dir, p)
			}
			// Autoload scans the working directory, so this is what puts the
			// fake plugins on the bus.
			t.Chdir(dir)
			setProviders(t, tc.providers...)

			if err := runProviderBuildInstallEvent(t); err != nil {
				t.Fatalf("ProviderBuildInstallEvent: %v", err)
			}
		})
	}
}
