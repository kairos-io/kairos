package cli

import (
	"bytes"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const validCloudConfig = `#cloud-config
users:
  - name: kairos
`

var _ = Describe("edit CloudInit", func() {
	It("saves valid configuration while preserving its mode", func() {
		dir := GinkgoT().TempDir()
		path := writeCloudConfig(dir, validCloudConfig, 0640)
		editor := writeEditor(dir, "valid-editor", "printf '%s' '#cloud-config\nusers:\n  - name: updated\n' > \"$1\"")

		Expect(editCloudInit(path, editor, &bytes.Buffer{})).To(Succeed())

		content, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("#cloud-config\nusers:\n  - name: updated\n"))
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0640)))
	})

	It("reopens after validation failure", func() {
		dir := GinkgoT().TempDir()
		path := writeCloudConfig(dir, validCloudConfig, 0644)
		editor := writeEditor(dir, "retry-editor", `if [ -e "$1.count" ]; then
printf '%s' '#cloud-config
users:
  - name: corrected
' > "$1"
else
touch "$1.count"
printf '%s' 'not yaml: [' > "$1"
fi`)
		var output bytes.Buffer

		Expect(editCloudInit(path, editor, &output)).To(Succeed())
		content, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("#cloud-config\nusers:\n  - name: corrected\n"))
	})

	It("does not save unchanged content", func() {
		dir := GinkgoT().TempDir()
		path := writeCloudConfig(dir, validCloudConfig, 0644)
		editor := writeEditor(dir, "unchanged-editor", ":")

		Expect(editCloudInit(path, editor, &bytes.Buffer{})).To(Succeed())
		content, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(validCloudConfig))
	})

	It("leaves the original configuration on editor failure", func() {
		dir := GinkgoT().TempDir()
		path := writeCloudConfig(dir, validCloudConfig, 0644)
		editor := writeEditor(dir, "failing-editor", "exit 1")

		Expect(editCloudInit(path, editor, &bytes.Buffer{})).To(HaveOccurred())
		content, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(validCloudConfig))
	})

	It("requires an editor", func() {
		dir := GinkgoT().TempDir()
		path := writeCloudConfig(dir, validCloudConfig, 0644)

		Expect(editCloudInit(path, "", &bytes.Buffer{})).To(HaveOccurred())
	})

	It("parses quoted editor arguments", func() {
		path := filepath.Join(GinkgoT().TempDir(), "config.yaml")
		editor := `/bin/sh -c 'printf "%s" "#cloud-config" > "$1"' --`

		Expect(runEditor(editor, path)).To(Succeed())
		content, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("#cloud-config"))
	})

	It("discards invalid configuration", func() {
		dir := GinkgoT().TempDir()
		path := writeCloudConfig(dir, validCloudConfig, 0644)
		editor := writeEditor(dir, "invalid-editor", "printf '%s' 'not yaml: [' > \"$1\"")
		var output bytes.Buffer

		Expect(editCloudInit(path, editor, &output)).To(Succeed())
		content, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(validCloudConfig))
		Expect(output.String()).To(ContainSubstring("Invalid draft discarded"))
	})

	It("keeps validation messages out of the configuration", func() {
		content := []byte(validCloudConfig)
		withMessage := addValidationMessage("/oem/90_custom.yaml", content, os.ErrInvalid)

		Expect(string(withMessage)).To(ContainSubstring("# File: /oem/90_custom.yaml"))
		Expect(string(withMessage)).To(ContainSubstring("# - invalid argument"))
		Expect(removeValidationMessage(withMessage)).To(Equal(content))
	})
})

func writeCloudConfig(dir, content string, mode os.FileMode) string {
	path := filepath.Join(dir, "90_custom.yaml")
	Expect(os.WriteFile(path, []byte(content), mode)).To(Succeed())
	return path
}

func writeEditor(dir, name, body string) string {
	path := filepath.Join(dir, name)
	Expect(os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0755)).To(Succeed())
	return path
}
