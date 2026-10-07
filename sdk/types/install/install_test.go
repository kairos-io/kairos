// install_selinux_test.go - Tests for parsing the install.selinux block into
// the typed install.Install struct.
package install_test

import (
	"testing"

	install "github.com/kairos-io/kairos/v4/sdk/types/install"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

func TestInstallTypes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Install Types Suite")
}

var _ = Describe("Install Selinux type parsing", func() {
	DescribeTable("parses the install.selinux block into the typed struct",
		func(yamlDoc string, expectedEnabled bool, expectedMode string) {
			var inst install.Install
			err := yaml.Unmarshal([]byte(yamlDoc), &inst)
			Expect(err).ToNot(HaveOccurred())
			Expect(inst.Selinux.Enabled).To(Equal(expectedEnabled))
			Expect(inst.Selinux.Mode).To(Equal(expectedMode))
		},
		Entry("enabled with explicit enforcing mode",
			"selinux:\n  enabled: true\n  mode: enforcing\n",
			true, "enforcing"),
		Entry("enabled with explicit permissive mode",
			"selinux:\n  enabled: true\n  mode: permissive\n",
			true, "permissive"),
		Entry("enabled without mode leaves mode empty (permissive is applied downstream)",
			"selinux:\n  enabled: true\n",
			true, ""),
		Entry("absent selinux block leaves the zero value",
			"auto: true\n",
			false, ""),
	)
})

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
