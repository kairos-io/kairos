package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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

		Expect(runEditor(editor, path, nil)).To(Succeed())
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

	Context("with ssh_hardening", func() {
		const hardenedCloudConfig = "#cloud-config\ninstall:\n  ssh_hardening: true\nusers:\n  - name: kairos\n    passwd: kairos\n"
		var dir string

		BeforeEach(func() {
			dir = GinkgoT().TempDir()
			original := configDirs
			configDirs = func() []string { return []string{dir} }
			DeferCleanup(func() { configDirs = original })
		})

		It("rejects configuration the agent would refuse to load", func() {
			path := writeCloudConfig(dir, validCloudConfig, 0644)
			editor := writeEditor(dir, "hardening-editor", "printf '%s' '"+hardenedCloudConfig+"' > \"$1\"")
			var output bytes.Buffer

			Expect(editCloudInit(path, editor, &output)).To(Succeed())
			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(validCloudConfig))
			Expect(output.String()).To(ContainSubstring("Invalid draft discarded"))
		})

		It("accepts it when another config gives a user ssh_authorized_keys", func() {
			Expect(os.WriteFile(filepath.Join(dir, "10_keys.yaml"), []byte("#cloud-config\nusers:\n  - name: kairos\n    ssh_authorized_keys:\n      - github:kairos\n"), 0644)).To(Succeed())
			path := writeCloudConfig(dir, validCloudConfig, 0644)
			editor := writeEditor(dir, "hardening-editor", "printf '%s' '"+hardenedCloudConfig+"' > \"$1\"")

			Expect(editCloudInit(path, editor, &bytes.Buffer{})).To(Succeed())
			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(hardenedCloudConfig))
		})

		It("rejects it when the edited file is a symlink whose old content had the keys", func() {
			target := filepath.Join(GinkgoT().TempDir(), "real.yaml")
			keyed := "#cloud-config\nusers:\n  - name: kairos\n    ssh_authorized_keys:\n      - github:kairos\n"
			Expect(os.WriteFile(target, []byte(keyed), 0644)).To(Succeed())
			path := filepath.Join(dir, "90_custom.yaml")
			Expect(os.Symlink(target, path)).To(Succeed())
			editor := writeEditor(dir, "hardening-editor", "printf '%s' '"+hardenedCloudConfig+"' > \"$1\"")
			var output bytes.Buffer

			Expect(editCloudInit(path, editor, &output)).To(Succeed())
			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(keyed))
			Expect(output.String()).To(ContainSubstring("Invalid draft discarded"))
		})

		It("accepts it when a config_url cannot be merged", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not_a_cloud_config: true\n"))
			}))
			DeferCleanup(server.Close)
			Expect(os.WriteFile(filepath.Join(dir, "10_remote.yaml"), []byte("#cloud-config\nconfig_url: "+server.URL+"/keys.yaml\n"), 0644)).To(Succeed())
			path := writeCloudConfig(dir, validCloudConfig, 0644)
			editor := writeEditor(dir, "hardening-editor", "printf '%s' '"+hardenedCloudConfig+"' > \"$1\"")

			Expect(editCloudInit(path, editor, &bytes.Buffer{})).To(Succeed())
			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(hardenedCloudConfig))
		})
	})

	It("edits a copy outside the configuration directory", func() {
		dir := GinkgoT().TempDir()
		path := writeCloudConfig(dir, validCloudConfig, 0644)
		marker := filepath.Join(dir, "editor-path")
		editor := writeEditor(dir, "path-editor", "printf '%s' \"$1\" > "+marker+"\nprintf '%s' '#cloud-config\nusers:\n  - name: updated\n' > \"$1\"")

		Expect(editCloudInit(path, editor, &bytes.Buffer{})).To(Succeed())

		editedPath, err := os.ReadFile(marker)
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Dir(string(editedPath))).NotTo(Equal(dir))
		Expect(filepath.Base(string(editedPath))).To(Equal("90_custom.yaml"))
		Expect(filepath.Dir(string(editedPath))).NotTo(BeADirectory())
		Expect(loadableConfigs(dir)).To(ConsistOf(path))
	})

	It("removes the working copy and keeps the original when terminated", func() {
		dir := GinkgoT().TempDir()
		path := writeCloudConfig(dir, validCloudConfig, 0644)
		marker := filepath.Join(dir, "editor-path")
		editor := writeEditor(dir, "slow-editor", "printf '%s' \"$1\" > "+marker+".tmp\nmv "+marker+".tmp "+marker+"\nexec sleep 30")
		signals := make(chan os.Signal)
		result := make(chan error, 1)

		go func() { result <- editCloudInitWithSignals(path, editor, &bytes.Buffer{}, signals) }()
		Eventually(marker).Should(BeAnExistingFile())
		signals <- syscall.SIGTERM

		var err error
		Eventually(result, "10s").Should(Receive(&err))
		Expect(err).To(MatchError(ContainSubstring("interrupted by terminated")))
		editedPath, readErr := os.ReadFile(marker)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(filepath.Dir(string(editedPath))).NotTo(BeADirectory())
		Expect(loadableConfigs(dir)).To(ConsistOf(path))
		content, readErr := os.ReadFile(path)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(validCloudConfig))
	})

	It("leaves SIGINT to the editor", func() {
		dir := GinkgoT().TempDir()
		path := writeCloudConfig(dir, validCloudConfig, 0644)
		started := filepath.Join(dir, "started")
		proceed := filepath.Join(dir, "proceed")
		editor := writeEditor(dir, "waiting-editor", "touch "+started+"\nwhile [ ! -e "+proceed+" ]; do sleep 0.05; done\nprintf '%s' '#cloud-config\nusers:\n  - name: updated\n' > \"$1\"")
		signals := make(chan os.Signal)
		result := make(chan error, 1)

		go func() { result <- editCloudInitWithSignals(path, editor, &bytes.Buffer{}, signals) }()
		Eventually(started).Should(BeAnExistingFile())
		signals <- os.Interrupt
		Expect(os.WriteFile(proceed, nil, 0644)).To(Succeed())

		Eventually(result, "10s").Should(Receive(Succeed()))
		content, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("#cloud-config\nusers:\n  - name: updated\n"))
	})

	It("stages saves under a name yip does not load", func() {
		ext := filepath.Ext(strings.ReplaceAll(saveTempPattern, "*", "123"))

		Expect(ext).NotTo(BeElementOf(".yaml", ".yml"))
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

func loadableConfigs(dir string) []string {
	var configs []string
	Expect(filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if ext := filepath.Ext(path); !info.IsDir() && (ext == ".yaml" || ext == ".yml") {
			configs = append(configs, path)
		}
		return nil
	})).To(Succeed())
	return configs
}
