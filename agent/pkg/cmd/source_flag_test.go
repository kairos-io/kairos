package cmd

import (
	"strings"
	"testing"
)

func TestValidateSource(t *testing.T) {
	for _, ok := range []string{
		"",
		"oci:quay.io/kairos/hadron:v1",
		"dir:/tmp/x",
		"file:/tmp/x.img",
		"ocifile:/tmp/x.tar",
		"ssh://10.0.0.5",
		"ssh://root@10.0.0.5",
		"ssh://root@10.0.0.5:2222/active",
		"ssh://root@node/passive",
		"ssh://root@node/some/absolute/file.img",
	} {
		if err := validateSource(ok); err != nil {
			t.Errorf("validateSource(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{
		"docker.io/foo",
		"http://x",
		"ssh:root@node",
		"ssh://root:secret@node",
		"ssh://root@node/a/../b",
		"ssh://-oProxyCommand=x@node",
	} {
		err := validateSource(bad)
		if err == nil {
			t.Errorf("validateSource(%q) was accepted", bad)
			continue
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("validateSource(%q) echoes the password: %v", bad, err)
		}
	}
	if err := validateSource("docker.io/foo"); err == nil || !strings.Contains(err.Error(), "ssh://") {
		t.Errorf("the error does not list ssh://: %v", err)
	}
}

// Every command that takes a source checks it with validateSource before it
// does anything else, so an ssh:// source must not be refused by any of them.
func TestSourceFlagAcceptsSSH(t *testing.T) {
	cases := map[string][]string{
		"upgrade":             {"--source", "ssh://kairos@10.0.0.5"},
		"manual-install":      {"--source", "ssh://kairos@10.0.0.5"},
		"interactive-install": {"--source", "ssh://kairos@10.0.0.5"},
		"install":             {"--source", "ssh://kairos@10.0.0.5"},
	}
	for name, args := range cases {
		cmd := findCommand(t, name)
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		err := cmd.PreRunE(cmd, nil)
		if err != nil && strings.Contains(err.Error(), "does not match") {
			t.Errorf("%s refuses an ssh source: %v", name, err)
		}
		bad := findCommand(t, name)
		if err := bad.ParseFlags([]string{"--source", "bogus://x"}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := bad.PreRunE(bad, nil); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Errorf("%s accepted a bad source: %v", name, err)
		}
	}
}

func TestUpgradePositionalSourceAcceptsSSH(t *testing.T) {
	// checkUpgrade is what the positional form goes through as well
	if err := checkUpgrade("ssh://kairos@10.0.0.5"); err != nil && strings.Contains(err.Error(), "does not match") {
		t.Errorf("checkUpgrade refuses an ssh source: %v", err)
	}
}
