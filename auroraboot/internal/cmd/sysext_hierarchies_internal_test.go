package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUnmergedPrefix(t *testing.T) {
	merged := []string{"/usr/bin", "/usr/share", "/usr/lib", "/usr/local/bin"}
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "a merged hierarchy", path: "usr/bin/edgevpn", want: ""},
		{name: "deep inside a merged hierarchy", path: "/usr/share/doc/foo/README", want: ""},
		{name: "the hierarchy itself", path: "usr/lib", want: ""},
		{name: "a sibling of the merged ones", path: "usr/libexec/helper", want: "/usr/libexec"},
		{name: "a hierarchy nobody merges", path: "opt/vendor/bin/tool", want: "/opt"},
		{name: "a partially merged parent", path: "usr/local/lib/libfoo.so", want: "/usr/local/lib"},
		{name: "a file directly under an ancestor", path: "usr/stray", want: "/usr/stray"},
		{name: "the root", path: "/", want: ""},
		{name: "empty", path: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unmergedPrefix(tt.path, merged); got != tt.want {
				t.Fatalf("unmergedPrefix(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestUnmergedPrefixesWalksTheStagedTree(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"usr/bin/tool",
		"usr/share/doc/readme",
		"usr/libexec/helper",
		"usr/libexec/sub/other",
		"opt/vendor/bin/thing",
	} {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// An empty directory contributes no files to the node, so it is not worth
	// a warning.
	if err := os.MkdirAll(filepath.Join(root, "srv", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := unmergedPrefixes(root, mergedSysextHierarchies)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/opt", "/usr/libexec"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unmergedPrefixes = %v, want %v", got, want)
	}
}

func TestUnmergedPrefixesSaysNothingForAMergedTree(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "usr", "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := unmergedPrefixes(root, mergedSysextHierarchies)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unmergedPrefixes = %v, want none", got)
	}
}

func TestUnmergedHierarchyWarnings(t *testing.T) {
	t.Run("a prefix nobody asked for says to move the files", func(t *testing.T) {
		got := unmergedHierarchyWarnings([]string{"/usr/libexec"}, nil)
		if len(got) != 1 {
			t.Fatalf("got %d warnings, want 1: %v", len(got), got)
		}
		if !strings.Contains(got[0], "/usr/libexec") {
			t.Errorf("warning does not name the prefix: %q", got[0])
		}
		if !strings.Contains(got[0], "Move them under a merged hierarchy") {
			t.Errorf("warning does not say what to do: %q", got[0])
		}
	})

	t.Run("a prefix the operator included says to add the hierarchy", func(t *testing.T) {
		got := unmergedHierarchyWarnings([]string{"/opt"}, []string{"/opt"})
		if len(got) != 1 {
			t.Fatalf("got %d warnings, want 1: %v", len(got), got)
		}
		if !strings.Contains(got[0], "packed as you asked") {
			t.Errorf("an explicitly included path got the generic warning: %q", got[0])
		}
	})

	t.Run("a prefix under an included path counts as included", func(t *testing.T) {
		got := unmergedHierarchyWarnings([]string{"/opt/vendor"}, []string{"opt/"})
		if len(got) != 1 || !strings.Contains(got[0], "packed as you asked") {
			t.Errorf("got %v, want the included-path wording", got)
		}
	})

	t.Run("nothing unmerged, nothing printed", func(t *testing.T) {
		if got := unmergedHierarchyWarnings(nil, []string{"/opt"}); len(got) != 0 {
			t.Fatalf("got %v, want none", got)
		}
	})
}

func TestWarnUnmergedHierarchiesReportsEachPrefixOnce(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"usr/libexec/a", "usr/libexec/b", "usr/bin/c"} {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var lines []string
	if err := warnUnmergedHierarchies(root, nil, func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "/usr/libexec") {
		t.Fatalf("warning does not name the prefix: %q", lines[0])
	}
}

func TestParseSysextHierarchies(t *testing.T) {
	config := []byte(`
            Environment="SYSTEMD_SYSEXT_HIERARCHIES=/usr/bin:/usr/lib"
            export SYSTEMD_SYSEXT_HIERARCHIES="/usr/bin:/usr/lib/"
`)
	got := parseSysextHierarchies(config)
	want := [][]string{{"/usr/bin", "/usr/lib"}, {"/usr/bin", "/usr/lib"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSysextHierarchies = %v, want %v", got, want)
	}
}

// TestMergedHierarchiesMatchKairos is the drift guard. mergedSysextHierarchies
// is a copy of a list that lives in the kairos repository, and a copy with
// nothing holding it to its original is how this bug was introduced in the
// first place. Read the original out of the kairos module this build depends
// on and fail when they disagree, so a dependency bump that changes the
// hierarchies becomes a decision somebody makes rather than a warning that
// quietly stops being true.
func TestMergedHierarchiesMatchKairos(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/kairos-io/kairos/v4").Output()
	if err != nil {
		t.Skipf("cannot locate the kairos module (no module cache?): %v", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Skip("kairos module has no local directory")
	}

	config, err := os.ReadFile(filepath.Join(dir, "kairos-init", "pkg", "bundled", "cloudconfigs", "99_sysext.yaml"))
	if err != nil {
		t.Fatalf("reading 99_sysext.yaml from the kairos module: %v", err)
	}

	found := parseSysextHierarchies(config)
	if len(found) == 0 {
		t.Fatal("no SYSTEMD_SYSEXT_HIERARCHIES assignment in 99_sysext.yaml: " +
			"either the variable was renamed or the config moved, and the warning this guards is now checking nothing")
	}
	for i, list := range found {
		if !reflect.DeepEqual(list, found[0]) {
			t.Fatalf("99_sysext.yaml sets SYSTEMD_SYSEXT_HIERARCHIES to two different values: %v and %v (assignment %d)", found[0], list, i)
		}
	}

	want := append([]string(nil), found[0]...)
	got := append([]string(nil), mergedSysextHierarchies...)
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergedSysextHierarchies has drifted from 99_sysext.yaml.\n  here:   %v\n  kairos: %v", got, want)
	}
}

var _ = Describe("warnUnmergedHierarchies", func() {
	// Kairos mounts the persistent partition at /usr/local and does not
	// merge extensions over it, so a sysext carrying files there has to be
	// told.
	It("warns about files under /usr/local", func() {
		root := GinkgoT().TempDir()
		full := filepath.Join(root, "usr", "local", "bin", "hello.sh")
		Expect(os.MkdirAll(filepath.Dir(full), 0o755)).To(Succeed())
		Expect(os.WriteFile(full, []byte("#!/bin/sh\n"), 0o755)).To(Succeed())

		var lines []string
		Expect(warnUnmergedHierarchies(root, nil, func(l string) { lines = append(lines, l) })).To(Succeed())
		Expect(lines).To(ConsistOf(ContainSubstring("/usr/local")))
	})
})
