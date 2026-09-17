package validation_test

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/mudler/yip/pkg/schema"
	"gopkg.in/yaml.v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// yip's directory plugin applies the declared mode verbatim: it mkdirs a new
// directory with os.FileMode(Permissions) and chmods an already existing one to
// the same value (pkg/plugins/dir.go, writeDirectory and writePath).
// Permissions is a plain uint32 with no default, so omitting the key leaves it
// at 0, and os.FileMode(0) is 0000. The bundled configs run as ordinary stages,
// so that mode is reapplied on every boot and an operator's own chmod does not
// survive a reboot. A download's mode behaves the same way.
//
// An omitted key therefore does not mean "leave it alone", it means "make it
// unreachable", and no schema check can catch it because the config is
// perfectly valid. Assert it here instead, over every bundled config at once,
// so a new entry cannot reintroduce it.
var _ = Describe("Bundled cloudconfig directory permissions", func() {
	It("declares a mode for every directory and download, so nothing is chmodded to 0000", func() {
		var inspected, missing []string

		err := fs.WalkDir(bundled.EmbeddedConfigs, "cloudconfigs", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
				return nil
			}

			content, err := bundled.EmbeddedConfigs.ReadFile(path)
			if err != nil {
				return err
			}

			// Unmarshal into yip's own schema rather than grepping, so the test
			// reads Permissions exactly the way the runtime does.
			var config schema.YipConfig
			if err := yaml.Unmarshal(content, &config); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}

			record := func(kind, stage, step, target string, permissions uint32) {
				where := fmt.Sprintf("%s: stage %q, step %q, %s %s", path, stage, step, kind, target)
				inspected = append(inspected, where)
				if permissions == 0 {
					missing = append(missing, where)
				}
			}

			for stageName, steps := range config.Stages {
				for _, step := range steps {
					for _, dir := range step.Directories {
						record("directory", stageName, step.Name, dir.Path, dir.Permissions)
					}
					for _, download := range step.Downloads {
						record("download", stageName, step.Name, download.Path, download.Permissions)
					}
				}
			}

			return nil
		})
		Expect(err).NotTo(HaveOccurred())

		// Without this the check below passes vacuously: a walk that matched
		// nothing, or a schema whose yaml keys stopped lining up, would look
		// exactly like a clean audit.
		Expect(inspected).NotTo(BeEmpty(), "found no directories: or downloads: entries to check at all")

		Expect(missing).To(BeEmpty(), "these entries declare no permissions:, so yip applies mode 0000 to them on every boot")
	})
})
