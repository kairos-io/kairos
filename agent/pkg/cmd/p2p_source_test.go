package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/agent/pkg/p2p"
)

func TestValidateSourceAcceptsP2P(t *testing.T) {
	for _, s := range []string{"p2p://active", "p2p://10.0.0.2,10.0.0.3/passive", "oci:foo", "file:/x.img", ""} {
		if err := validateSource(s); err != nil {
			t.Errorf("validateSource(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{"p2p:/active", "ssh://x", "ftp://y"} {
		if err := validateSource(s); err == nil {
			t.Errorf("validateSource(%q) accepted, want error", s)
		}
	}
}

func TestP2PCommandIsRegistered(t *testing.T) {
	c := newP2PCmd()
	names := map[string]bool{}
	for _, s := range c.Commands() {
		names[s.Name()] = true
	}
	if !names["seed"] || !names["fetch"] || !names["pin"] || len(names) != 3 {
		t.Fatalf("p2p subcommands = %v, want seed, fetch and pin", names)
	}
	up := findCommand(t, "upgrade")
	for _, f := range []string{"experimental-p2p", "p2p-bind", "p2p-allow-cidr", "p2p-port", "p2p-insecure"} {
		if up.Flags().Lookup(f) == nil {
			t.Errorf("upgrade has no --%s flag", f)
		}
	}
}

func TestP2PSeedRefusesWithoutOptIn(t *testing.T) {
	t.Setenv("KAIROS_P2P_EXPERIMENTAL", "")
	c := newP2PSeedCmd()
	c.SetArgs([]string{"--state-dir", t.TempDir(), "--bind", "127.0.0.1", "--allow-cidr", "127.0.0.0/8"})
	if err := c.Execute(); err == nil {
		t.Fatal("seed ran without the experimental opt in")
	}
}

func capture(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var b bytes.Buffer
	_, _ = io.Copy(&b, r)
	return b.String()
}

func TestP2PPinPrintsTheSource(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "active.img")
	if err := os.WriteFile(img, bytes.Repeat([]byte{7}, 600<<10), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newP2PPinCmd()
	c.SetArgs([]string{"--image", img, "--bind", "10.0.0.2"})
	out := strings.TrimSpace(capture(t, func() {
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
	}))
	m, err := p2p.BuildManifest(img, "active", p2p.DefaultChunkSize)
	if err != nil {
		t.Fatal(err)
	}
	want := "p2p://10.0.0.2/active?pin=" + m.Root()
	if out != want {
		t.Fatalf("pin = %q, want %q", out, want)
	}
	c2 := newP2PPinCmd()
	c2.SetArgs([]string{"--image", img, "--bind", "10.0.0.2", "--kind", "persistent"})
	if err := c2.Execute(); err == nil {
		t.Fatal("pin accepted an image kind that is not shared")
	}
}

func TestP2PFetchRefusesASourceWithoutAPin(t *testing.T) {
	t.Setenv("KAIROS_P2P_EXPERIMENTAL", "1")
	c := newP2PFetchCmd()
	c.SetArgs([]string{"--source", "p2p://127.0.0.1:1/active", "--output", filepath.Join(t.TempDir(), "x"), "--bind", "127.0.0.1", "--allow-cidr", "127.0.0.0/8"})
	err := c.Execute()
	if err == nil || !strings.Contains(err.Error(), "refusing p2p source") {
		t.Fatalf("fetch without a pin: err = %v", err)
	}
}
