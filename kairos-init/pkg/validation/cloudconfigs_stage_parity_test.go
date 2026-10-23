package validation_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// runStageRe matches every `run-stage <stage>` in a bundled service
// definition: a systemd ExecStart, the body of an openrc start(), or an
// openrc command_args.
var runStageRe = regexp.MustCompile(`run-stage ([a-z][a-z.-]*)`)

// runlevelLinkRe matches the openrc runlevel symlinks the "Enable OpenRC
// services" step creates.
var runlevelLinkRe = regexp.MustCompile(`init\.d/([a-z0-9-]+) /etc/runlevels/[a-z]+/`)

var commentRe = regexp.MustCompile(`^\s*#`)

// readServiceDefinitions returns the file with its comment lines removed, so
// prose that happens to say "run-stage" is not mistaken for an invocation.
func readServiceDefinitions(name string) string {
	content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", name))
	Expect(err).NotTo(HaveOccurred(), name)

	var kept []string
	for _, line := range strings.Split(string(content), "\n") {
		if !commentRe.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func stagesLaunchedBy(name string) []string {
	seen := map[string]bool{}
	for _, m := range runStageRe.FindAllStringSubmatch(readServiceDefinitions(name), -1) {
		seen[m[1]] = true
	}

	out := make([]string, 0, len(seen))
	for stage := range seen {
		out = append(out, stage)
	}
	sort.Strings(out)
	return out
}

var _ = Describe("Bundled service definitions", func() {
	// The openrc half of the bundled cloud-configs is not exercised by CI: no
	// workflow boots an Alpine or Hadron image. A stage only one of the two
	// service managers launches is a silent no-op on every image built for the
	// other. #4709 was reconcile, which openrc named by a binary that does not
	// exist. #4713 was fs, which openrc had no service for at all, so
	// /usr/local/cloud-config was never created on an openrc node.
	It("launches the same set of stages under systemd and openrc", func() {
		systemd := stagesLaunchedBy("09_systemd_services.yaml")
		openrc := stagesLaunchedBy("09_openrc_services.yaml")

		Expect(systemd).NotTo(BeEmpty(), "regexp matched nothing in the systemd services")
		Expect(openrc).To(Equal(systemd))
	})

	It("links every stage-running openrc service into a runlevel", func() {
		enabled := map[string]bool{}
		for _, m := range runlevelLinkRe.FindAllStringSubmatch(readServiceDefinitions("09_openrc_services.yaml"), -1) {
			enabled[m[1]] = true
		}
		Expect(enabled).NotTo(BeEmpty(), "regexp matched no runlevel symlinks")

		// A service that is written but never linked into a runlevel never
		// starts, which is the same no-op as not writing it at all.
		for _, stage := range stagesLaunchedBy("09_openrc_services.yaml") {
			service := "cos-setup-" + stage
			Expect(enabled).To(HaveKey(service), "%s is written but not linked into a runlevel", service)
		}
	})
})
