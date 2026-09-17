package validation_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/plugins"
	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"gopkg.in/yaml.v3"
)

// yip's directory plugin chmods a directory that already exists to whatever
// Permissions holds (pkg/plugins/dir.go, writePath). An omitted permissions:
// key leaves Permissions at 0, and os.FileMode(0) is 0000, so the step strips
// the mode off a directory the OS image ships. Because these are ordinary
// stages they run on every boot, which makes an operator's own chmod
// unrecoverable (kairos-io/kairos#4700).
//
// Assert the behaviour, not the YAML text: decode each bundled cloud-config
// through yip's own schema and run the real plugin over a pre-created tree.
var _ = Describe("Bundled cloudconfig directories", func() {
	It("leaves every declared directory traversable", func() {
		dir := filepath.Join("..", "bundled", "cloudconfigs")
		entries, err := os.ReadDir(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).NotTo(BeEmpty())

		checked := 0
		var offenders []string
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			Expect(err).NotTo(HaveOccurred())

			var config schema.YipConfig
			Expect(yaml.Unmarshal(content, &config)).To(Succeed(), entry.Name())

			for stageName, steps := range config.Stages {
				for _, step := range steps {
					if len(step.Directories) == 0 {
						continue
					}
					root, err := os.MkdirTemp("", "kairos-dirperms-")
					Expect(err).NotTo(HaveOccurred())
					defer os.RemoveAll(root)

					// Pre-create each path at 0755, the way the OS image
					// ships it, so the plugin takes the already-exists
					// branch that chmods rather than the mkdir one.
					rebased := make([]schema.Directory, 0, len(step.Directories))
					for _, d := range step.Directories {
						path := filepath.Join(root, d.Path)
						Expect(os.MkdirAll(path, 0755)).To(Succeed())
						copied := d
						copied.Path = path
						// The plugin chowns after it chmods, and the
						// test does not run as root. Owner/group is not
						// what is under test, so point it at the caller
						// to keep the chown a no-op.
						copied.Owner = os.Getuid()
						copied.Group = os.Getgid()
						rebased = append(rebased, copied)
					}

					err = plugins.EnsureDirectories(
						logger.NewNullLogger(),
						schema.Stage{Directories: rebased},
						vfs.OSFS,
						nil,
					)
					Expect(err).NotTo(HaveOccurred())

					for i, d := range rebased {
						info, err := os.Stat(d.Path)
						Expect(err).NotTo(HaveOccurred())
						mode := info.Mode().Perm()
						if mode&0100 == 0 {
							offenders = append(offenders, fmt.Sprintf(
								"%s: stage %q step %q declares %s with permissions %#o, "+
									"which leaves it non-traversable after the stage runs (got %#o)",
								entry.Name(), stageName, step.Name,
								step.Directories[i].Path, step.Directories[i].Permissions, mode))
						}
						// The declared mode is exactly what lands, so a
						// later edit cannot quietly widen or narrow it.
						Expect(mode).To(Equal(os.FileMode(d.Permissions).Perm()), d.Path)
						checked++
					}
				}
			}
		}
		Expect(offenders).To(BeEmpty(), strings.Join(offenders, "\n"))
		Expect(checked).NotTo(BeZero(), "no bundled cloud-config declared a directory")
	})
})
