package schema_test

import (
	"os"
	"path/filepath"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// exampleCloudConfigs is the directory of cloud configs the repository ships
// as the examples users copy. It is reached from sdk/schema, where this test
// runs.
const exampleCloudConfigs = "../../examples/cloud-configs"

var _ = Describe("A key left blank", func() {
	// A key with nothing under it is an explicit null in YAML, and the zero
	// value of its Go field once it is decoded. The validator is the only
	// reader that could tell it apart from an absent key, and it must not.
	DescribeTable("validates as if the key were absent",
		func(config string) {
			Expect(Validate(config)).To(Succeed())
		},
		Entry("a list of strings", "#cloud-config\nenv:\n"),
		Entry("a list of objects", "#cloud-config\nbundles:\n"),
		Entry("a list of anything", "#cloud-config\noptions:\n"),
		Entry("the users list", "#cloud-config\nusers:\n"),
		Entry("a map of stages", "#cloud-config\nstages:\n"),
		Entry("the install block", "#cloud-config\ninstall:\n"),
		Entry("the p2p block", "#cloud-config\np2p:\n"),
		Entry("the kcrypt block", "#cloud-config\nkcrypt:\n"),
		Entry("a string inside a block", "#cloud-config\ninstall:\n  device:\n"),
		Entry("a list inside a block", "#cloud-config\ninstall:\n  bind_mounts:\n"),
		Entry("a list inside a list item",
			"#cloud-config\nusers:\n- name: kairos\n  passwd: kairos\n  groups:\n"),
		Entry("the authorized keys of a user, with the entries commented out",
			`#cloud-config
users:
- name: kairos
  passwd: kairos
  ssh_authorized_keys:
  # Replace with your github user and un-comment the line below:
  # - github:mudler
`),
	)

	DescribeTable("is not a way around the rest of the schema",
		func(config, wantError string) {
			err := Validate(config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(wantError))
		},
		// An empty list is a value, not a blank key: the user wrote out a
		// list and put nothing in it. RootSchema.Users keeps rejecting that.
		Entry("an explicitly empty users list",
			"#cloud-config\nusers: []\n",
			"minimum 1 items required"),
		// A null entry of a list is a mistake, it is not the same as writing
		// a shorter list, so only mapping entries are dropped.
		Entry("a null item in a list",
			"#cloud-config\nusers:\n- \n",
			"expected object, but got null"),
		Entry("a key that does carry a value of the wrong type",
			"#cloud-config\ninstall:\n  device: 123\n",
			"expected string, but got number"),
	)

	It("accepts every cloud config the repository ships as an example", func() {
		entries, err := os.ReadDir(exampleCloudConfigs)
		Expect(err).ToNot(HaveOccurred())

		var checked int
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
				continue
			}
			path := filepath.Join(exampleCloudConfigs, entry.Name())
			contents, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(Validate(string(contents))).To(Succeed(), "%s does not validate", path)
			checked++
		}
		Expect(checked).ToNot(BeZero(), "no example cloud configs were found in %s", exampleCloudConfigs)
	})
})
