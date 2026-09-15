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
	svc, err := NewFor(Systemd, Spec{Name: "k3s", Root: root})
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

// systemd passes arguments in the unit, so the env file is only environment and
// defaults to where systemd distributions keep it.
func TestSystemdEnvFileDefault(t *testing.T) {
	svc, err := NewFor(Systemd, Spec{Name: "k3s"})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if got, want := svc.EnvFile(), "/etc/sysconfig/k3s"; got != want {
		t.Errorf("EnvFile() = %q, want %q", got, want)
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
