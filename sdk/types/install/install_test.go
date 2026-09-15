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
