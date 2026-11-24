package bundled_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// auditSyscalls returns the -S list of an audit rule line, nil when it has none.
func auditSyscalls(line string) []string {
	fields := strings.Fields(line)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "-S" {
			return strings.Split(fields[i+1], ",")
		}
	}
	return nil
}

// auditLines returns the syscall rule lines for one audit arch (b64 or b32).
func auditLines(rules, arch string) []string {
	out := []string{}
	for _, line := range strings.Split(rules, "\n") {
		if strings.Contains(line, " -F arch="+arch+" ") {
			out = append(out, line)
		}
	}
	return out
}

var _ = Describe("CISAuditRulesForArch (kairos-io/kairos#6090)", func() {
	aarch64Missing := []string{"chmod", "chown", "lchown", "creat", "open", "unlink", "rename"}

	DescribeTable("keeps the rules unchanged off arm64",
		func(arch string) {
			Expect(bundled.CISAuditRulesForArch(arch)).To(Equal(bundled.CISAuditRules))
		},
		Entry("amd64", "amd64"),
		Entry("unset", ""),
	)

	Describe("on arm64", func() {
		var rules string

		BeforeEach(func() {
			rules = bundled.CISAuditRulesForArch("arm64")
		})

		It("names no syscall aarch64 lacks on a b64 line", func() {
			b64 := auditLines(rules, "b64")
			Expect(b64).NotTo(BeEmpty())
			for _, line := range b64 {
				Expect(auditSyscalls(line)).NotTo(ContainElements(aarch64Missing), line)
			}
		})

		It("leaves no b64 line with an empty syscall list", func() {
			for _, line := range auditLines(rules, "b64") {
				names := auditSyscalls(line)
				Expect(names).NotTo(BeEmpty(), line)
				Expect(names).NotTo(ContainElement(""), line)
			}
		})

		It("keeps the same b64 rules, only with fewer syscalls", func() {
			Expect(auditLines(rules, "b64")).To(HaveLen(len(auditLines(bundled.CISAuditRules, "b64"))))
		})

		It("keeps the b32 lines unchanged", func() {
			Expect(auditLines(rules, "b32")).To(Equal(auditLines(bundled.CISAuditRules, "b32")))
		})

		It("keeps the *at forms that cover the dropped syscalls", func() {
			Expect(rules).To(ContainSubstring("-F arch=b64 -S fchmod,fchmodat -F"))
			Expect(rules).To(ContainSubstring("-F arch=b64 -S fchown,fchownat -F"))
			Expect(rules).To(ContainSubstring("-F arch=b64 -S openat,truncate,ftruncate -F"))
			Expect(rules).To(ContainSubstring("-F arch=b64 -S unlinkat,renameat -F"))
		})

		It("still ends with -e 2", func() {
			Expect(strings.TrimSpace(rules)).To(HaveSuffix("\n-e 2"))
		})
	})
})
