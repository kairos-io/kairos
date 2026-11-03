package machine_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// stageWritingFile is a cloud-config whose only step writes marker, so the
// spec can tell "the stage ran" from "the call returned nil". It writes with a
// command rather than a files block because the files plugin chowns to root,
// which a spec running as an ordinary user cannot do.
func stageWritingFile(stage, marker string) string {
	return fmt.Sprintf(`name: sdk machine spec
stages:
    %s:
        - commands:
            - echo written by the %s stage > %s
`, stage, stage, marker)
}

var _ = Describe("Running a cloud-config stage", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	Describe("ExecuteInlineCloudConfig", func() {
		It("runs the stage of the document it is given", func() {
			marker := filepath.Join(dir, "inline")

			// Before the fix this returned "exec: Stdout already set" from
			// ShellSTDIN, and with that repaired it returned exit status 127,
			// because no Kairos image carries an elemental binary. Either way
			// the stage never ran.
			Expect(machine.ExecuteInlineCloudConfig(stageWritingFile("initramfs", marker), "initramfs")).To(Succeed())

			Expect(os.ReadFile(marker)).To(ContainSubstring("written by the initramfs stage"))
		})

		It("runs only the stage it is asked for", func() {
			asked := filepath.Join(dir, "asked")
			other := filepath.Join(dir, "other")
			config := fmt.Sprintf(`name: sdk machine spec
stages:
    initramfs:
        - commands:
            - touch %s
    boot:
        - commands:
            - touch %s
`, asked, other)

			Expect(machine.ExecuteInlineCloudConfig(config, "initramfs")).To(Succeed())

			Expect(asked).To(BeAnExistingFile())
			Expect(other).ToNot(BeAnExistingFile())
		})

		It("reports a step that fails", func() {
			config := `name: sdk machine spec
stages:
    initramfs:
        - commands:
            - exit 7
`
			Expect(machine.ExecuteInlineCloudConfig(config, "initramfs")).ToNot(Succeed())
		})
	})

	Describe("ExecuteCloudConfig", func() {
		It("runs the stage of the file it is given", func() {
			marker := filepath.Join(dir, "fromfile")
			config := filepath.Join(dir, "config.yaml")
			Expect(os.WriteFile(config, []byte(stageWritingFile("boot", marker)), 0644)).To(Succeed())

			Expect(machine.ExecuteCloudConfig(config, "boot")).To(Succeed())

			Expect(os.ReadFile(marker)).To(ContainSubstring("written by the boot stage"))
		})
	})
})
