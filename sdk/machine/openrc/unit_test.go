package openrc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverrideCmd(t *testing.T) {
	tests := []struct {
		name     string
		opts     []ServiceOpts
		cmd      string
		wantFile string
		wantArgs string
	}{
		{
			name: "k3s server",
			opts: []ServiceOpts{
				WithName("k3s"),
				WithEnvFile("/etc/rancher/k3s/k3s.env"),
			},
			cmd:      "/usr/bin/k3s server --with-node-id",
			wantFile: "/etc/rancher/k3s/k3s.env",
			wantArgs: "server --with-node-id",
		},
		{
			name: "k0s controller, no k3s binary in sight",
			opts: []ServiceOpts{
				WithName("k0scontroller"),
				WithEnvFile("/etc/k0s/k0scontroller.env"),
			},
			cmd:      "/usr/bin/k0s controller --config /etc/k0s/k0s.yaml",
			wantFile: "/etc/k0s/k0scontroller.env",
			wantArgs: "controller --config /etc/k0s/k0s.yaml",
		},
		{
			name:     "arguments repeating the binary path are left alone",
			opts:     []ServiceOpts{WithName("k3s"), WithEnvFile("/etc/rancher/k3s/k3s.env")},
			cmd:      "/usr/bin/k3s server --kubelet-arg=root-dir=/usr/bin/k3s",
			wantFile: "/etc/rancher/k3s/k3s.env",
			wantArgs: "server --kubelet-arg=root-dir=/usr/bin/k3s",
		},
		{
			name:     "a command with no arguments",
			opts:     []ServiceOpts{WithName("k0sworker"), WithEnvFile("/etc/k0s/k0sworker.env")},
			cmd:      "/usr/bin/k0s",
			wantFile: "/etc/k0s/k0sworker.env",
			wantArgs: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			opts := append([]ServiceOpts{WithRoot(root)}, tt.opts...)

			s, err := NewService(opts...)
			if err != nil {
				t.Fatalf("NewService: %s", err)
			}

			if err := os.MkdirAll(filepath.Join(root, filepath.Dir(tt.wantFile)), 0755); err != nil {
				t.Fatalf("mkdir: %s", err)
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

func TestOverrideCmdKeepsExistingEnv(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "/etc/k0s/k0scontroller.env")
	if err := os.MkdirAll(filepath.Dir(envFile), 0755); err != nil {
		t.Fatalf("mkdir: %s", err)
	}
	if err := os.WriteFile(envFile, []byte("K0S_TOKEN=\"sometoken\"\n"), 0644); err != nil {
		t.Fatalf("seeding the env file: %s", err)
	}

	s, err := NewService(
		WithRoot(root),
		WithName("k0scontroller"),
		WithEnvFile("/etc/k0s/k0scontroller.env"),
	)
	if err != nil {
		t.Fatalf("NewService: %s", err)
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

func TestOverrideCmdEmptyCommand(t *testing.T) {
	s, err := NewService(WithRoot(t.TempDir()), WithName("k3s"), WithEnvFile("/etc/rancher/k3s/k3s.env"))
	if err != nil {
		t.Fatalf("NewService: %s", err)
	}
	if err := s.OverrideCmd("   "); err == nil {
		t.Error("expected an error for an empty command, got none")
	}
}

// openrc sources /etc/conf.d/<name> before the script body, so a command_args
// written there is overwritten by the script's own assignment. Fail loudly
// instead of writing a file that cannot take effect.
func TestOverrideCmdWithoutEnvFile(t *testing.T) {
	root := t.TempDir()
	s, err := NewService(WithRoot(root), WithName("myservice"))
	if err != nil {
		t.Fatalf("NewService: %s", err)
	}
	if err := s.OverrideCmd("/usr/bin/myservice --flag"); err == nil {
		t.Error("expected an error when no env file is configured, got none")
	}
	if _, err := os.Stat(filepath.Join(root, "/etc/conf.d/myservice")); !os.IsNotExist(err) {
		t.Errorf("nothing should have been written to conf.d, got %v", err)
	}
}
