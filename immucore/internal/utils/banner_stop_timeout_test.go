package utils_test

import (
	"strconv"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// rebootHandoffMargin is the time HaltWithBanner needs after the countdown
// ends: a syscall.Sync and starting systemd-reboot.service. The unit has to
// allow for it on top of the banner's own budget, because a SIGKILL during
// the handoff loses the reboot just as surely as one during the countdown.
const rebootHandoffMargin = 15 * time.Second

// parseTimeoutStopSec reads the TimeoutStopSec= value out of a unit file.
// Only the two forms this unit is allowed to use are accepted, a bare count
// of seconds and the same with an "s" suffix, so that a value written in
// systemd's wider time-span syntax fails the spec loudly instead of being
// silently misread as something shorter.
func parseTimeoutStopSec(unit string) (time.Duration, bool) {
	for _, line := range strings.Split(unit, "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "TimeoutStopSec=")
		if !found {
			continue
		}
		seconds, err := strconv.Atoi(strings.TrimSuffix(value, "s"))
		if err != nil {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	return 0, false
}

// The failure screen is painted from the signal watch, which means systemd is
// already stopping immucore.service and counting the timeout that ends in
// SIGKILL. If that timeout is shorter than the countdown the screen shows,
// the node is killed mid-promise: it never reboots, the switch-root goes
// ahead, and the boot reaches a login prompt whose persistent binds were
// never made. With no TimeoutStopSec= at all the manager default applies,
// which is the same 90 seconds the banner counts, so the kill always wins.
// See kairos-io/kairos#5144.
var _ = Describe("The immucore unit's stop timeout", func() {
	It("outlasts the reboot the failure banner promises", func() {
		timeout, found := parseTimeoutStopSec(bundled.ImmucoreServiceDracut)
		Expect(found).To(BeTrue(),
			"immucore.service must set TimeoutStopSec= explicitly; the manager default is too short for the banner")

		budget := utils.BannerSettleDelay + utils.BannerRebootGrace + rebootHandoffMargin
		Expect(timeout).To(BeNumerically(">=", budget),
			"TimeoutStopSec=%s leaves the banner's %s countdown unfinished", timeout, utils.BannerRebootGrace)
	})
})
