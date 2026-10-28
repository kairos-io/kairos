package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/internal/phonehome"
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

	// Same blind spot, one step further out: the sweep reads the
	// cloud-configs, so it does not see a unit written by a kairos-init stage
	// either. kairos-splash is one. GetServicesStage writes it to
	// /usr/lib/systemd/system, which is neither the directory nor the
	// frontend the sweep above looks at, so it went missing from this list
	// for the whole of the splash feature's first release.
	//
	// Spelled out rather than derived from bundled.SplashServicePath on
	// purpose: kairos-init embeds binaries that are built rather than
	// committed, and importing it here would stop a plain `go test ./...` in
	// this package from compiling.
	It("still covers the units the kairos-init stages write", func() {
		Expect(defaultLogsConfig().Journal).To(ContainElement("kairos-splash"))
	})

	// The sweep above reads the cloud-configs, so it is blind to a unit the
	// agent writes under /etc/systemd/system itself. phone-home is the one
	// such unit today: enablePhoneHome writes phonehome.ServicePath and runs
	// systemctl enable on it whenever the merged config carries a url. The
	// name is derived from the constant the writer uses rather than spelled
	// out, so renaming the unit without touching this list fails here.
	It("asks journald for the units the agent writes itself", func() {
		unit := strings.TrimSuffix(filepath.Base(phonehome.ServicePath), ".service")
		Expect(unit).NotTo(Equal(filepath.Base(phonehome.ServicePath)),
			"expected %s to name a systemd service", phonehome.ServicePath)
		Expect(defaultLogsConfig().Journal).To(ContainElement(unit))
	})

	// The Kubernetes spec above covers only the units the providers install
	// at build time. The p2p provider also writes two of its own at
	// bootstrap: SetupVPN writes edgevpn@<instance>, SetupAPI writes
	// edgevpn, and bootstrap takes one branch or the other on every
	// token-based node. Neither name starts with kairos- or cos-setup-, so
	// the derived sweep's regexp would not see them even from a
	// cloud-config.
	//
	// Spelled out rather than derived from services.EdgeVPNDefaultInstance:
	// that constant is under provider/internal, which Go's internal rule
	// puts out of reach of this package.
	It("asks journald for the units the p2p provider writes at bootstrap", func() {
		Expect(defaultLogsConfig().Journal).To(ContainElements(
			"edgevpn", "edgevpn@kairos"))
	})
})

// unitDropInPath matches a drop-in Kairos writes for a unit it did not write
// itself. The unit name is unconstrained here, unlike kairosUnitPath above:
// the point of this sweep is the units whose names the project does not own.
var unitDropInPath = regexp.MustCompile(`/etc/systemd/system/([A-Za-z0-9@._-]+)\.service\.d/[A-Za-z0-9._-]+\.conf`)

// clearsExecStart matches the line that empties a unit's ExecStart list.
// systemd treats `ExecStart=` with nothing after it as "forget what the vendor
// unit said", so a drop-in carrying one replaces what the unit runs rather
// than adjusting how it runs.
var clearsExecStart = regexp.MustCompile(`(?m)^\s*ExecStart=\s*$`)

// execStartReplacedElsewhere lists the units a bundled cloud-config takes the
// ExecStart of, that the bundle deliberately does not carry a journal for.
// Keep this list short and say why for each entry: the spec below exists to
// make a new entry a decision somebody makes on purpose, rather than an
// omission nobody notices until a bundle is missing the one thing it needed.
var execStartReplacedElsewhere = map[string]string{
	// 25_autologin.yaml points both at agetty --autologin. They are console
	// units rather than part of a feature's control path, and both are
	// templates: `journalctl -u getty@` collects nothing, the instance name
	// is only known at runtime. kairos-splash already covers the console.
	"getty@":             "console template unit",
	"serial-getty@ttyS0": "console unit",
}

var _ = Describe("Journal list and the units Kairos takes over", Label("logs", "cmd"), func() {
	// A drop-in that clears ExecStart makes Kairos, not the distro, the
	// author of what the unit does. systemd-sysext and systemd-confext are
	// the case this was written for (#5534): 99_sysext.yaml replaces their
	// ExecStart with a Kairos image policy and gives them a 10 second start
	// timeout, with the comment that the unit can fail when no key unlocks an
	// extension. When that happens the journal is the only record of why a
	// system extension is not merged, and the bundle carried none of it.
	//
	// kairosUnitPath cannot see this: it is anchored to the kairos- and
	// cos-setup- name prefixes, and these units carry neither.
	It("asks journald for every unit whose ExecStart a bundled cloud-config replaces", func() {
		entries, err := os.ReadDir(cloudConfigDir)
		Expect(err).NotTo(HaveOccurred())

		takenOver := map[string]bool{}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			content, err := os.ReadFile(filepath.Join(cloudConfigDir, entry.Name()))
			Expect(err).NotTo(HaveOccurred())

			// Cut the file at each drop-in path and read only as far as the
			// next one, so an ExecStart= belonging to a later file entry is
			// not credited to this unit. Matched on the raw text for the same
			// reason kairosUnitPath is: one cloud-config writes its unit from
			// a shell heredoc rather than from a files: entry.
			text := string(content)
			matches := unitDropInPath.FindAllStringSubmatchIndex(text, -1)
			for i, match := range matches {
				end := len(text)
				if i+1 < len(matches) {
					end = matches[i+1][0]
				}
				if clearsExecStart.MatchString(text[match[1]:end]) {
					takenOver[text[match[2]:match[3]]] = true
				}
			}
		}
		// Guards against the directory moving out from under the relative
		// path above, which would leave this spec passing on an empty set.
		Expect(takenOver).NotTo(BeEmpty(), "found no replaced ExecStart in %s", cloudConfigDir)

		collected := map[string]bool{}
		for _, unit := range defaultLogsConfig().Journal {
			collected[unit] = true
		}

		var missing []string
		for unit := range takenOver {
			if collected[unit] {
				continue
			}
			if _, excused := execStartReplacedElsewhere[unit]; excused {
				continue
			}
			missing = append(missing, unit)
		}
		sort.Strings(missing)
		Expect(missing).To(BeEmpty(),
			"a bundled cloud-config replaces the ExecStart of these units, so Kairos decides what they run, "+
				"but kairos-agent logs never asks journald for them: %s. "+
				"Add them to defaultLogsConfig, or add them to execStartReplacedElsewhere with the reason.",
			strings.Join(missing, ", "))
	})

	// The exclusion list is only honest if every name in it is still a unit
	// some cloud-config actually takes over. A stale entry would silently
	// excuse a unit that came back under a different shape.
	It("keeps no stale entry in the exclusion list", func() {
		entries, err := os.ReadDir(cloudConfigDir)
		Expect(err).NotTo(HaveOccurred())

		var all strings.Builder
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			content, err := os.ReadFile(filepath.Join(cloudConfigDir, entry.Name()))
			Expect(err).NotTo(HaveOccurred())
			all.Write(content)
		}

		for unit, reason := range execStartReplacedElsewhere {
			Expect(all.String()).To(ContainSubstring("/etc/systemd/system/"+unit+".service.d/"),
				"%s is excused as %q but no bundled cloud-config drops in for it any more", unit, reason)
		}
	})
})
