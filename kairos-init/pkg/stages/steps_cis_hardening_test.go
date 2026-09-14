package stages_test

import (
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/stages"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/schema"
)

// fileByPath returns the single yip file entry written to path across all the
// given stages, failing the spec when it is absent or duplicated.
func fileByPath(result []schema.Stage, path string) schema.File {
	var found []schema.File
	for _, st := range result {
		for _, f := range st.Files {
			if f.Path == path {
				found = append(found, f)
			}
		}
	}
	ExpectWithOffset(1, found).To(HaveLen(1), "expected exactly one stage writing "+path)
	return found[0]
}

// commandsFor returns every command of the stages guarded on path existing.
func commandsFor(result []schema.Stage, path string) []string {
	var cmds []string
	for _, st := range result {
		if st.If == "test -f "+path {
			cmds = append(cmds, st.Commands...)
		}
	}
	return cmds
}

// chmodMode returns the mode expression of the single chmod applied to path.
func chmodMode(result []schema.Stage, path string) string {
	cmds := commandsFor(result, path)
	ExpectWithOffset(1, cmds).To(HaveLen(1), "expected exactly one command guarded on "+path)

	fields := strings.Fields(cmds[0])
	ExpectWithOffset(1, fields).To(HaveLen(3), "expected a `chmod <mode> <path>` command, got "+cmds[0])
	ExpectWithOffset(1, fields[0]).To(Equal("chmod"))
	return fields[1]
}

// dropsReadFor builds a matcher for a symbolic chmod expression that takes the
// read bit away from the given who-class (or from `a`). Matching on substrings
// is not enough: "g-r" does not appear in "go-rwx" even though that clause
// does clear group read.
func dropsReadFor(who string) OmegaMatcher {
	return MatchRegexp(`(` + who + `|a)[ugoa]*-[a-z]*r`)
}

var _ = Describe("GetCISHardeningStage", func() {
	var log logger.KairosLogger

	BeforeEach(func() {
		log = logger.NewKairosLogger("test", "error", true)
	})

	Context("with default config", func() {
		var result []schema.Stage

		BeforeEach(func() {
			result = stages.GetCISHardeningStage(values.System{}, log)
		})

		It("returns at least one stage", func() {
			Expect(result).ToNot(BeEmpty())
		})

		Describe("the filesystem module blocklist", func() {
			var blocklist schema.File

			BeforeEach(func() {
				blocklist = fileByPath(result, "/etc/modprobe.d/cis-blocklist.conf")
			})

			It("is a 0644 root-owned file", func() {
				Expect(blocklist.Path).To(Equal(bundled.CISModprobeBlocklistPath))
				Expect(blocklist.Permissions).To(Equal(uint32(0o644)))
				Expect(blocklist.Owner).To(BeZero())
				Expect(blocklist.Group).To(BeZero())
			})

			It("redirects every filesystem the benchmark lists to /bin/false", func() {
				for _, mod := range []string{"cramfs", "freevxfs", "jffs2", "hfs", "hfsplus", "udf"} {
					Expect(blocklist.Content).To(ContainSubstring("install " + mod + " /bin/false"))
				}
			})

			It("does not blocklist anything else", func() {
				var installs []string
				for _, line := range strings.Split(blocklist.Content, "\n") {
					if strings.HasPrefix(line, "install ") {
						installs = append(installs, line)
					}
				}
				Expect(installs).To(HaveLen(6))
			})

			It("is not gated on a service manager", func() {
				for _, st := range result {
					for _, f := range st.Files {
						if f.Path == bundled.CISModprobeBlocklistPath {
							Expect(st.OnlyIfServiceManager).To(BeEmpty())
						}
					}
				}
			})
		})

		Describe("the remote login warning banner", func() {
			var banner schema.File

			BeforeEach(func() {
				banner = fileByPath(result, "/etc/issue.net")
			})

			It("is a 0644 root-owned file", func() {
				Expect(banner.Path).To(Equal(bundled.IssueNetPath))
				Expect(banner.Permissions).To(Equal(uint32(0o644)))
				Expect(banner.Owner).To(BeZero())
				Expect(banner.Group).To(BeZero())
			})

			It("warns about unauthorized use and monitoring", func() {
				Expect(banner.Content).To(ContainSubstring("authorized users only"))
				Expect(banner.Content).To(ContainSubstring("monitored"))
			})

			It("leaks no system information", func() {
				// What the benchmark's audit greps for: the agetty
				// escapes that expand to the OS, release and kernel,
				// plus the /etc/os-release ID of any base we build on.
				Expect(banner.Content).ToNot(MatchRegexp(`\\[mrsv]`))
				for _, id := range []string{"kairos", "ubuntu", "debian", "fedora", "alpine", "rocky", "almalinux", "opensuse", "sles", "hadron"} {
					Expect(strings.ToLower(banner.Content)).ToNot(ContainSubstring(id))
				}
			})

			It("leaves /etc/issue alone", func() {
				for _, st := range result {
					for _, f := range st.Files {
						Expect(f.Path).ToNot(Equal("/etc/issue"))
						Expect(f.Path).ToNot(HavePrefix("/etc/issue.d/"))
					}
				}
			})
		})

		Describe("the account database permissions", func() {
			It("tightens every database and backup the benchmark covers", func() {
				for _, path := range []string{
					"/etc/passwd", "/etc/group", "/etc/shadow", "/etc/gshadow",
					"/etc/passwd-", "/etc/group-", "/etc/shadow-", "/etc/gshadow-",
				} {
					Expect(commandsFor(result, path)).To(
						ConsistOf(MatchRegexp(`^chmod \S+ `+regexp.QuoteMeta(path)+`$`)),
						"expected a single chmod guarded on "+path,
					)
				}
			})

			It("strips world access from the shadow files", func() {
				for _, path := range []string{"/etc/shadow", "/etc/gshadow"} {
					Expect(chmodMode(result, path)).To(dropsReadFor("o"))
				}
			})

			It("keeps group read on the shadow files so setgid unix_chkpwd still works", func() {
				// Debian-family and Alpine bases ship /etc/shadow as
				// root:shadow 0640 and PAM reads it through that group.
				for _, path := range []string{"/etc/shadow", "/etc/gshadow"} {
					Expect(chmodMode(result, path)).ToNot(dropsReadFor("g"))
				}
			})

			It("does not take /etc/passwd or /etc/group below world-readable", func() {
				// Too much reads them by name for 0600 to be survivable.
				for _, path := range []string{"/etc/passwd", "/etc/group"} {
					Expect(chmodMode(result, path)).ToNot(dropsReadFor("o"))
				}
			})

			It("takes the backups down to owner-only", func() {
				for _, path := range []string{
					"/etc/passwd-", "/etc/group-", "/etc/shadow-", "/etc/gshadow-",
				} {
					Expect(chmodMode(result, path)).To(dropsReadFor("g"))
					Expect(chmodMode(result, path)).To(dropsReadFor("o"))
				}
			})

			It("only ever subtracts bits", func() {
				for _, st := range result {
					for _, cmd := range st.Commands {
						if strings.HasPrefix(cmd, "chmod ") {
							Expect(strings.Fields(cmd)[1]).ToNot(ContainSubstring("+"))
						}
					}
				}
			})

			It("guards the chmod so a missing backup does not fail the build", func() {
				for _, st := range result {
					for _, cmd := range st.Commands {
						if strings.HasPrefix(cmd, "chmod ") {
							Expect(st.If).To(HavePrefix("test -f /etc/"))
						}
					}
				}
			})

			It("never writes the account databases as yip file entries", func() {
				// A file entry would create a missing backup as an
				// empty account database.
				for _, st := range result {
					for _, f := range st.Files {
						Expect(f.Path).ToNot(HavePrefix("/etc/passwd"))
						Expect(f.Path).ToNot(HavePrefix("/etc/group"))
						Expect(f.Path).ToNot(HavePrefix("/etc/shadow"))
						Expect(f.Path).ToNot(HavePrefix("/etc/gshadow"))
					}
				}
			})
		})
	})

	Context("when the skip step is configured", func() {
		var previous []string

		BeforeEach(func() {
			previous = config.DefaultConfig.SkipSteps
			config.DefaultConfig.SkipSteps = []string{values.CISHardeningStep}
		})

		AfterEach(func() {
			config.DefaultConfig.SkipSteps = previous
		})

		It("returns no stages", func() {
			Expect(stages.GetCISHardeningStage(values.System{}, log)).To(BeEmpty())
		})
	})
})
