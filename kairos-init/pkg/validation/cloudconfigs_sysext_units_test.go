package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// systemd added units/systemd-sysext.service in v248 and
// units/systemd-confext.service in v254. Below those versions the unit is
// simply not in the image, and `systemctl enable` on it fails: Ubuntu 22.04
// (systemd 249), Debian 12 (252) and the Red Hat 9 family (252) have sysext
// and no confext, Ubuntu 20.04 (245) has neither. yip's systemctl plugin keeps
// going after a failed unit, so the boot survives, but the stage reports an
// error on every boot of an image built on any of them, and that buries a real
// failure in the same stage.
//
// These specs read the stages that ask for the units and check that each one
// is guarded by the presence of its own unit file, by running the guard the
// way yip does.

type sysextSystemctl struct {
	Enable []string `yaml:"enable"`
}

type sysextStage struct {
	Name               string          `yaml:"name"`
	If                 string          `yaml:"if"`
	OnlyServiceManager string          `yaml:"only_service_manager"`
	Systemctl          sysextSystemctl `yaml:"systemctl"`
}

type sysextConfig struct {
	Stages map[string][]sysextStage `yaml:"stages"`
}

// unitDirRe matches the two spellings of the system unit directory, longest
// first, so a single pass can reroot both without the /usr one being rewritten
// twice.
var unitDirRe = regexp.MustCompile(`(/usr)?/lib/systemd/system`)

// stagesEnablingUnit returns every systemd stage in 99_sysext.yaml whose
// systemctl enable list names the unit.
func stagesEnablingUnit(unit string) []sysextStage {
	content, err := os.ReadFile(filepath.Join(cloudConfigsDir, "99_sysext.yaml"))
	Expect(err).NotTo(HaveOccurred())

	var cfg sysextConfig
	Expect(yaml.Unmarshal(content, &cfg)).To(Succeed())
	Expect(cfg.Stages["initramfs"]).NotTo(BeEmpty())

	var out []sysextStage
	for _, s := range cfg.Stages["initramfs"] {
		if s.OnlyServiceManager != "systemd" {
			continue
		}
		for _, e := range s.Systemctl.Enable {
			if e == unit {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// evalUnitGuard runs a stage's `if` with sh -c against a synthetic unit
// directory holding exactly the units present. It reports whether the stage
// would run.
func evalUnitGuard(expr string, present ...string) bool {
	root := GinkgoT().TempDir()
	unitDir := filepath.Join(root, "usr", "lib", "systemd", "system")
	Expect(os.MkdirAll(unitDir, 0755)).To(Succeed())
	for _, unit := range present {
		Expect(os.WriteFile(filepath.Join(unitDir, unit), nil, 0644)).To(Succeed())
	}

	// Both /usr/lib and /lib point at the same synthetic directory, which is
	// what a merged-/usr image looks like.
	expr = unitDirRe.ReplaceAllString(expr, filepath.Join(root, "usr", "lib", "systemd", "system"))

	err := exec.Command("sh", "-c", expr).Run()
	if err == nil {
		return true
	}
	_, isExit := err.(*exec.ExitError)
	Expect(isExit).To(BeTrue(), "guard failed to run: %v", err)
	return false
}

var _ = Describe("Bundled sysext cloudconfig unit guards", func() {
	// Each row is a target kairos-init builds for, with the units its systemd
	// package actually installs, read off the distro archives.
	targets := []struct {
		name   string
		units  []string
		sysext bool
		confex bool
	}{
		{"Ubuntu 24.04, Debian 13, Fedora, Hadron", []string{"systemd-sysext.service", "systemd-confext.service"}, true, true},
		{"Ubuntu 22.04, Debian 12, Red Hat 9", []string{"systemd-sysext.service"}, true, false},
		{"Ubuntu 20.04", nil, false, false},
	}

	DescribeTable("enables a unit only where the image ships it",
		func(unit string, want func(row int) bool) {
			stages := stagesEnablingUnit(unit)
			Expect(stages).To(HaveLen(1), "expected exactly one stage enabling %s", unit)
			guard := stages[0].If
			Expect(guard).NotTo(BeEmpty(), "%s is enabled unconditionally", unit)

			for i, target := range targets {
				Expect(evalUnitGuard(guard, target.units...)).To(Equal(want(i)),
					"%s on %s", unit, target.name)
			}
		},
		Entry("systemd-sysext", "systemd-sysext", func(i int) bool { return targets[i].sysext }),
		Entry("systemd-confext", "systemd-confext", func(i int) bool { return targets[i].confex }),
	)

	// The two units have independent version gates, so one stage asking for
	// both is wrong by construction: the confext half of it fails on every
	// systemd between 248 and 253, which is most of what Kairos builds on.
	It("does not ask for both units in one stage", func() {
		for _, stage := range stagesEnablingUnit("systemd-sysext") {
			Expect(stage.Systemctl.Enable).NotTo(ContainElement("systemd-confext"),
				"stage %q enables both, so its guard cannot be right for either", stage.Name)
		}
	})
})
