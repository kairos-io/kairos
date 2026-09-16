package utils

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MountUnitName", func() {
	It("names the unit systemd-fstab-generator would name", func() {
		Expect(MountUnitName("/var/log/audit")).To(Equal("var-log-audit.mount"))
		Expect(MountUnitName("/usr/local/.state")).To(Equal("usr-local-.state.mount"))
	})

	It("does not care about a trailing slash", func() {
		Expect(MountUnitName("/var/log/audit/")).To(Equal(MountUnitName("/var/log/audit")))
	})

	It("escapes what systemd-escape escapes", func() {
		Expect(MountUnitName("/var/log/audit-old")).To(Equal(`var-log-audit\x2dold.mount`))
		Expect(MountUnitName("/var/my logs")).To(Equal(`var-my\x20logs.mount`))
		Expect(MountUnitName("/")).To(Equal("-.mount"))
	})
})

var _ = Describe("WriteMountRequirementDropIn", func() {
	var unitDir string

	BeforeEach(func() {
		unitDir = GinkgoT().TempDir()
	})

	It("requires and orders after the mount unit of the path", func() {
		file, err := WriteMountRequirementDropIn(unitDir, constants.AuditdUnit, constants.AuditLogPath)

		Expect(err).ToNot(HaveOccurred())
		Expect(file).To(Equal(filepath.Join(unitDir, "auditd.service.d", constants.MountRequirementDropInName)))

		content, err := os.ReadFile(file)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("[Unit]\n"))
		Expect(string(content)).To(ContainSubstring("RequiresMountsFor=/var/log/audit\n"))
		// Requires= on the mount unit is what makes auditd fail instead of
		// logging to the ephemeral directory when the mount never happened.
		Expect(string(content)).To(ContainSubstring("Requires=var-log-audit.mount\n"))
		Expect(string(content)).To(ContainSubstring("After=var-log-audit.mount\n"))
	})

	It("is written for a unit that is not installed", func() {
		// auditd ships from the distro package, so on most images there is no
		// unit here yet at all. A drop-in for a missing unit is inert.
		_, err := WriteMountRequirementDropIn(unitDir, "not-installed.service", constants.AuditLogPath)

		Expect(err).ToNot(HaveOccurred())
		Expect(filepath.Join(unitDir, "not-installed.service.d")).To(BeADirectory())
	})

	It("overwrites the drop-in of an earlier boot", func() {
		first, err := WriteMountRequirementDropIn(unitDir, constants.AuditdUnit, "/var/log/stale")
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(first, []byte("stale\n"), 0644)).To(Succeed())

		second, err := WriteMountRequirementDropIn(unitDir, constants.AuditdUnit, constants.AuditLogPath)

		Expect(err).ToNot(HaveOccurred())
		Expect(second).To(Equal(first))
		content, err := os.ReadFile(second)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).ToNot(ContainSubstring("stale"))
		Expect(string(content)).To(ContainSubstring("Requires=var-log-audit.mount\n"))
	})
})
