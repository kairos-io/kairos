package utils

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("silenceConsole", func() {
	var order []string

	record := func() (func(...string), func(string), func()) {
		return func(argv ...string) { order = append(order, "run "+strings.Join(argv, " ")) },
			func(level string) { order = append(order, "printk "+strings.TrimSpace(level)) },
			func() { order = append(order, "showstatus off") }
	}

	indexOf := func(prefix string) int {
		for i, step := range order {
			if strings.HasPrefix(step, prefix) {
				return i
			}
		}
		return -1
	}

	BeforeEach(func() {
		order = nil
		silenceConsole(record())
	})

	It("stops the boot splash", func() {
		Expect(indexOf("run systemctl stop kairos-splash.service")).ToNot(Equal(-1))
	})

	// The splash restores the kernel loglevel it quieted when it exits. Stop
	// it after the printk write and that restore lands on top of ours, so the
	// kernel starts logging over the failure screen the moment it is painted.
	It("stops the splash before it quiets the kernel", func() {
		Expect(indexOf("run systemctl stop kairos-splash.service")).
			To(BeNumerically("<", indexOf("printk ")))
	})

	It("still does everything it did before the splash existed", func() {
		Expect(indexOf("printk 1 4 1 7")).ToNot(Equal(-1))
		Expect(indexOf("run systemctl log-target null")).ToNot(Equal(-1))
		Expect(indexOf("showstatus off")).ToNot(Equal(-1))
		Expect(indexOf("run plymouth quit --retain-splash")).ToNot(Equal(-1))
	})

	// plymouth re-enables status output on its own, so quitting it has to come
	// after systemd's own status has been turned off, not before.
	It("quits plymouth last", func() {
		Expect(indexOf("run plymouth quit")).To(Equal(len(order) - 1))
	})
})
