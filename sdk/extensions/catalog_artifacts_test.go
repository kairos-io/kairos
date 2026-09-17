package extensions

import (
	"strings"
	"testing"
)

// An index lists a layer's image tags whether or not a system extension image
// was built for them: `latest` and the tag list come from the layer's own
// container package, the artifacts from a separate one. So a published tag
// with an empty `sysext` object is normal, and the published index has several.
// These tests pin that such a tag does not hide the versions that do publish.
const artifactlessLatestCatalog = `{
  "repo": "kairos-io/hadron-layers",
  "layers": [
    {"name": "fwupd", "latest": "2.1.7", "tags": [
      {"tag": "2.1.7", "sysext": {}},
      {"tag": "2.1.6", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/fwupd@sha256:` + oldDigest + `"}}},
      {"tag": "2.1.5", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/fwupd@sha256:` + currentDigest + `"}}}
    ]},
    {"name": "drbd", "latest": "9.3.3", "tags": [
      {"tag": "9.3.3", "sysext": {"arm64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/drbd@sha256:` + armDigest + `"}}},
      {"tag": "9.3.2", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/drbd@sha256:` + oldDigest + `"}}}
    ]},
    {"name": "git", "latest": "2.55.0", "tags": [
      {"tag": "2.55.0", "sysext": {}}
    ]},
    {"name": "vendor-tool", "latest": "nightly", "tags": [
      {"tag": "nightly", "sysext": {}},
      {"tag": "stable", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/vendor-tool@sha256:` + currentDigest + `"}}}
    ]}
  ]
}`

func artifactlessCatalog(t *testing.T) Catalog {
	t.Helper()
	return parseCatalogs(t, artifactlessLatestCatalog)[0]
}

// A bare name has to reach the newest version that is actually installable.
// Resolving `latest` and stopping there makes the layer unresolvable for every
// node as soon as an image tag is published ahead of its extension image, even
// though the previous version is published in full.
func TestResolveFallsBackToTheNewestPublishedVersion(t *testing.T) {
	catalog := artifactlessCatalog(t)

	got, err := catalog.Resolve("fwupd", "", "amd64")
	if err != nil {
		t.Fatalf("Resolve(fwupd, latest, amd64): %v", err)
	}
	want := Resolved{
		Repository:   "kairos-io/hadron-layers",
		Name:         "fwupd",
		Version:      "2.1.6",
		Architecture: "amd64",
		OCI:          "ghcr.io/kairos-io/hadron-layers/sysext/fwupd@sha256:" + oldDigest,
	}
	if got != want {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

// The fallback is per architecture: the newest version that publishes for
// amd64 and the newest that publishes for arm64 need not be the same one.
func TestResolveFallsBackPerArchitecture(t *testing.T) {
	catalog := artifactlessCatalog(t)

	amd64, err := catalog.Resolve("drbd", "", "amd64")
	if err != nil {
		t.Fatalf("Resolve(drbd, latest, amd64): %v", err)
	}
	if amd64.Version != "9.3.2" || amd64.OCI != "ghcr.io/kairos-io/hadron-layers/sysext/drbd@sha256:"+oldDigest {
		t.Errorf("Resolve(drbd, latest, amd64) = %#v, want version 9.3.2", amd64)
	}

	arm64, err := catalog.Resolve("drbd", "", "arm64")
	if err != nil {
		t.Fatalf("Resolve(drbd, latest, arm64): %v", err)
	}
	if arm64.Version != "9.3.3" || arm64.OCI != "ghcr.io/kairos-io/hadron-layers/sysext/drbd@sha256:"+armDigest {
		t.Errorf("Resolve(drbd, latest, arm64) = %#v, want version 9.3.3", arm64)
	}
}

// A constraint picks the highest version that satisfies it and publishes, for
// the same reason: ">= 2.1" must not resolve to a version with no image.
func TestResolveConstraintSkipsVersionsThatPublishNothing(t *testing.T) {
	catalog := artifactlessCatalog(t)

	got, err := catalog.Resolve("fwupd", ">= 2.1", "amd64")
	if err != nil {
		t.Fatalf("Resolve(fwupd, >= 2.1, amd64): %v", err)
	}
	if got.Version != "2.1.6" {
		t.Fatalf("Resolve(fwupd, >= 2.1) = %q, want 2.1.6: 2.1.7 publishes no image", got.Version)
	}
}

// A layer whose versions are not semver keeps resolving by index order, which
// lists the newest tag first.
func TestResolveFallsBackWithNonSemverTags(t *testing.T) {
	catalog := artifactlessCatalog(t)

	got, err := catalog.Resolve("vendor-tool", "", "amd64")
	if err != nil {
		t.Fatalf("Resolve(vendor-tool, latest, amd64): %v", err)
	}
	if got.Version != "stable" {
		t.Fatalf("Resolve(vendor-tool, latest) = %q, want stable: nightly publishes no image", got.Version)
	}
}

// An explicit version is not silently replaced by another one: the caller
// asked for that version, so an unbuildable one has to be reported.
func TestResolveKeepsAnExplicitVersionStrict(t *testing.T) {
	catalog := artifactlessCatalog(t)

	_, err := catalog.Resolve("fwupd", "2.1.7", "amd64")
	if err == nil {
		t.Fatal("Resolve(fwupd, 2.1.7) succeeded, want an error: that version publishes no image")
	}
	// The architecture is not what is wrong, so the error must not blame it.
	if strings.Contains(err.Error(), "architecture \"amd64\"") {
		t.Errorf("error = %q, want it to say the version publishes nothing, not that amd64 is unavailable", err)
	}
	for _, want := range []string{"fwupd", "2.1.7", "any architecture"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

func TestResolveErrorSaysWhatIsPublished(t *testing.T) {
	catalog := artifactlessCatalog(t)

	tests := []struct {
		name      string
		layer     string
		version   string
		arch      string
		wantInErr []string
		wantNotIn string
	}{
		{
			// The version exists and publishes, just not for this node.
			name:      "other architectures",
			layer:     "drbd",
			version:   "9.3.3",
			arch:      "amd64",
			wantInErr: []string{"amd64", "drbd", "9.3.3", "arm64"},
		},
		{
			// No version of the layer was ever built as an extension, so
			// naming the architecture would send the reader after the wrong
			// thing: there is nothing to install on any architecture.
			name:      "nothing at all",
			layer:     "git",
			version:   "",
			arch:      "amd64",
			wantInErr: []string{"git", "any architecture"},
			wantNotIn: "amd64",
		},
		{
			// Every version satisfying the constraint is artifactless, while
			// the layer itself does publish: say both.
			name:      "constraint met only by unpublished versions",
			layer:     "fwupd",
			version:   "> 2.1.6",
			arch:      "amd64",
			wantInErr: []string{"fwupd", "> 2.1.6", "amd64"},
		},
		{
			// The layer publishes, but never for this architecture.
			name:      "architecture never published",
			layer:     "fwupd",
			version:   "",
			arch:      "riscv64",
			wantInErr: []string{"fwupd", "riscv64", "amd64"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := catalog.Resolve(tt.layer, tt.version, tt.arch)
			if err == nil {
				t.Fatalf("Resolve(%s, %q, %s) succeeded, want an error", tt.layer, tt.version, tt.arch)
			}
			for _, want := range tt.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to name %q", err, want)
				}
			}
			if tt.wantNotIn != "" && strings.Contains(err.Error(), tt.wantNotIn) {
				t.Errorf("error = %q, want it not to mention %q", err, tt.wantNotIn)
			}
		})
	}
}

// A present but malformed reference is an index the node cannot trust, so it
// has to be reported rather than skipped over in favour of an older version.
func TestResolveDoesNotFallBackPastABrokenReference(t *testing.T) {
	document := `{"repo":"kairos-io/hadron-layers","layers":[{"name":"fwupd","latest":"2.1.7","tags":[
	  {"tag":"2.1.7","sysext":{"amd64":{"oci":"ghcr.io/kairos-io/hadron-layers/sysext/fwupd:2.1.7"}}},
	  {"tag":"2.1.6","sysext":{"amd64":{"oci":"ghcr.io/kairos-io/hadron-layers/sysext/fwupd@sha256:` + oldDigest + `"}}}
	]}]}`
	catalog := parseCatalogs(t, document)[0]

	got, err := catalog.Resolve("fwupd", "", "amd64")
	if err == nil {
		t.Fatalf("Resolve(fwupd) = %#v, want the malformed reference reported", got)
	}
	for _, want := range []string{"fwupd", "2.1.7", "amd64"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}
