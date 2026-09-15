package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRCOverrideCmd(t *testing.T) {
	tests := []struct {
		name     string
		spec     Spec
		cmd      string
		wantFile string
		wantArgs string
	}{
		{
			name: "k3s server",
			spec: Spec{Name: "k3s", Init: map[Flavor]InitSpec{
				OpenRC: {EnvFile: "/etc/rancher/k3s/k3s.env"},
			}},
			cmd:      "/usr/bin/k3s server --with-node-id",
			wantFile: "/etc/rancher/k3s/k3s.env",
			wantArgs: "server --with-node-id",
		},
		{
			name: "k0s controller, no k3s binary in sight",
			spec: Spec{Name: "k0scontroller", Init: map[Flavor]InitSpec{
				OpenRC: {EnvFile: "/etc/k0s/k0scontroller.env"},
			}},
			cmd:      "/usr/bin/k0s controller --config /etc/k0s/k0s.yaml",
			wantFile: "/etc/k0s/k0scontroller.env",
			wantArgs: "controller --config /etc/k0s/k0s.yaml",
		},
		{
			name: "arguments repeating the binary path are left alone",
			spec: Spec{Name: "k3s", Init: map[Flavor]InitSpec{
				OpenRC: {EnvFile: "/etc/rancher/k3s/k3s.env"},
			}},
			cmd:      "/usr/bin/k3s server --kubelet-arg=root-dir=/usr/bin/k3s",
			wantFile: "/etc/rancher/k3s/k3s.env",
			wantArgs: "server --kubelet-arg=root-dir=/usr/bin/k3s",
		},
		{
			name: "a command with no arguments",
			spec: Spec{Name: "k0sworker", Init: map[Flavor]InitSpec{
				OpenRC: {EnvFile: "/etc/k0s/k0sworker.env"},
			}},
			cmd:      "/usr/bin/k0s",
			wantFile: "/etc/k0s/k0sworker.env",
			wantArgs: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.spec.Root = root

			s, err := NewFor(OpenRC, tt.spec)
			if err != nil {
				t.Fatalf("NewFor: %s", err)
			}
			if err := s.OverrideCmd(tt.cmd); err != nil {
				t.Fatalf("OverrideCmd: %s", err)
			}

			got, err := os.ReadFile(filepath.Join(root, tt.wantFile))
			if err != nil {
				t.Fatalf("reading the env file: %s", err)
			}

			want := "command_args=\"" + tt.wantArgs + "\"\n"
			if string(got) != want {
				t.Errorf("env file content:\ngot  %q\nwant %q", string(got), want)
			}
		})
	}
}

func TestOpenRCOverrideCmdKeepsExistingEnv(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "/etc/k0s/k0scontroller.env")
	if err := os.MkdirAll(filepath.Dir(envFile), 0755); err != nil {
		t.Fatalf("mkdir: %s", err)
	}
	if err := os.WriteFile(envFile, []byte("K0S_TOKEN=\"sometoken\"\n"), 0644); err != nil {
		t.Fatalf("seeding the env file: %s", err)
	}

	s, err := NewFor(OpenRC, Spec{
		Name: "k0scontroller",
		Root: root,
		Init: map[Flavor]InitSpec{OpenRC: {EnvFile: "/etc/k0s/k0scontroller.env"}},
	})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if err := s.OverrideCmd("/usr/bin/k0s controller"); err != nil {
		t.Fatalf("OverrideCmd: %s", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("reading the env file: %s", err)
	}
	want := "K0S_TOKEN=\"sometoken\"\ncommand_args=\"controller\"\n"
	if string(got) != want {
		t.Errorf("env file content:\ngot  %q\nwant %q", string(got), want)
	}
}

func TestOpenRCOverrideCmdEmptyCommand(t *testing.T) {
	s, err := NewFor(OpenRC, Spec{
		Name: "k3s",
		Root: t.TempDir(),
		Init: map[Flavor]InitSpec{OpenRC: {EnvFile: "/etc/rancher/k3s/k3s.env"}},
	})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if err := s.OverrideCmd("   "); err == nil {
		t.Error("expected an error for an empty command, got none")
	}
}

// openrc sources /etc/conf.d/<name> before the script body, so a command_args
// written there is overwritten by the script's own assignment. Fail loudly
// instead of writing a file that cannot take effect.
func TestOpenRCOverrideCmdWithoutEnvFile(t *testing.T) {
	root := t.TempDir()
	s, err := NewFor(OpenRC, Spec{Name: "myservice", Root: root})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	err = s.OverrideCmd("/usr/bin/myservice --flag")
	if err == nil {
		t.Fatal("expected an error when no env file is configured, got none")
	}
	// And for that reason, not because writing somewhere else happened to fail.
	if !strings.Contains(err.Error(), "no env file configured") {
		t.Errorf("error is %q, want it to name the missing env file", err)
	}

	if _, err := os.Stat(filepath.Join(root, "/etc/conf.d/myservice")); !os.IsNotExist(err) {
		t.Errorf("nothing should have been written to conf.d, got %v", err)
	}
}

func TestOpenRCWriteUnit(t *testing.T) {
	root := t.TempDir()
	s, err := NewFor(OpenRC, Spec{
		Name: "myservice",
		Root: root,
		Init: map[Flavor]InitSpec{OpenRC: {
			Unit:    "#!/sbin/openrc-run\n. " + EnvFilePlaceholder + "\n",
			EnvFile: "/etc/myservice/myservice.env",
		}},
	})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if err := s.WriteUnit(); err != nil {
		t.Fatalf("WriteUnit: %s", err)
	}

	path := filepath.Join(root, "/etc/init.d/myservice")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the unit: %s", err)
	}
	if want := "#!/sbin/openrc-run\n. /etc/myservice/myservice.env\n"; string(got) != want {
		t.Errorf("unit content:\ngot  %q\nwant %q", string(got), want)
	}

	// openrc runs the script, so it has to be executable.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %s", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("unit is not executable: mode %v", info.Mode().Perm())
	}
}
