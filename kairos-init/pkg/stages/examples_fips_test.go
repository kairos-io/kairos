package stages_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/stages"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// repoRoot is the repository root, reached from this package's own location in
// the monorepo.
const repoRoot = "../../.."

// kairosInitCall matches one `kairos-init` invocation in an example Dockerfile
// and captures its arguments.
var kairosInitCall = regexp.MustCompile(`(?m)^\s*/kairos-init\s+(.*)$`)

// fipsExampleDockerfiles lists the Dockerfile of every examples/builds entry
// that builds a FIPS image.
func fipsExampleDockerfiles() []string {
	buildsDir := filepath.Join(repoRoot, "examples", "builds")
	entries, err := os.ReadDir(buildsDir)
	Expect(err).ToNot(HaveOccurred())

	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.Contains(e.Name(), "fips") {
			out = append(out, filepath.Join(buildsDir, e.Name(), "Dockerfile"))
		}
	}
	Expect(out).ToNot(BeEmpty())
	return out
}

// stageOf returns the stage a kairos-init invocation runs, defaulting to the
// binary's own default of "all".
func stageOf(args string) string {
	fields := strings.Fields(args)
	for i, f := range fields {
		if (f == "-s" || f == "--stage") && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return "all"
}

var _ = Describe("The FIPS example builds", func() {
	// --fips is a flag of a single kairos-init invocation, not state carried
	// between them, and KAIROS_FIPS is written by the init stage. An example
	// that splits the build and passes --fips only to the install stage
	// therefore ships FIPS binaries under a kairos-release saying
	// KAIROS_FIPS=false.
	It("passes --fips to the init stage whenever it passes it at all", func() {
		for _, dockerfile := range fipsExampleDockerfiles() {
			body, err := os.ReadFile(dockerfile)
			Expect(err).ToNot(HaveOccurred())

			calls := kairosInitCall.FindAllStringSubmatch(string(body), -1)
			Expect(calls).ToNot(BeEmpty(), "no kairos-init invocation in %s", dockerfile)

			var fipsUsed bool
			for _, c := range calls {
				if strings.Contains(c[1], "--fips") {
					fipsUsed = true
					break
				}
			}
			if !fipsUsed {
				continue
			}

			for _, c := range calls {
				stage := stageOf(c[1])
				if stage != "init" && stage != "all" {
					continue
				}
				Expect(c[1]).To(ContainSubstring("--fips"),
					"%s builds with --fips but runs the %q stage without it, so the image reports KAIROS_FIPS=false",
					dockerfile, stage)
			}
		}
	})
})

var _ = Describe("The Ubuntu FIPS error message", func() {
	// The install stage refuses Ubuntu + FIPS and points the user at an
	// example build. That pointer is the only guidance the user gets, so every
	// path it names has to exist.
	It("names example directories that exist", func() {
		config.DefaultConfig.Fips = true
		DeferCleanup(func() { config.DefaultConfig.Fips = false })

		_, err := stages.GetInstallStage(
			values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Arch: values.ArchAMD64, Version: "24.04"},
			logger.NewKairosLogger("test", "error", false),
		)
		Expect(err).To(HaveOccurred())

		paths := regexp.MustCompile(`examples/builds/[^/\s]+/Dockerfile`).FindAllString(err.Error(), -1)
		Expect(paths).ToNot(BeEmpty(), "the error no longer points at an example: %s", err)

		for _, p := range paths {
			Expect(filepath.Join(repoRoot, p)).To(BeAnExistingFile(),
				"the Ubuntu FIPS error points at %s, which does not exist", p)
		}
	})
})

var _ = Describe("The init stage with --fips", func() {
	BeforeEach(func() {
		config.DefaultConfig.Fips = true
		DeferCleanup(func() { config.DefaultConfig.Fips = false })
	})

	// This is what lets the RHEL-family examples stop hand-copying a dracut
	// config: with --fips, the init stage writes it itself.
	DescribeTable("writes the dracut FIPS config itself",
		func(distro values.Distro, family values.Family, version string) {
			data, err := stages.GetKairosInitramfsFilesStage(
				values.System{Distro: distro, Family: family, Arch: values.ArchAMD64, Version: version},
				logger.NewKairosLogger("test", "error", false),
			)
			Expect(err).ToNot(HaveOccurred())

			var content string
			for _, stage := range data {
				for _, f := range stage.Files {
					if f.Path == bundled.DracutFipsPath {
						content = f.Content
					}
				}
			}
			Expect(content).To(Equal(bundled.DracutFipsConfig))
			Expect(content).To(ContainSubstring(`add_dracutmodules+=" fips "`))
		},
		Entry("on Fedora", values.Fedora, values.RedHatFamily, "40"),
		Entry("on Rocky Linux", values.RockyLinux, values.RedHatFamily, "9"),
	)
})
