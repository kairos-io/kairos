package install_test

import (
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/types/install"
	"gopkg.in/yaml.v3"
)

// TestInstallOEMFilesYAML pins the oem_files yaml/mapstructure/json tags: a
// cloud-config author writes oem_files with a name and content, and it has
// to decode into install.Install.OEMFiles the same way ssh_hardening and
// ephemeral_mounts do for their own keys.
func TestInstallOEMFilesYAML(t *testing.T) {
	const doc = `
oem_files:
  - name: foo
    content: |
      #cloud-config
      users:
        - name: kairos
  - name: bar.yaml
    content: "#cloud-config"
`
	var got install.Install
	if err := yaml.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	want := []install.OEMFile{
		{Name: "foo", Content: "#cloud-config\nusers:\n  - name: kairos\n"},
		{Name: "bar.yaml", Content: "#cloud-config"},
	}
	if len(got.OEMFiles) != len(want) {
		t.Fatalf("OEMFiles = %#v, want %#v", got.OEMFiles, want)
	}
	for i := range want {
		if got.OEMFiles[i] != want[i] {
			t.Fatalf("OEMFiles[%d] = %#v, want %#v", i, got.OEMFiles[i], want[i])
		}
	}
}

// TestInstallOEMFilesEmptyByDefault makes sure a config without an
// oem_files key leaves the slice nil rather than an empty, allocated one, so
// the OEMFiles hook's early "nothing to do" check keeps working unchanged.
func TestInstallOEMFilesEmptyByDefault(t *testing.T) {
	var got install.Install
	if err := yaml.Unmarshal([]byte("device: auto\n"), &got); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if got.OEMFiles != nil {
		t.Fatalf("OEMFiles = %#v, want nil", got.OEMFiles)
	}
}
