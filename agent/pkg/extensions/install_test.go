package extensions_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	installer "github.com/kairos-io/kairos/v4/agent/pkg/extensions"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	extensiontypes "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

const gitDigest = "1111111111111111111111111111111111111111111111111111111111111111"
const inHouseDigest = "2222222222222222222222222222222222222222222222222222222222222222"

const hadronCatalog = `{
  "repo": "kairos-io/hadron-layers",
  "layers": [{"name": "git", "latest": "2.55.0", "tags": [
    {"tag": "2.55.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:` + gitDigest + `"}}},
    {"tag": "2.50.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:` + gitDigest + `"}}}
  ]}]
}`

const privateCatalog = `{
  "repo": "example/private-layers",
  "layers": [{"name": "in-house", "latest": "1.0.0", "tags": [
    {"tag": "1.0.0", "sysext": {"amd64": {"oci": "registry.example.org/private/sysext/in-house@sha256:` + inHouseDigest + `"}}}
  ]}]
}`

// catalogClient serves catalog documents by URL onto the test filesystem, the
// way the real HTTP client writes a download to a path. A URL with no document
// registered fails, which is how an unreachable catalog is simulated.
type catalogClient struct {
	fs        vfs.FS
	documents map[string]string
	requested []string
}

func (c *catalogClient) GetURL(_ sdkLogger.KairosLogger, url, destination string) error {
	c.requested = append(c.requested, url)
	document, found := c.documents[url]
	if !found {
		return errors.New("connection refused")
	}
	return c.fs.WriteFile(destination, []byte(document), 0644)
}

func testConfig(t *testing.T, documents map[string]string) (*sdkConfig.Config, *catalogClient, *v1mock.FakeImageExtractor) {
	t.Helper()
	fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	logger := sdkLogger.NewNullLogger()
	client := &catalogClient{fs: fs, documents: documents}
	extractor := v1mock.NewFakeImageExtractor(logger)
	cfg := agentConfig.NewConfig(
		agentConfig.WithFs(fs),
		agentConfig.WithLogger(logger),
		agentConfig.WithClient(client),
		agentConfig.WithImageExtractor(extractor),
	)
	cfg.Platform.GolangArch = "amd64"
	return cfg, client, extractor
}

func TestResolveURIForACatalogName(t *testing.T) {
	cfg, _, _ := testConfig(t, map[string]string{"https://one.test/releases.json": hadronCatalog})
	catalogs, err := installer.FetchCatalogs(cfg, []string{"https://one.test/releases.json"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		requested extensiontypes.Extension
		want      string
	}{
		{
			name:      "newest version",
			requested: extensiontypes.Extension{Name: "git"},
			want:      "oci:ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:" + gitDigest,
		},
		{
			name:      "exact version",
			requested: extensiontypes.Extension{Name: "git", Version: "2.50.0"},
			want:      "oci:ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:" + gitDigest,
		},
		{
			name:      "constraint",
			requested: extensiontypes.Extension{Name: "git", Version: "< 2.55"},
			want:      "oci:ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:" + gitDigest,
		},
		{
			name:      "OCI reference",
			requested: extensiontypes.Extension{Name: "oci://ghcr.io/example/tools.sysext.raw"},
			want:      "oci://ghcr.io/example/tools.sysext.raw",
		},
		{
			name:      "absolute path becomes a file URI",
			requested: extensiontypes.Extension{Name: "/run/initramfs/live/tools.sysext.raw"},
			want:      "file:/run/initramfs/live/tools.sysext.raw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := installer.ResolveURI(cfg, catalogs, tt.requested)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("ResolveURI(%#v) = %q, want %q", tt.requested, got, tt.want)
			}
		})
	}
}

func TestResolveURIRejectsAVersionOnAReference(t *testing.T) {
	cfg, _, _ := testConfig(t, nil)

	_, err := installer.ResolveURI(cfg, nil, extensiontypes.Extension{Name: "oci://ghcr.io/example/tools", Version: "1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "cannot also take version") {
		t.Fatalf("error = %v, want it to refuse a version on a reference", err)
	}
}

func TestFetchCatalogsSkipsUnreachableOnes(t *testing.T) {
	cfg, client, _ := testConfig(t, map[string]string{"https://two.test/releases.json": privateCatalog})

	catalogs, err := installer.FetchCatalogs(cfg, []string{"https://one.test/releases.json", "https://two.test/releases.json"})
	if err != nil {
		t.Fatalf("one unreachable catalog must not fail the fetch: %v", err)
	}
	if len(catalogs) != 1 || catalogs[0].Repository != "example/private-layers" {
		t.Fatalf("catalogs = %#v, want only the reachable one", catalogs)
	}
	if len(client.requested) != 2 {
		t.Fatalf("requested %q, want both catalogs to be attempted", client.requested)
	}

	// The extension still resolves through the catalog that answered.
	if _, err := installer.ResolveURI(cfg, catalogs, extensiontypes.Extension{Name: "in-house"}); err != nil {
		t.Fatalf("ResolveURI(in-house): %v", err)
	}
}

func TestFetchCatalogsFailsWhenNoneAreReachable(t *testing.T) {
	cfg, _, _ := testConfig(t, nil)

	_, err := installer.FetchCatalogs(cfg, []string{"https://one.test/releases.json", "https://two.test/releases.json"})
	if err == nil {
		t.Fatal("FetchCatalogs succeeded with no reachable catalog")
	}
	// Both URLs have to be named, otherwise an operator cannot tell which one
	// is wrong.
	for _, want := range []string{"one.test", "two.test", "connection refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestFetchCatalogsRemovesItsTemporaryFiles(t *testing.T) {
	cfg, _, _ := testConfig(t, map[string]string{"https://one.test/releases.json": hadronCatalog})

	if _, err := installer.FetchCatalogs(cfg, []string{"https://one.test/releases.json"}); err != nil {
		t.Fatal(err)
	}
	leftovers, err := filepath.Glob(filepath.Join(os.TempDir(), "kairos-extension-catalog-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary catalog directories left behind: %q", leftovers)
	}
}

func TestInstallDeclaredResolvesEveryEntry(t *testing.T) {
	cfg, client, extractor := testConfig(t, map[string]string{
		"https://one.test/releases.json": hadronCatalog,
		"https://two.test/releases.json": privateCatalog,
	})
	cfg.Extensions.Catalogs = []string{"https://one.test/releases.json", "https://two.test/releases.json"}

	if err := vfs.MkdirAll(cfg.Fs, "/live", 0755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Fs.WriteFile("/live/local.sysext.raw", []byte("local image"), 0644); err != nil {
		t.Fatal(err)
	}

	target := "/target/extensions"
	requested := extensiontypes.Extensions{
		{Name: "git", Version: "< 2.55"},
		{Name: "in-house"},
		{Name: "oci://ghcr.io/example/tools.sysext.raw"},
		{Name: "/live/local.sysext.raw"},
	}
	if err := installer.InstallDeclared(cfg, requested, target); err != nil {
		t.Fatal(err)
	}

	// The two catalog names and the OCI reference all go through the image
	// extractor, into the directory that was asked for.
	var pulled []string
	for _, call := range extractor.ClientCalls {
		if call.Destination != target {
			t.Errorf("extension pulled into %q, want %q", call.Destination, target)
		}
		pulled = append(pulled, call.ImageRef)
	}
	want := []string{
		"ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:" + gitDigest,
		"registry.example.org/private/sysext/in-house@sha256:" + inHouseDigest,
		"ghcr.io/example/tools.sysext.raw:latest",
	}
	if len(pulled) != len(want) {
		t.Fatalf("pulled %q, want %q", pulled, want)
	}
	for i := range want {
		if pulled[i] != want[i] {
			t.Errorf("pull %d = %q, want %q", i, pulled[i], want[i])
		}
	}

	// The local path is copied rather than pulled.
	copied, err := cfg.Fs.ReadFile(filepath.Join(target, "local.sysext.raw"))
	if err != nil {
		t.Fatalf("the local extension was not installed: %v", err)
	}
	if string(copied) != "local image" {
		t.Fatalf("local extension content = %q", copied)
	}

	// Both catalogs were fetched once, not once per extension.
	if len(client.requested) != 2 {
		t.Fatalf("catalog requests = %q, want each catalog fetched once", client.requested)
	}
}

// A config that names nothing but references must install without reaching
// for a catalog at all, so a node with no route to the index still works.
func TestInstallDeclaredSkipsTheCatalogWhenNothingNeedsIt(t *testing.T) {
	cfg, client, extractor := testConfig(t, nil)

	requested := extensiontypes.Extensions{{Name: "oci://ghcr.io/example/tools.sysext.raw"}}
	if err := installer.InstallDeclared(cfg, requested, "/target/extensions"); err != nil {
		t.Fatal(err)
	}
	if len(client.requested) != 0 {
		t.Fatalf("catalogs requested = %q, want none", client.requested)
	}
	if len(extractor.ClientCalls) != 1 {
		t.Fatalf("extractor calls = %d, want 1", len(extractor.ClientCalls))
	}
}

func TestInstallDeclaredNamesTheExtensionThatFailed(t *testing.T) {
	cfg, _, _ := testConfig(t, map[string]string{"https://one.test/releases.json": hadronCatalog})
	cfg.Extensions.Catalogs = []string{"https://one.test/releases.json"}

	err := installer.InstallDeclared(cfg, extensiontypes.Extensions{{Name: "git"}, {Name: "nowhere", Version: "1.0.0"}}, "/target")
	if err == nil {
		t.Fatal("InstallDeclared succeeded with an unresolvable extension")
	}
	if !strings.Contains(err.Error(), "nowhere@1.0.0") {
		t.Fatalf("error = %q, want it to name the extension that failed", err)
	}
}

func TestInstallDeclaredWithNothingRequested(t *testing.T) {
	cfg, client, extractor := testConfig(t, nil)

	if err := installer.InstallDeclared(cfg, nil, "/target"); err != nil {
		t.Fatal(err)
	}
	if len(client.requested) != 0 || len(extractor.ClientCalls) != 0 {
		t.Fatal("InstallDeclared did work with nothing requested")
	}
}
