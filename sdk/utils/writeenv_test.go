package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"
)

// The files WriteEnv produces hold cluster join tokens: EDGEVPNTOKEN for the
// p2p mesh, K3S_TOKEN and K0S_TOKEN for the Kubernetes distributions. They
// must not be readable by anyone but root. See kairos-io/kairos#4761.
func TestWriteEnvCreatesPrivateFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "edgevpn-kairos.env")

	if err := WriteEnv(envFile, map[string]string{"EDGEVPNTOKEN": "a-join-token"}); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	info, err := os.Stat(envFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0600); got != want {
		t.Errorf("mode = %04o, want %04o", got, want)
	}
}

// A node installed before the mode was fixed already has a 0644 file on disk,
// and WriteEnv merges into whatever is there. Rewriting it has to bring the
// mode down too, otherwise an upgrade leaves the token exposed forever.
func TestWriteEnvTightensAnExistingFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "k3s")

	if err := os.WriteFile(envFile, []byte("K3S_URL=\"https://10.0.0.1:6443\"\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := WriteEnv(envFile, map[string]string{"K3S_TOKEN": "a-node-token"}); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	info, err := os.Stat(envFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0600); got != want {
		t.Errorf("mode = %04o, want %04o", got, want)
	}

	env, err := godotenv.Read(envFile)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if env["K3S_URL"] != "https://10.0.0.1:6443" {
		t.Errorf("K3S_URL = %q, want the pre-existing value preserved", env["K3S_URL"])
	}
	if env["K3S_TOKEN"] != "a-node-token" {
		t.Errorf("K3S_TOKEN = %q, want the new value written", env["K3S_TOKEN"])
	}
}
