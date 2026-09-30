package validation_test

import (
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// dropinUnit returns the unit a systemd drop-in file configures, which is the
// name of the ".d" directory holding it with that suffix removed:
// /etc/systemd/system/getty@.service.d/override.conf configures getty@.service.
func dropinUnit(path string) string {
	return strings.TrimSuffix(filepath.Base(filepath.Dir(path)), ".d")
}

// dropinCovers reports whether a drop-in written for unit dropin also applies
// to the instance unit want.
//
// systemd looks a drop-in up under the unit's own name and widens an instance
// name to its template directory, so getty@.service.d reaches every
// getty@ttyN.service while getty@tty1.service.d reaches only tty1. See
// unit_file_expand_dropin_names in systemd's src/shared/dropin.c.
func dropinCovers(dropin, want string) bool {
	if dropin == want {
		return true
	}
	prefix, _, isInstance := strings.Cut(want, "@")
	return isInstance && dropin == prefix+"@"+filepath.Ext(want)
}

var _ = Describe("Bundled autologin cloudconfig", func() {
	// 25_autologin.yaml gives a live boot a root console without a password.
	// It wrote the virtual-console drop-in to getty@tty1.service.d, so only
	// the first VT autologged in. logind starts tty2 to tty6 on demand as
	// autovt@ttyN (NAutoVTs=6), and an instance drop-in for tty1 says nothing
	// about those, so Alt+F2 onwards asked for a password. That is worst on an
	// interactive install, where 52_installer.yaml masks getty@tty1 to own the
	// console, leaving the serial line as the only autologin on the machine.
	//
	// autovt@.service is a symlink to getty@.service, so systemd resolves
	// autovt@ttyN.service to the getty@.service fragment and records
	// getty@ttyN.service as an alias of it. Drop-in lookup runs over every
	// alias and widens each instance to its template directory, so a drop-in
	// on the template does reach the autovt instances.
	//
	// Whether this stage runs at all is pinned by the 25_autologin.yaml specs
	// in cloudconfigs_install_mode_guard_test.go.
	var units []string

	BeforeEach(func() {
		units = nil
		for _, s := range readStages("25_autologin.yaml") {
			if s.OnlyServiceManager != "systemd" {
				continue
			}
			for _, f := range s.Files {
				units = append(units, dropinUnit(f.Path))
			}
			break
		}
		Expect(units).NotTo(BeEmpty(), "no systemd stage in 25_autologin.yaml writes a drop-in")
	})

	It("autologs in on every virtual console, not only tty1", func() {
		for _, vt := range []string{"tty1", "tty2", "tty3", "tty4", "tty5", "tty6"} {
			want := "getty@" + vt + ".service"

			var covered bool
			for _, u := range units {
				if dropinCovers(u, want) {
					covered = true
					break
				}
			}
			Expect(covered).To(BeTrue(), "nothing autologs in on %s; drop-ins are for %v", vt, units)
		}
	})

	// serial-getty@.service is its own template, so the getty@ drop-in does
	// not reach it. A machine whose only console is serial depends on this one.
	It("keeps the serial console drop-in", func() {
		Expect(units).To(ContainElement("serial-getty@ttyS0.service"))
	})
})
