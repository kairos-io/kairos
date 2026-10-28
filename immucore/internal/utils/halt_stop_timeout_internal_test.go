package utils

import (
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// HaltWithBanner is reached from haltTerminated when immucore is signalled
// before the mount DAG finishes, and the signal that gets it there is the
// SIGTERM systemd sends when it stops the unit for Conflicts=. That SIGTERM
// also arms TimeoutStopSec=. The unit has to outlive the countdown the banner
// prints, or systemd SIGKILLs immucore first, the Conflicts= is released and
// the switch-root carries on into a root whose binds were never established.
// See kairos-io/kairos#5143.
var _ = Describe("the dracut unit and the failure banner", func() {
	stopTimeout := func() time.Duration {
		for _, line := range strings.Split(bundled.ImmucoreServiceDracut, "\n") {
			value, found := strings.CutPrefix(strings.TrimSpace(line), "TimeoutStopSec=")
			if !found {
				continue
			}
			d, err := time.ParseDuration(value)
			Expect(err).ToNot(HaveOccurred(),
				"TimeoutStopSec= has to carry a unit both systemd and time.ParseDuration read")
			return d
		}
		return -1
	}

	// Without this, systemd falls back to DefaultTimeoutStopSec, which is 90s
	// in the initramfs and so is already shorter than the banner's own 90s.
	It("declares a stop timeout, in the Service section", func() {
		Expect(stopTimeout()).To(BeNumerically(">", 0))

		service := strings.Index(bundled.ImmucoreServiceDracut, "[Service]")
		Expect(service).To(BeNumerically(">=", 0))
		Expect(bundled.ImmucoreServiceDracut[service:]).To(ContainSubstring("TimeoutStopSec="))
	})

	It("gives the banner time to finish the reboot it promises", func() {
		Expect(stopTimeout()).To(BeNumerically(">", haltBannerSettle+haltBannerGrace))
	})
})
