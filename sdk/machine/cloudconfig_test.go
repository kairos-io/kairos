package machine_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/kairos-io/kairos/v4/sdk/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Running a cloud-config stage", func() {
	var dir string

	// writesMarker is a cloud-config whose only effect is to create a file, so
	// a spec can tell whether the stage actually ran.
	writesMarker := func(stage, marker string) string {
		return fmt.Sprintf(`name: test
stages:
  %s:
    - name: marker
      commands:
        - echo ran > %s
`, stage, marker)
	}

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "cloud-config-stage")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(dir) })
	})

	Context("ExecuteInlineCloudConfig", func() {
		It("runs the stage", func() {
			marker := filepath.Join(dir, "marker")

			Expect(ExecuteInlineCloudConfig(writesMarker("initramfs", marker), "initramfs")).To(Succeed())

			Expect(os.ReadFile(marker)).To(BeEquivalentTo("ran\n"))
		})

		It("runs only the stage it was asked for", func() {
			wanted := filepath.Join(dir, "wanted")
			other := filepath.Join(dir, "other")
			config := fmt.Sprintf(`name: test
stages:
  initramfs:
    - name: wanted
      commands:
        - echo ran > %s
  boot:
    - name: other
      commands:
        - echo ran > %s
`, wanted, other)

			Expect(ExecuteInlineCloudConfig(config, "initramfs")).To(Succeed())

			Expect(os.ReadFile(wanted)).To(BeEquivalentTo("ran\n"))
			_, err := os.Stat(other)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("reports a failing command", func() {
			config := `name: test
stages:
  initramfs:
    - name: doomed
      commands:
        - exit 7
`
			Expect(ExecuteInlineCloudConfig(config, "initramfs")).ToNot(Succeed())
		})

		It("reports a cloud-config that is not valid YAML", func() {
			Expect(ExecuteInlineCloudConfig("stages: [", "initramfs")).ToNot(Succeed())
		})
	})

	Context("ExecuteCloudConfig", func() {
		It("runs the stage from the file", func() {
			marker := filepath.Join(dir, "marker")
			file := filepath.Join(dir, "config.yaml")
			Expect(os.WriteFile(file, []byte(writesMarker("initramfs", marker)), 0600)).To(Succeed())

			Expect(ExecuteCloudConfig(file, "initramfs")).To(Succeed())

			Expect(os.ReadFile(marker)).To(BeEquivalentTo("ran\n"))
		})

		It("does not run cloud-configs other than the one it was given", func() {
			mine := filepath.Join(dir, "mine")
			theirs := filepath.Join(dir, "theirs")

			// A second config sitting next to the first one. Passing a file
			// must not pull in its directory.
			Expect(os.WriteFile(filepath.Join(dir, "a-config.yaml"),
				[]byte(writesMarker("initramfs", mine)), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "b-config.yaml"),
				[]byte(writesMarker("initramfs", theirs)), 0600)).To(Succeed())

			Expect(ExecuteCloudConfig(filepath.Join(dir, "a-config.yaml"), "initramfs")).To(Succeed())

			Expect(os.ReadFile(mine)).To(BeEquivalentTo("ran\n"))
			_, err := os.Stat(theirs)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})
})
