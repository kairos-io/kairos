package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtraHierarchies(t *testing.T) {
	tests := []struct {
		name     string
		includes []string
		want     []string
	}{
		{name: "no includes", includes: nil, want: nil},
		{
			name:     "only built-ins",
			includes: []string{"/opt", "/usr", "opt/"},
			want:     nil,
		},
		{
			name:     "a hierarchy repart does not copy",
			includes: []string{"/srv"},
			want:     []string{"/srv"},
		},
		{
			name:     "normalizes, dedupes and sorts",
			includes: []string{"var/lib", " /srv ", "/srv/", "/opt"},
			want:     []string{"/srv", "/var/lib"},
		},
		{
			name:     "drops the root hierarchy",
			includes: []string{"/", ""},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extraHierarchies(tt.includes)
			if len(got) != len(tt.want) {
				t.Fatalf("extraHierarchies(%q) = %q, want %q", tt.includes, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("extraHierarchies(%q) = %q, want %q", tt.includes, got, tt.want)
				}
			}
		})
	}
}

// The built-in sysext.repart.d definitions copy /usr and /opt only, so a
// generated definition has to keep both and add every extra hierarchy.
// Missing a CopyFiles= line is what made `--include-path /srv` produce an
// image without /srv while the build still exited 0.
func TestWriteSysextDefinitionsCopiesEveryHierarchy(t *testing.T) {
	dir, err := writeSysextDefinitions([]string{"/srv", "/var/lib"})
	if err != nil {
		t.Fatalf("writeSysextDefinitions: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	root, err := os.ReadFile(filepath.Join(dir, "10-root.conf"))
	if err != nil {
		t.Fatalf("reading 10-root.conf: %v", err)
	}
	for _, want := range []string{
		"CopyFiles=/usr/",
		"CopyFiles=/opt/",
		"CopyFiles=/srv/",
		"CopyFiles=/var/lib/",
		"Type=root",
		"Format=erofs",
		"Verity=data",
		"VerityMatchKey=root",
	} {
		if !strings.Contains(string(root), want+"\n") {
			t.Errorf("10-root.conf is missing %q:\n%s", want, root)
		}
	}

	// The verity and signature partitions have to be there too: without them
	// --exclude-partitions=root-verity-sig and --private-key= have nothing to
	// act on and the DDI is not the shape systemd-sysext expects.
	for name, want := range map[string]string{
		"20-root-verity.conf":     "Type=root-verity",
		"30-root-verity-sig.conf": "Type=root-verity-sig",
	} {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if !strings.Contains(string(content), want+"\n") {
			t.Errorf("%s is missing %q:\n%s", name, want, content)
		}
	}
}

func TestWriteSysextDefinitionsIsReproducible(t *testing.T) {
	first, err := writeSysextDefinitions([]string{"/srv", "/var/lib"})
	if err != nil {
		t.Fatalf("writeSysextDefinitions: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(first) })
	second, err := writeSysextDefinitions([]string{"/srv", "/var/lib"})
	if err != nil {
		t.Fatalf("writeSysextDefinitions: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(second) })

	for _, name := range []string{"10-root.conf", "20-root-verity.conf", "30-root-verity-sig.conf"} {
		a, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		b, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if string(a) != string(b) {
			t.Errorf("%s differs between two runs:\n%s\n---\n%s", name, a, b)
		}
	}
}
