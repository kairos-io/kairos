package services

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("k0s service units", func() {
	Context("systemd", func() {
		It("reads the environment file the provider writes", func() {
			unit := K0sSystemdUnit("controller", "/etc/sysconfig/k0scontroller")

			Expect(unit).To(ContainSubstring("EnvironmentFile=-/etc/sysconfig/k0scontroller"))
			Expect(unit).To(ContainSubstring("ExecStart=/usr/bin/k0s controller"))
		})

		It("keeps the environment file optional, so the unit starts before the file exists", func() {
			unit := K0sSystemdUnit("worker", "/etc/sysconfig/k0sworker")

			for _, line := range strings.Split(unit, "\n") {
				if strings.HasPrefix(line, "EnvironmentFile=") {
					Expect(line).To(HavePrefix("EnvironmentFile=-"))
				}
			}
			Expect(unit).To(ContainSubstring("EnvironmentFile=-/etc/sysconfig/k0sworker"))
			Expect(unit).To(ContainSubstring("ExecStart=/usr/bin/k0s worker"))
		})

		It("declares the environment file inside the Service section", func() {
			unit := K0sSystemdUnit("controller", "/etc/sysconfig/k0scontroller")

			Expect(strings.Index(unit, "EnvironmentFile=")).To(
				BeNumerically(">", strings.Index(unit, "[Service]")))
			Expect(strings.Index(unit, "EnvironmentFile=")).To(
				BeNumerically("<", strings.Index(unit, "[Install]")))
		})
	})

	Context("openrc", func() {
		It("sources the environment file with allexport, so the variables reach the daemon", func() {
			envFile := "/etc/k0s/k0sworker.env"
			unit := K0sOpenRCUnit("worker", envFile)

			Expect(unit).To(ContainSubstring("set -o allexport"))
			Expect(unit).To(ContainSubstring(fmt.Sprintf("if [ -f %s ]; then source %s; fi", envFile, envFile)))
			Expect(unit).To(ContainSubstring("set +o allexport"))
			Expect(unit).To(ContainSubstring(`command_args="'worker' "`))

			// allexport has to be turned back off, otherwise everything the
			// rest of the script sets is exported too.
			Expect(strings.Index(unit, "set -o allexport")).To(
				BeNumerically("<", strings.Index(unit, envFile)))
			Expect(strings.Index(unit, "set +o allexport")).To(
				BeNumerically(">", strings.Index(unit, envFile)))
		})

		It("builds the controller script from the same template", func() {
			unit := K0sOpenRCUnit("controller", "/etc/k0s/k0scontroller.env")

			Expect(unit).To(HavePrefix("#!/sbin/openrc-run"))
			Expect(unit).To(ContainSubstring(`command_args="'controller' "`))
			Expect(unit).To(ContainSubstring("/etc/k0s/k0scontroller.env"))
		})
	})
})
