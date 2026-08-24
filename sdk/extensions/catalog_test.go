package extensions

import (
	"fmt"
	"strings"
	"testing"
)

const (
	oldDigest     = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	currentDigest = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	armDigest     = "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789"
	testCatalog   = `{
  "repo": "kairos-io/hadron-layers",
  "layers": [{
    "name": "git",
    "latest": "2.55.0",
    "tags": [
      {"tag": "2.54.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:` + oldDigest + `"}}},
      {"tag": "2.55.0", "sysext": {
        "amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:` + currentDigest + `"},
        "arm64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:` + armDigest + `"}
      }}
    ]
  }]
}`
)

func TestResolveExactVersion(t *testing.T) {
	catalog, err := Parse(strings.NewReader(testCatalog))
	if err != nil {
		t.Fatal(err)
	}

	got, err := catalog.Resolve("git", "2.54.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	want := Resolved{
		Repository:   "kairos-io/hadron-layers",
		Name:         "git",
		Version:      "2.54.0",
		Architecture: "amd64",
		OCI:          "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:" + oldDigest,
	}
	if got != want {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestResolveLatestVersion(t *testing.T) {
	catalog, err := Parse(strings.NewReader(testCatalog))
	if err != nil {
		t.Fatal(err)
	}

	got, err := catalog.Resolve("git", "", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "2.55.0" || got.OCI != "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:"+armDigest {
		t.Fatalf("Resolve() = %#v", got)
	}
}

func TestResolveRejectsMissingCatalogEntries(t *testing.T) {
	tests := []struct {
		name      string
		layer     string
		version   string
		arch      string
		wantInErr string
	}{
		{name: "layer", layer: "gpg", version: "1.0.0", arch: "amd64", wantInErr: "gpg"},
		{name: "version", layer: "git", version: "9.9.9", arch: "amd64", wantInErr: "9.9.9"},
		{name: "architecture", layer: "git", version: "2.55.0", arch: "s390x", wantInErr: "s390x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog, err := Parse(strings.NewReader(testCatalog))
			if err != nil {
				t.Fatal(err)
			}
			_, err = catalog.Resolve(tt.layer, tt.version, tt.arch)
			if err == nil || !strings.Contains(err.Error(), tt.wantInErr) {
				t.Fatalf("Resolve() error = %v, want error containing %q", err, tt.wantInErr)
			}
		})
	}
}

func TestResolveRejectsInvalidOCIReferences(t *testing.T) {
	tests := []struct {
		name string
		oci  string
	}{
		{name: "empty", oci: ""},
		{name: "missing digest", oci: "ghcr.io/example/git:1.0.0"},
		{name: "empty digest", oci: "ghcr.io/example/git@sha256:"},
		{name: "short digest", oci: "ghcr.io/example/git@sha256:0123456789abcdef"},
		{name: "nonhex digest", oci: "ghcr.io/example/git@sha256:" + strings.Repeat("g", 64)},
		{name: "trailing characters", oci: "ghcr.io/example/git@sha256:" + oldDigest + "extra"},
		{name: "multiple separators", oci: "ghcr.io/example/git@sha256:" + oldDigest + "@sha256:" + currentDigest},
		{name: "malformed separator", oci: "ghcr.io/example/git@@sha256:" + oldDigest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document := fmt.Sprintf(`{"layers":[{"name":"git","latest":"1.0.0","tags":[{"tag":"1.0.0","sysext":{"amd64":{"oci":%q}}}]}]}`, tt.oci)
			catalog, err := Parse(strings.NewReader(document))
			if err != nil {
				t.Fatal(err)
			}
			_, err = catalog.Resolve("git", "", "amd64")
			if err == nil {
				t.Fatalf("Resolve() accepted invalid OCI reference %q", tt.oci)
			}
			for _, requested := range []string{"git", "1.0.0", "amd64"} {
				if !strings.Contains(err.Error(), requested) {
					t.Errorf("Resolve() error = %q, want requested value %q", err, requested)
				}
			}
		})
	}
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	_, err := Parse(strings.NewReader("{"))
	if err == nil {
		t.Fatal("Parse() error = nil, want invalid JSON error")
	}
}
