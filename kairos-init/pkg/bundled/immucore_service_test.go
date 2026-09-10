package bundled_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ImmucoreServiceDracut", func() {
	lines := func() []string {
		return strings.Split(bundled.ImmucoreServiceDracut, "\n")
	}

	indexOf := func(want string) int {
		for i, line := range lines() {
			if strings.TrimSpace(line) == want {
				return i
			}
		}
		return -1
	}

	It("conflicts with the switch-root target", func() {
		Expect(indexOf("Conflicts=initrd-switch-root.target")).ToNot(Equal(-1))
	})

	// Conflicts= carries no ordering. Without an explicit Before=, systemd may
	// start initrd-switch-root.target while immucore is still building the
	// mount DAG and stop it mid-run, and the boot then continues into a root
	// whose binds and overlays were never established.
	It("orders itself before the switch-root target", func() {
		Expect(indexOf("Before=initrd-switch-root.target")).ToNot(Equal(-1))
	})

	It("declares both in the Unit section", func() {
		unit := strings.Index(bundled.ImmucoreServiceDracut, "[Unit]")
		service := strings.Index(bundled.ImmucoreServiceDracut, "[Service]")
		Expect(unit).To(BeNumerically(">=", 0))
		Expect(service).To(BeNumerically(">", unit))

		section := bundled.ImmucoreServiceDracut[unit:service]
		Expect(section).To(ContainSubstring("Before=initrd-switch-root.target"))
		Expect(section).To(ContainSubstring("Conflicts=initrd-switch-root.target"))
	})

	It("still runs as a oneshot that stays active after exiting", func() {
		Expect(bundled.ImmucoreServiceDracut).To(ContainSubstring("Type=oneshot"))
		Expect(bundled.ImmucoreServiceDracut).To(ContainSubstring("RemainAfterExit=yes"))
	})
})
