package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemdWriteUnitNamesATemplateForAnInstance(t *testing.T) {
	root := t.TempDir()
	svc, err := NewFor(Systemd, Spec{
		Name:     "getty",
		Instance: "tty2",
		Root:     root,
		NoReload: true,
		Init:     map[Flavor]InitSpec{Systemd: {Unit: "[Service]\n"}},
	})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if err := svc.WriteUnit(); err != nil {
		t.Fatalf("WriteUnit: %s", err)
	}

	// A templated unit is installed under its template name, without the
	// instance, or systemd will not find it for any instance.
	if _, err := os.Stat(filepath.Join(root, "/etc/systemd/system/getty@.service")); err != nil {
		t.Errorf("template unit was not written: %v", err)
	}
}

func TestSystemdOverrideCmdWritesAReadableDropIn(t *testing.T) {
	root := t.TempDir()
	svc, err := NewFor(Systemd, Spec{Name: "k3s", Root: root, NoReload: true})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if err := svc.OverrideCmd("/usr/bin/k3s server --with-node-id"); err != nil {
		t.Fatalf("OverrideCmd: %s", err)
	}

	dir := filepath.Join(root, "/etc/systemd/system/k3s.service.d")
	got, err := os.ReadFile(filepath.Join(dir, "override.conf"))
	if err != nil {
		t.Fatalf("reading the drop-in: %s", err)
	}
	want := "\n[Service]\nExecStart=\nExecStart=/usr/bin/k3s server --with-node-id\n"
	if string(got) != want {
		t.Errorf("drop-in content:\ngot  %q\nwant %q", string(got), want)
	}

	// systemd has to be able to traverse the drop-in directory to read the
	// file inside it.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %s", err)
	}
	if info.Mode().Perm()&0100 == 0 {
		t.Errorf("drop-in directory is not traversable: mode %v", info.Mode().Perm())
	}
}

// The systemd backend must not invent an env file path. Writing to a path no
// unit names reports success for environment the service never reads, and the
// openrc backend already refuses to do it.
func TestSystemdEnvFileHasNoDefault(t *testing.T) {
	svc, err := NewFor(Systemd, Spec{Name: "edgevpn", Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if got := svc.EnvFile(); got != "" {
		t.Errorf("EnvFile() = %q, want an empty string: nothing in the Spec names one", got)
	}
	if err := svc.SetEnv(map[string]string{"FOO": "bar"}); err == nil {
		t.Error("SetEnv succeeded with no env file configured, so the caller thinks environment was written")
	}
}

// A Spec that names one is used as given.
func TestSystemdEnvFileFromTheSpec(t *testing.T) {
	root := t.TempDir()
	svc, err := NewFor(Systemd, Spec{
		Name: "k3s",
		Root: root,
		Init: map[Flavor]InitSpec{Systemd: {EnvFile: "/etc/sysconfig/k3s"}},
	})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if got, want := svc.EnvFile(), "/etc/sysconfig/k3s"; got != want {
		t.Errorf("EnvFile() = %q, want %q", got, want)
	}
	if err := svc.SetEnv(map[string]string{"FOO": "bar"}); err != nil {
		t.Fatalf("SetEnv: %s", err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/sysconfig/k3s")); err != nil {
		t.Errorf("SetEnv wrote nothing to the configured env file: %s", err)
	}
}

// The drop-in has to land where systemd looks for it, which for a templated
// unit is <name>@<instance>.service.d, not <name>.service.d.
func TestSystemdOverrideCmdDropInFollowsTheInstance(t *testing.T) {
	root := t.TempDir()
	svc, err := NewFor(Systemd, Spec{Name: "getty", Instance: "tty2", Root: root, NoReload: true})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if err := svc.OverrideCmd("/sbin/agetty tty2"); err != nil {
		t.Fatalf("OverrideCmd: %s", err)
	}

	if _, err := os.Stat(filepath.Join(root, "etc/systemd/system/getty@tty2.service.d/override.conf")); err != nil {
		t.Errorf("the drop-in is not where systemd reads it for getty@tty2.service: %s", err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/systemd/system/getty.service.d/override.conf")); err == nil {
		t.Error("the drop-in went to getty.service.d, which the running unit never reads")
	}
}

func TestSystemdOverrideCmdEmptyCommand(t *testing.T) {
	svc, err := NewFor(Systemd, Spec{Name: "k3s", Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if err := svc.OverrideCmd("   "); err == nil {
		t.Error("expected an error for an empty command, got none")
	}
}

// fakeSystemctl puts a systemctl first on PATH that appends its arguments to a
// log, and returns the log's path.
func fakeSystemctl(t *testing.T) string {
	t.Helper()

	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> '" + log + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0755); err != nil {
		t.Fatalf("writing the fake systemctl: %s", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	return log
}

// kairos-io/kairos#5612: OverrideCmd wrote the drop-in and never told systemd.
// A unit systemd already had loaded kept its stock ExecStart until something
// else happened to reload, so k3s could start without k3s.args.
func TestSystemdOverrideCmdReloadsSystemd(t *testing.T) {
	log := fakeSystemctl(t)
	svc, err := NewFor(Systemd, Spec{Name: "k3s", Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if err := svc.OverrideCmd("/usr/bin/k3s server --disable=traefik"); err != nil {
		t.Fatalf("OverrideCmd: %s", err)
	}

	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("systemctl was never called, so a loaded unit keeps its old ExecStart: %s", err)
	}
	if string(got) != "daemon-reload\n" {
		t.Errorf("systemctl calls = %q, want a single daemon-reload", got)
	}
}

// A Spec with NoReload writes for a system that is not running, where there is
// no systemd to talk to.
func TestSystemdOverrideCmdHonoursNoReload(t *testing.T) {
	log := fakeSystemctl(t)
	svc, err := NewFor(Systemd, Spec{Name: "k3s", Root: t.TempDir(), NoReload: true})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if err := svc.OverrideCmd("/usr/bin/k3s server"); err != nil {
		t.Fatalf("OverrideCmd: %s", err)
	}

	if got, err := os.ReadFile(log); err == nil {
		t.Errorf("systemctl was called despite NoReload: %q", got)
	}
}
