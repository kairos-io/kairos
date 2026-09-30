package mcp

import (
	"strings"
	"testing"

	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// The boot pins the image with `kairos-installer --source`, and the TUI and the
// web UI both install it. An install driven over MCP has to install the same
// one, or the machine comes up on a different image than the console would have
// given it, with nothing reporting that it happened.
func TestInstallUsesTheSourceTheBootPinnedWhenTheCallerNamesNone(t *testing.T) {
	const booted = "docker:quay.io/kairos/fedora:40-core-amd64-generic-v3.6.0"

	var installer *fakeInstaller
	session, _ := testServer(t, func(s *Server) {
		s.source = booted
		installer = &fakeInstaller{agentBin: "/usr/bin/kairos-agent"}
		s.installer = installer
	})

	res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true})
	if res.IsError {
		t.Fatalf("the install was refused: %s", resultText(t, res))
	}

	// Layer one: the argument handed to kairos-agent, which becomes --source.
	if installer.source != booted {
		t.Errorf("the agent was run with source %q, want %q", installer.source, booted)
	}

	// Layer two: the cloud-config written to disk, checked separately because
	// either one alone would install the wrong image if the other were right.
	if want := "source: " + booted; !strings.Contains(installer.config, want) {
		t.Errorf("the cloud-config the agent read is:\n%s\nwant it to carry %q", installer.config, want)
	}

	// And it is reported back, so the caller can see what it installed.
	out := decodeOutput[installOutput](t, res)
	if !strings.Contains(out.CloudConfig, booted) {
		t.Errorf("the reported cloud-config does not name the source:\n%s", out.CloudConfig)
	}
}

// The schema says an explicit source is "passed through to kairos-agent", so
// the fallback must not take it over.
func TestInstallPrefersTheCallersSourceOverTheBootedOne(t *testing.T) {
	const (
		booted = "docker:quay.io/kairos/fedora:40-core-amd64-generic-v3.6.0"
		asked  = "oci://my.registry/images/kairos:custom"
	)

	var installer *fakeInstaller
	session, _ := testServer(t, func(s *Server) {
		s.source = booted
		installer = &fakeInstaller{agentBin: "/usr/bin/kairos-agent"}
		s.installer = installer
	})

	res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true, Source: asked})
	if res.IsError {
		t.Fatalf("the install was refused: %s", resultText(t, res))
	}

	if installer.source != asked {
		t.Errorf("the agent was run with source %q, want the caller's %q", installer.source, asked)
	}
	if strings.Contains(installer.config, booted) {
		t.Errorf("the cloud-config still carries the booted source:\n%s", installer.config)
	}
	if want := "source: " + asked; !strings.Contains(installer.config, want) {
		t.Errorf("the cloud-config is:\n%s\nwant it to carry %q", installer.config, want)
	}
}

// A boot that pinned nothing must keep pinning nothing. Writing an empty source
// in would override the one kairos-agent resolves for itself, which turns a
// working install into a broken one.
func TestInstallWritesNoSourceWhenNeitherTheBootNorTheCallerNamedOne(t *testing.T) {
	var installer *fakeInstaller
	session, _ := testServer(t, func(s *Server) {
		installer = &fakeInstaller{agentBin: "/usr/bin/kairos-agent"}
		s.installer = installer
	})

	res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true})
	if res.IsError {
		t.Fatalf("the install was refused: %s", resultText(t, res))
	}

	if installer.source != "" {
		t.Errorf("the agent was run with source %q, want none", installer.source)
	}
	if strings.Contains(installer.config, "source:") {
		t.Errorf("the cloud-config names a source it was never given:\n%s", installer.config)
	}
}

// An agent decides what to pass by reading get_install_options first, so the
// default it will get has to be visible there rather than only in prose.
func TestInstallOptionsReportsTheSourceTheBootPinned(t *testing.T) {
	const booted = "docker:quay.io/kairos/fedora:40-core-amd64-generic-v3.6.0"

	session, _ := testServer(t, func(s *Server) { s.source = booted })

	res := call(t, session, ToolInstallOptions, installOptionsInput{})
	if res.IsError {
		t.Fatalf("get_install_options failed: %s", resultText(t, res))
	}

	if out := decodeOutput[installOptionsOutput](t, res); out.DefaultSource != booted {
		t.Errorf("default_source is %q, want %q", out.DefaultSource, booted)
	}
}

func TestInstallOptionsReportsNoSourceWhenTheBootPinnedNone(t *testing.T) {
	session, _ := testServer(t, nil)

	res := call(t, session, ToolInstallOptions, installOptionsInput{})
	if res.IsError {
		t.Fatalf("get_install_options failed: %s", resultText(t, res))
	}

	if out := decodeOutput[installOptionsOutput](t, res); out.DefaultSource != "" {
		t.Errorf("default_source is %q, want it empty", out.DefaultSource)
	}
}

// The wiring the bug was in: main hands a source to Handler, and it has to
// reach the Server the tools run against.
func TestNewCarriesTheSourceOntoTheServer(t *testing.T) {
	const booted = "docker:quay.io/kairos/fedora:40-core-amd64-generic-v3.6.0"

	log := sdkLogger.NewKairosLogger("test", "fatal", true)

	if got := New(log, booted).source; got != booted {
		t.Errorf("New kept source %q, want %q", got, booted)
	}
	if got := New(log, "").source; got != "" {
		t.Errorf("New invented a source %q from none", got)
	}
}
