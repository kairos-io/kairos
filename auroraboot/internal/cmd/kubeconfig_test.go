package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const minimalKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: %[1]s
  cluster:
    server: %[2]s
contexts:
- name: %[1]s
  context:
    cluster: %[1]s
    user: %[1]s
users:
- name: %[1]s
  user:
    token: fake
current-context: %[1]s
`

func writeKubeconfig(t *testing.T, dir, ctx, server string) string {
	t.Helper()
	path := filepath.Join(dir, ctx+".kubeconfig")
	body := fmt.Sprintf(minimalKubeconfig, ctx, server)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// isolateKubeEnv scrubs every env var that could pull a config in from the
// host: KUBECONFIG (default loading rules), the in-cluster service-host pair,
// and HOME (~/.kube/config lookup by the default loading rules). Every branch
// of loadKubeConfig then depends only on what the test explicitly sets.
func isolateKubeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("KUBECONFIG", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	t.Setenv("HOME", t.TempDir())
}

func TestLoadKubeConfigExplicitPath(t *testing.T) {
	isolateKubeEnv(t)
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "prod", "https://prod.example")

	cfg, err := loadKubeConfig(path)
	if err != nil {
		t.Fatalf("loadKubeConfig(%q) returned error: %v", path, err)
	}
	if cfg.Host != "https://prod.example" {
		t.Fatalf("Host = %q, want https://prod.example", cfg.Host)
	}
}

func TestLoadKubeConfigMissingExplicitPath(t *testing.T) {
	isolateKubeEnv(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	if _, err := loadKubeConfig(missing); err == nil {
		t.Fatalf("loadKubeConfig(%q) succeeded, want error", missing)
	}
}

func TestLoadKubeConfigEmptyPathUsesKubeconfigEnv(t *testing.T) {
	isolateKubeEnv(t)
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "prod", "https://prod.example")
	t.Setenv("KUBECONFIG", path)

	cfg, err := loadKubeConfig("")
	if err != nil {
		t.Fatalf("loadKubeConfig(\"\") with KUBECONFIG=%q returned error: %v", path, err)
	}
	if cfg.Host != "https://prod.example" {
		t.Fatalf("Host = %q, want https://prod.example", cfg.Host)
	}
}

// TestLoadKubeConfigEmptyPathHandlesMultiFileKubeconfig is the regression test
// for the bug where --kubeconfig captured KUBECONFIG verbatim and handed a
// colon-joined multi-file value to BuildConfigFromFlags as a single path. With
// the flag no longer stealing the env var, the empty-path branch delegates to
// the default loading rules, which filepath.SplitList the env value.
func TestLoadKubeConfigEmptyPathHandlesMultiFileKubeconfig(t *testing.T) {
	isolateKubeEnv(t)
	dir := t.TempDir()
	primary := writeKubeconfig(t, dir, "prod", "https://prod.example")
	secondary := writeKubeconfig(t, dir, "dev", "https://dev.example")
	// Order matters: the first file's current-context wins the merge, so we
	// pick "prod" as the primary and assert its Host below.
	t.Setenv("KUBECONFIG", primary+string(os.PathListSeparator)+secondary)

	cfg, err := loadKubeConfig("")
	if err != nil {
		t.Fatalf("loadKubeConfig(\"\") with multi-file KUBECONFIG returned error: %v", err)
	}
	if cfg.Host != "https://prod.example" {
		t.Fatalf("Host = %q, want https://prod.example", cfg.Host)
	}
}

func TestLoadKubeConfigEmptyPathNoConfigErrors(t *testing.T) {
	// clientcmd memoizes the recommended kubeconfig path (~/.kube/config)
	// from HOME at package init, so t.Setenv("HOME", tempdir) does not
	// hide a real kubeconfig on the developer's box. Skip this assertion
	// when the host has one - the other tests still cover the meaningful
	// cases (explicit path, KUBECONFIG env, multi-file KUBECONFIG).
	if home, err := os.UserHomeDir(); err == nil {
		if _, statErr := os.Stat(filepath.Join(home, ".kube", "config")); statErr == nil {
			t.Skip("host has ~/.kube/config; loader cannot be isolated from it in-process")
		}
	}
	isolateKubeEnv(t)

	if _, err := loadKubeConfig(""); err == nil {
		t.Fatalf("loadKubeConfig(\"\") succeeded with no KUBECONFIG / no ~/.kube/config / not in-cluster, want error")
	}
}

func TestSanitizeClusterURL(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain https URL passes through", "https://kind.example:6443", "https://kind.example:6443"},
		{"userinfo is stripped", "https://alice:secret@kind.example:6443", "https://kind.example:6443"},
		{"user-only is stripped", "https://alice@kind.example:6443", "https://kind.example:6443"},
		{"query string is stripped", "https://kind.example:6443?token=SECRET", "https://kind.example:6443"},
		{"fragment is stripped", "https://kind.example:6443#anchor", "https://kind.example:6443"},
		{"userinfo and query both stripped", "https://alice:secret@kind.example:6443?token=SECRET", "https://kind.example:6443"},
		{"path is preserved", "https://rancher.example/k8s/clusters/c-abc", "https://rancher.example/k8s/clusters/c-abc"},
		{"host-only passes through", "kind.example:6443", "kind.example:6443"},
		{"empty passes through", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeClusterURL(tc.in); got != tc.want {
				t.Fatalf("sanitizeClusterURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
