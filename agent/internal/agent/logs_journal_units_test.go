package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// cloudConfigDir holds the cloud-configs kairos-init bakes into every image.
// Read off disk rather than through the kairos-init package, which embeds
// binaries that are built rather than committed and so cannot be imported from
// a plain `go test` here.
const cloudConfigDir = "../../../kairos-init/pkg/bundled/cloudconfigs"

// kairosUnitPath matches a unit Kairos itself writes under
// /etc/systemd/system. Restricted to the two prefixes the project owns, so the
// distro units the same files drop overrides for, getty@tty1 and the
// systemd-boot ones, stay out. Matched against the raw file text rather than
// the parsed YAML because 26_selinux.yaml writes its unit from a shell
// heredoc, not from a files: entry.
var kairosUnitPath = regexp.MustCompile(`/etc/systemd/system/((?:kairos|cos-setup)-[A-Za-z0-9._-]+)\.service`)

var _ = Describe("Default logs journal list", Label("logs", "cmd"), func() {
	// A debug bundle is read after the fact, by someone who was not watching
	// the boot. On a systemd image these units log to the journal and nowhere
	// else: /var/log/kairos/*.log only fills up when journald is absent, see
	// sdk/types/logger/logger.go. So a unit missing from this list is a unit
	// whose output the bundle cannot carry at all.
	It("asks journald for every unit the bundled cloud-configs write", func() {
		entries, err := os.ReadDir(cloudConfigDir)
		Expect(err).NotTo(HaveOccurred())

		shipped := map[string]bool{}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			content, err := os.ReadFile(filepath.Join(cloudConfigDir, entry.Name()))
			Expect(err).NotTo(HaveOccurred())

			for _, match := range kairosUnitPath.FindAllStringSubmatch(string(content), -1) {
				shipped[match[1]] = true
			}
		}
		// Guards against the directory moving out from under the relative
		// path above, which would otherwise leave this spec passing on an
		// empty set.
		Expect(shipped).NotTo(BeEmpty(), "found no Kairos units in %s", cloudConfigDir)

		collected := map[string]bool{}
		for _, unit := range defaultLogsConfig().Journal {
			collected[unit] = true
		}

		var missing []string
		for unit := range shipped {
			if !collected[unit] {
				missing = append(missing, unit)
			}
		}
		sort.Strings(missing)
		Expect(missing).To(BeEmpty(),
			"the cloud-configs enable these units but kairos-agent logs never asks journald for them: %s",
			strings.Join(missing, ", "))
	})

	// The reverse direction is not asserted. The list also carries k3s,
	// k3s-agent, k0scontroller and k0sworker, which the providers install at
	// build time rather than from a cloud-config, and a unit that did not run
	// costs nothing: Collect drops an empty journal.
	It("still covers the units the Kubernetes providers install", func() {
		Expect(defaultLogsConfig().Journal).To(ContainElements(
			"k3s", "k3s-agent", "k0scontroller", "k0sworker"))
	})
})
