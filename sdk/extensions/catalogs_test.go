package extensions

import (
	"strings"
	"testing"
)

const (
	hadronDigest  = "1111111111111111111111111111111111111111111111111111111111111111"
	privateDigest = "2222222222222222222222222222222222222222222222222222222222222222"
	oldGitDigest  = "3333333333333333333333333333333333333333333333333333333333333333"
)

// hadronCatalog stands in for the published index: it has several semver
// versions of one layer, and a layer whose tags are not semver at all.
const hadronCatalog = `{
  "repo": "kairos-io/hadron-layers",
  "layers": [
    {"name": "git", "latest": "2.55.0", "tags": [
      {"tag": "2.55.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:` + hadronDigest + `"}}},
      {"tag": "2.54.1", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:` + oldGitDigest + `"}}},
      {"tag": "2.50.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/git@sha256:` + oldGitDigest + `"}}}
    ]},
    {"name": "vendor-tool", "latest": "stable", "tags": [
      {"tag": "stable", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/vendor-tool@sha256:` + hadronDigest + `"}}}
    ]}
  ]
}`

// privateCatalog publishes a name the default catalog also publishes, plus one
// of its own.
const privateCatalog = `{
  "repo": "example/private-layers",
  "layers": [
    {"name": "git", "latest": "3.0.0", "tags": [
      {"tag": "3.0.0", "sysext": {"amd64": {"oci": "registry.example.org/private/sysext/git@sha256:` + privateDigest + `"}}}
    ]},
    {"name": "in-house", "latest": "1.0.0", "tags": [
      {"tag": "1.0.0", "sysext": {"amd64": {"oci": "registry.example.org/private/sysext/in-house@sha256:` + privateDigest + `"}}}
    ]}
  ]
}`

func parseCatalogs(t *testing.T, documents ...string) Catalogs {
	t.Helper()
	var catalogs Catalogs
	for _, document := range documents {
		catalog, err := Parse(strings.NewReader(document))
		if err != nil {
			t.Fatal(err)
		}
		catalogs = append(catalogs, catalog)
	}
	return catalogs
}

func TestResolveSemverConstraint(t *testing.T) {
	catalog := parseCatalogs(t, hadronCatalog)[0]

	tests := []struct {
		constraint string
		want       string
	}{
		// An exact published tag wins before anything is read as a constraint.
		{constraint: "2.54.1", want: "2.54.1"},
		// A constraint picks the highest published version that satisfies it.
		{constraint: ">= 2.50, < 2.55", want: "2.54.1"},
		{constraint: "^2.50", want: "2.55.0"},
		{constraint: "~2.54.0", want: "2.54.1"},
		{constraint: "< 2.54", want: "2.50.0"},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			got, err := catalog.Resolve("git", tt.constraint, "amd64")
			if err != nil {
				t.Fatal(err)
			}
			if got.Version != tt.want {
				t.Fatalf("Resolve(git, %q) = %q, want %q", tt.constraint, got.Version, tt.want)
			}
		})
	}
}

func TestResolveConstraintThatMatchesNothing(t *testing.T) {
	catalog := parseCatalogs(t, hadronCatalog)[0]

	_, err := catalog.Resolve("git", ">= 4.0", "amd64")
	if err == nil {
		t.Fatal("Resolve(git, >= 4.0) succeeded, want an error")
	}
	for _, want := range []string{"git", ">= 4.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

// A layer whose tags are not semver still has to resolve by exact tag, so a
// version scheme the constraint parser cannot read does not lock the layer out.
func TestResolveNonSemverTags(t *testing.T) {
	catalog := parseCatalogs(t, hadronCatalog)[0]

	got, err := catalog.Resolve("vendor-tool", "stable", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "stable" {
		t.Fatalf("Resolve(vendor-tool, stable) = %q, want %q", got.Version, "stable")
	}

	if _, err := catalog.Resolve("vendor-tool", "nightly", "amd64"); err == nil {
		t.Fatal("Resolve(vendor-tool, nightly) succeeded, want an error")
	}
}

func TestCatalogsFirstWins(t *testing.T) {
	// The private catalog is listed first, so its git shadows the default one.
	catalogs := parseCatalogs(t, privateCatalog, hadronCatalog)

	got, shadowed, err := catalogs.Resolve("git", "", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got.Repository != "example/private-layers" || got.Version != "3.0.0" {
		t.Fatalf("Resolve(git) = %#v, want the first catalog's entry", got)
	}
	if len(shadowed) != 1 || shadowed[0] != "kairos-io/hadron-layers" {
		t.Fatalf("shadowed = %q, want the second catalog's repository", shadowed)
	}
}

func TestCatalogsAreAdditive(t *testing.T) {
	catalogs := parseCatalogs(t, hadronCatalog, privateCatalog)

	// A name only the later catalog has still resolves.
	got, shadowed, err := catalogs.Resolve("in-house", "", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got.Repository != "example/private-layers" {
		t.Fatalf("Resolve(in-house) = %#v, want the private catalog's entry", got)
	}
	if shadowed != nil {
		t.Fatalf("shadowed = %q, want none", shadowed)
	}

	// A name only the earlier catalog has resolves too.
	if _, _, err := catalogs.Resolve("vendor-tool", "", "amd64"); err != nil {
		t.Fatalf("Resolve(vendor-tool): %v", err)
	}
}

func TestCatalogsErrorNamesEveryReason(t *testing.T) {
	catalogs := parseCatalogs(t, hadronCatalog, privateCatalog)

	_, _, err := catalogs.Resolve("nowhere", "", "amd64")
	if err == nil {
		t.Fatal("Resolve(nowhere) succeeded, want an error")
	}
	// Both catalogs have to be accounted for, so an operator can tell where
	// the name was looked for.
	for _, want := range []string{"nowhere", "kairos-io/hadron-layers", "example/private-layers"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

func TestCatalogsWithNoneConfigured(t *testing.T) {
	_, _, err := Catalogs{}.Resolve("git", "", "amd64")
	if err == nil || !strings.Contains(err.Error(), "no catalog is configured") {
		t.Fatalf("Resolve with no catalogs = %v, want it to say no catalog is configured", err)
	}
}
