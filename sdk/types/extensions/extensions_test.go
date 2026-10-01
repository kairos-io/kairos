package extensions

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseExtensionShorthand(t *testing.T) {
	tests := []struct {
		input   string
		name    string
		version string
	}{
		{input: "fwupd", name: "fwupd"},
		{input: "  fwupd  ", name: "fwupd"},
		{input: "fwupd@2.1.7", name: "fwupd", version: "2.1.7"},
		{input: "fwupd@>=2.1", name: "fwupd", version: ">=2.1"},
		// A digest-pinned reference keeps its `@`: it is one image, not a
		// name and a version.
		{input: "oci://ghcr.io/example/tools@sha256:" + strings.Repeat("a", 64), name: "oci://ghcr.io/example/tools@sha256:" + strings.Repeat("a", 64)},
		{input: "oci:ghcr.io/example/tools:1.0.0", name: "oci:ghcr.io/example/tools:1.0.0"},
		{input: "https://example.org/tools.sysext.raw", name: "https://example.org/tools.sysext.raw"},
		{input: "/run/initramfs/live/tools.sysext.raw", name: "/run/initramfs/live/tools.sysext.raw"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseExtension(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tt.name || got.Version != tt.version {
				t.Fatalf("ParseExtension(%q) = %#v, want name %q version %q", tt.input, got, tt.name, tt.version)
			}
		})
	}
}

func TestParseExtensionRejectsMalformedShorthand(t *testing.T) {
	for _, input := range []string{"", "   ", "@2.1.7", "fwupd@"} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseExtension(input); err == nil {
				t.Fatalf("ParseExtension(%q) was accepted", input)
			}
		})
	}
}

func TestIsReference(t *testing.T) {
	tests := map[string]bool{
		"fwupd":                         false,
		"my-layer":                      false,
		"oci://ghcr.io/example/tools":   true,
		"oci:ghcr.io/example/tools":     true,
		"docker://example/tools":        true,
		"container:example/tools":       true,
		"file:/tmp/tools.sysext.raw":    true,
		"http://example.org/tools.raw":  true,
		"https://example.org/tools.raw": true,
		"/run/initramfs/live/tools.raw": true,
		"ftp://example.org/tools.raw":   false,
		"weird:thing":                   false,
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := (Extension{Name: input}).IsReference(); got != want {
				t.Fatalf("IsReference(%q) = %v, want %v", input, got, want)
			}
		})
	}
}

func TestUnmarshalAcceptsBothForms(t *testing.T) {
	document := `
- fwupd
- fwupd@2.1.7
- name: git
  version: ">= 2.50, < 3"
- name: /run/initramfs/live/tools.sysext.raw
- oci://ghcr.io/example/tools.sysext.raw
`
	var got Extensions
	if err := yaml.Unmarshal([]byte(document), &got); err != nil {
		t.Fatal(err)
	}
	want := Extensions{
		{Name: "fwupd"},
		{Name: "fwupd", Version: "2.1.7"},
		{Name: "git", Version: ">= 2.50, < 3"},
		{Name: "/run/initramfs/live/tools.sysext.raw"},
		{Name: "oci://ghcr.io/example/tools.sysext.raw"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d extensions, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("extension %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestUnmarshalRejectsMalformedMappings(t *testing.T) {
	tests := map[string]string{
		"no name":               "- version: 2.1.7\n",
		"version on a URI":      "- name: oci://ghcr.io/example/tools\n  version: 2.1.7\n",
		"version on a path":     "- name: /run/tools.sysext.raw\n  version: 2.1.7\n",
		"empty shorthand entry": "- \"\"\n",
	}

	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			var got Extensions
			if err := yaml.Unmarshal([]byte(document), &got); err == nil {
				t.Fatalf("%s was accepted as %#v", document, got)
			}
		})
	}
}

func TestMarshalRoundTrips(t *testing.T) {
	want := Extensions{
		{Name: "fwupd"},
		{Name: "fwupd", Version: "2.1.7"},
		// A constraint with a space cannot be written as name@version, so it
		// has to come back out as a mapping.
		{Name: "git", Version: ">= 2.50, < 3"},
		{Name: "oci://ghcr.io/example/tools.sysext.raw"},
	}

	document, err := yaml.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Extensions
	if err := yaml.Unmarshal(document, &got); err != nil {
		t.Fatalf("marshalled form does not parse back: %v\n%s", err, document)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("extension %d round-tripped to %#v, want %#v\n%s", i, got[i], want[i], document)
		}
	}
}

func TestCatalogURLsFallsBackToTheDefault(t *testing.T) {
	got := Config{}.CatalogURLs()
	if len(got) != 1 || got[0] != DefaultCatalogURL {
		t.Fatalf("CatalogURLs() = %q, want [%q]", got, DefaultCatalogURL)
	}

	configured := Config{Catalogs: []string{"https://example.org/a.json", "https://example.org/b.json"}}
	got = configured.CatalogURLs()
	if len(got) != 2 || got[0] != "https://example.org/a.json" {
		t.Fatalf("CatalogURLs() = %q, want the configured list in order", got)
	}
}

func TestParseCmdline(t *testing.T) {
	tests := []struct {
		name    string
		cmdline string
		want    []Extension
	}{
		{
			name:    "nothing declared",
			cmdline: "console=tty1 rd.immucore.debug",
		},
		{
			name:    "one URL",
			cmdline: "console=tty1 kairos.extensions=https://10.0.0.1/tools.sysext.raw quiet",
			want:    []Extension{{Name: "https://10.0.0.1/tools.sysext.raw"}},
		},
		{
			// The whole point on netboot: pixiecore mints one served URL per
			// image, and they arrive in one value.
			name:    "a comma separated list keeps its order",
			cmdline: "kairos.extensions=oci://ghcr.io/example/b.sysext.raw,oci://ghcr.io/example/a.sysext.raw",
			want: []Extension{
				{Name: "oci://ghcr.io/example/b.sysext.raw"},
				{Name: "oci://ghcr.io/example/a.sysext.raw"},
			},
		},
		{
			// A cmdline entry means what a cloud config entry means, so a
			// catalog name and a version constraint work here too.
			name:    "a catalog name with a version",
			cmdline: "kairos.extensions=fwupd@2.1.7",
			want:    []Extension{{Name: "fwupd", Version: "2.1.7"}},
		},
		{
			// A digest-pinned reference is one image, and its `@` is part of
			// the reference rather than a version separator.
			name:    "a digest pinned reference keeps its digest",
			cmdline: "kairos.extensions=oci://ghcr.io/example/tools.sysext.raw@sha256:abc",
			want:    []Extension{{Name: "oci://ghcr.io/example/tools.sysext.raw@sha256:abc"}},
		},
		{
			// A template that always emits the keyword turns it off by
			// emitting no value, so an empty value cannot be an error.
			name:    "an empty value declares nothing",
			cmdline: "console=tty1 kairos.extensions= quiet",
		},
		{
			name:    "a bare keyword declares nothing",
			cmdline: "console=tty1 kairos.extensions quiet",
		},
		{
			name:    "empty entries in a list are skipped",
			cmdline: "kairos.extensions=,fwupd,,",
			want:    []Extension{{Name: "fwupd"}},
		},
		{
			// Two boots' worth of templating, or a build that appends, can
			// leave the key twice. Both are declarations.
			name:    "a repeated key accumulates",
			cmdline: "kairos.extensions=fwupd kairos.extensions=gpg",
			want:    []Extension{{Name: "fwupd"}, {Name: "gpg"}},
		},
		{
			// Substring matches must not count: kairos.extensions.catalogs is
			// not this key, and neither is a vendor's own prefix.
			name:    "a different key with the same prefix is not this one",
			cmdline: "kairos.extensions.catalogs=https://example/i.json my.kairos.extensions=fwupd",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCmdline(tc.cmdline)
			if err != nil {
				t.Fatalf("ParseCmdline(%q): %v", tc.cmdline, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("ParseCmdline(%q) = %v, want %v", tc.cmdline, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// An extension that was asked for and silently not installed is the failure
// this path exists to close, so a value that cannot be read has to stop the
// install rather than be skipped.
func TestParseCmdlineRejectsAnUnreadableEntry(t *testing.T) {
	_, err := ParseCmdline("kairos.extensions=fwupd@")
	if err == nil {
		t.Fatal("ParseCmdline accepted a name with an empty version")
	}
	if !strings.Contains(err.Error(), CmdlineKey) {
		t.Errorf("error %q does not name the key that carried the bad value", err)
	}
}
