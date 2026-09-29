package stages_test

import (
	"os"
	"os/exec"
	"path/filepath"
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

// dropsBitFor builds a matcher for a symbolic chmod expression that takes the
// given permission bit away from the given who-class (or from `a`). Matching
// on substrings is not enough: "g-r" does not appear in "go-rwx" even though
// that clause does clear group read.
func dropsBitFor(who, bit string) OmegaMatcher {
	return MatchRegexp(`(` + who + `|a)[ugoa]*-[a-z]*` + bit)
}

// dropsReadFor builds a matcher for a symbolic chmod expression that takes the
// read bit away from the given who-class (or from `a`).
func dropsReadFor(who string) OmegaMatcher {
	return dropsBitFor(who, "r")
}

// dropsExecFor builds a matcher for a symbolic chmod expression that takes the
// execute bit away from the given who-class (or from `a`).
func dropsExecFor(who string) OmegaMatcher {
	return dropsBitFor(who, "x")
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

		Describe("the sysctl hardening drop-in", func() {
			var sysctl schema.File

			BeforeEach(func() {
				sysctl = fileByPath(result, "/etc/sysctl.d/99-kairos-cis.conf")
			})

			It("is a 0644 root-owned file", func() {
				Expect(sysctl.Path).To(Equal(bundled.CISSysctlPath))
				Expect(sysctl.Permissions).To(Equal(uint32(0o644)))
				Expect(sysctl.Owner).To(BeZero())
				Expect(sysctl.Group).To(BeZero())
			})

			It("sets every key the issue enumerates", func() {
				for _, kv := range []string{
					"kernel.randomize_va_space = 2",
					"net.ipv4.conf.all.rp_filter = 2",
					"net.ipv4.conf.default.rp_filter = 2",
					"net.ipv4.tcp_syncookies = 1",
					"net.ipv4.conf.all.accept_source_route = 0",
					"net.ipv4.conf.all.accept_redirects = 0",
					"net.ipv4.conf.all.send_redirects = 0",
					"net.ipv6.conf.all.accept_ra = 0",
					"net.ipv6.conf.all.accept_redirects = 0",
				} {
					Expect(sysctl.Content).To(ContainSubstring(kv))
				}
			})

			It("orders after the base distro drop-ins with a 99- prefix", func() {
				Expect(sysctl.Path).To(HavePrefix("/etc/sysctl.d/99-"))
			})
		})

		Describe("the audit rules drop-in", func() {
			var rules schema.File

			BeforeEach(func() {
				rules = fileByPath(result, "/etc/audit/rules.d/50-kairos.rules")
			})

			It("is a 0640 root-owned file so CIS 4.1.4.5 stays green", func() {
				Expect(rules.Path).To(Equal(bundled.CISAuditRulesPath))
				Expect(rules.Permissions).To(Equal(uint32(0o640)))
				Expect(rules.Owner).To(BeZero())
				Expect(rules.Group).To(BeZero())
			})

			It("watches every account database the identity control names", func() {
				for _, path := range []string{
					"/etc/group", "/etc/passwd", "/etc/gshadow", "/etc/shadow",
				} {
					Expect(rules.Content).To(MatchRegexp(`-w ` + regexp.QuoteMeta(path) + ` -p wa -k identity`))
				}
			})

			It("audits time, network-environment, MAC, login, session and DAC events", func() {
				for _, key := range []string{
					"time-change", "system-locale", "MAC-policy",
					"logins", "session", "perm_mod", "access",
					"mounts", "delete", "scope", "modules",
				} {
					Expect(rules.Content).To(
						SatisfyAny(
							ContainSubstring("-k "+key),
							ContainSubstring("key="+key),
						),
						"expected key "+key+" in rules",
					)
				}
			})

			It("pairs each syscall rule with a b32 variant so 32-bit compat calls are caught", func() {
				// The 32-bit ABI on amd64 (CONFIG_IA32_EMULATION) and
				// aarch64 (CONFIG_COMPAT) reaches the same syscall
				// numbers under arch=b32, so a b64-only rule leaves
				// that path unaudited. The baseline pairs every
				// -F arch=b64 -S ... rule with the same syscall list
				// under -F arch=b32.
				b64 := regexp.MustCompile(`(?m)^-a always,exit -F arch=b64 (.*)$`)
				for _, m := range b64.FindAllStringSubmatch(rules.Content, -1) {
					b32 := "-a always,exit -F arch=b32 " + m[1]
					Expect(rules.Content).To(ContainSubstring(b32),
						"missing b32 pair for: "+m[0])
				}
			})

			It("locks the config with -e 2 as the last non-blank line", func() {
				lines := strings.Split(strings.TrimRight(rules.Content, "\n"), "\n")
				var last string
				for i := len(lines) - 1; i >= 0; i-- {
					if strings.TrimSpace(lines[i]) != "" {
						last = strings.TrimSpace(lines[i])
						break
					}
				}
				Expect(last).To(Equal("-e 2"))
			})
		})

		Describe("the auditd enable stage", func() {
			It("enables auditd only where the systemd unit is present", func() {
				var found bool
				for _, st := range result {
					for _, u := range st.Systemctl.Enable {
						if u == "auditd" {
							found = true
							Expect(st.OnlyIfServiceManager).To(Equal("systemd"))
							Expect(st.If).To(ContainSubstring("auditd.service"))
						}
					}
				}
				Expect(found).To(BeTrue(), "expected an Enable entry for auditd")
			})
		})

		Describe("the Alpine openrc auditd drop-in", func() {
			var confd schema.File
			var stage schema.Stage

			BeforeEach(func() {
				confd = fileByPath(result, "/etc/conf.d/auditd")
				for _, st := range result {
					for _, f := range st.Files {
						if f.Path == bundled.CISAuditdConfDPath {
							stage = st
						}
					}
				}
			})

			It("is a 0644 root-owned file", func() {
				Expect(confd.Path).To(Equal(bundled.CISAuditdConfDPath))
				Expect(confd.Permissions).To(Equal(uint32(0o644)))
				Expect(confd.Owner).To(BeZero())
				Expect(confd.Group).To(BeZero())
			})

			It("gates on Alpine only", func() {
				Expect(stage.OnlyIfOs).To(Equal(values.AlpineRegex))
			})

			It("points RULEFILE_STARTUP at the CIS rules drop-in", func() {
				Expect(confd.Content).To(ContainSubstring("RULEFILE_STARTUP=" + bundled.CISAuditRulesPath))
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

			It("clears the execute bit for every class on every account file and its backup", func() {
				// CIS 6.1.x requires none of these ever be executable,
				// regardless of which read/write bits a given base ships.
				for _, path := range []string{
					"/etc/passwd", "/etc/group", "/etc/shadow", "/etc/gshadow",
					"/etc/passwd-", "/etc/group-", "/etc/shadow-", "/etc/gshadow-",
				} {
					mode := chmodMode(result, path)
					Expect(mode).To(dropsExecFor("u"), "expected owner execute cleared on "+path)
					Expect(mode).To(dropsExecFor("g"), "expected group execute cleared on "+path)
					Expect(mode).To(dropsExecFor("o"), "expected other execute cleared on "+path)
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
				accountDBs := map[string]struct{}{}
				for _, p := range []string{
					"/etc/passwd", "/etc/group", "/etc/shadow", "/etc/gshadow",
					"/etc/passwd-", "/etc/group-", "/etc/shadow-", "/etc/gshadow-",
				} {
					accountDBs[p] = struct{}{}
				}
				for _, st := range result {
					for _, cmd := range st.Commands {
						if !strings.HasPrefix(cmd, "chmod ") {
							continue
						}
						fields := strings.Fields(cmd)
						if len(fields) != 3 {
							continue
						}
						if _, ok := accountDBs[fields[2]]; !ok {
							continue
						}
						Expect(st.If).To(HavePrefix("test -f /etc/"))
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

		Describe("the pwquality password policy", func() {
			var pwq schema.File

			BeforeEach(func() {
				pwq = fileByPath(result, "/etc/security/pwquality.conf")
			})

			It("is a 0644 root-owned file", func() {
				Expect(pwq.Path).To(Equal(bundled.CISPwqualityPath))
				Expect(pwq.Permissions).To(Equal(uint32(0o644)))
				Expect(pwq.Owner).To(BeZero())
				Expect(pwq.Group).To(BeZero())
			})

			It("enforces the CIS 5.4.1 minimum length and character classes", func() {
				for _, kv := range []string{
					"minlen = 14",
					"dcredit = -1",
					"ucredit = -1",
					"ocredit = -1",
					"lcredit = -1",
				} {
					Expect(pwq.Content).To(ContainSubstring(kv))
				}
			})
		})

		Describe("the faillock lockout policy", func() {
			var fl schema.File

			BeforeEach(func() {
				fl = fileByPath(result, "/etc/security/faillock.conf")
			})

			It("is a 0644 root-owned file", func() {
				Expect(fl.Path).To(Equal(bundled.CISFaillockPath))
				Expect(fl.Permissions).To(Equal(uint32(0o644)))
				Expect(fl.Owner).To(BeZero())
				Expect(fl.Group).To(BeZero())
			})

			It("locks accounts after five failures with a 900s window", func() {
				for _, kv := range []string{
					"deny = 5",
					"unlock_time = 900",
					"fail_interval = 900",
					"even_deny_root",
				} {
					Expect(fl.Content).To(ContainSubstring(kv))
				}
			})
		})

		Describe("the login.defs aging and umask defaults", func() {
			var stage schema.Stage

			BeforeEach(func() {
				for _, st := range result {
					if st.If == "test -f /etc/login.defs" {
						stage = st
					}
				}
				Expect(stage.Commands).ToNot(BeEmpty(), "expected a login.defs stage")
			})

			It("pins every key CIS 5.4.1 and 5.4.5 require", func() {
				joined := strings.Join(stage.Commands, "\n")
				for _, kv := range []string{
					"PASS_MAX_DAYS", "365",
					"PASS_MIN_DAYS", "1",
					"PASS_WARN_AGE", "7",
					"UMASK", "027",
					"ENCRYPT_METHOD", "SHA512",
				} {
					Expect(joined).To(ContainSubstring(kv))
				}
			})

			It("rewrites the existing line rather than appending blindly", func() {
				for _, cmd := range stage.Commands {
					Expect(cmd).To(ContainSubstring("sed -i"))
					Expect(cmd).To(ContainSubstring("printf"))
				}
			})

			It("compares the shipped value against the CIS floor and skips when the base is already stricter", func() {
				// Every command has to read the current value and branch
				// on whether it is already at least as strict as CIS;
				// otherwise the stage would loosen a base image's
				// stricter policy (Hadron ships PASS_MAX_DAYS 60 and
				// UMASK 077, both tighter than CIS).
				for _, cmd := range stage.Commands {
					Expect(cmd).To(ContainSubstring(`cur=$(awk`),
						"login.defs command missing current-value read: "+cmd)
					Expect(cmd).To(SatisfyAny(
						ContainSubstring(`[ "$cur" -le`),
						ContainSubstring(`[ "$cur" -ge`),
						ContainSubstring(`[ -n "$cur" ]`),
					), "login.defs command missing tighten-only guard: "+cmd)
				}
			})

			It("uses printf and not echo so backslash-t stays a real tab", func() {
				// echo '\t' writes a literal backslash-t under most
				// /bin/sh implementations; only printf expands it.
				// The Hadron QA failure was exactly this.
				for _, cmd := range stage.Commands {
					Expect(cmd).ToNot(MatchRegexp(`echo '[^']*\\t`),
						"login.defs command uses echo with a literal \\t which will not expand: "+cmd)
				}
			})

			It("guards the trailing newline before appending a new key", func() {
				// Hadron's stock /etc/login.defs has no trailing
				// newline. Without a guard, the appended key glues onto
				// the last line and corrupts it.
				for _, cmd := range stage.Commands {
					Expect(cmd).To(ContainSubstring(`tail -c1`),
						"login.defs command missing trailing-newline guard: "+cmd)
				}
			})

			It("only matches live settings so comment prose is left alone", func() {
				// A pattern like `#?[[:space:]]*KEY` also matches
				// `# UMASK is the default umask value...`; sed'ing that
				// turns the prose into a duplicate live setting.
				for _, cmd := range stage.Commands {
					Expect(cmd).ToNot(ContainSubstring("#?"),
						"login.defs detection must not accept commented lines: "+cmd)
					Expect(cmd).ToNot(MatchRegexp(`\^\[\[:space:\]\]\*#`),
						"login.defs sed must not match comment lines: "+cmd)
				}
			})

			Context("when the shell runs the generated commands", func() {
				// The unit-level tests above prove the command shape;
				// this one runs it against tempfiles that reproduce
				// each base image's shipped /etc/login.defs to catch
				// regressions that only show up under sh.
				var tmpdir string

				BeforeEach(func() {
					var err error
					tmpdir, err = os.MkdirTemp("", "login-defs-*")
					Expect(err).ToNot(HaveOccurred())
				})

				AfterEach(func() {
					_ = os.RemoveAll(tmpdir)
				})

				runAgainst := func(initial string) string {
					path := filepath.Join(tmpdir, "login.defs")
					Expect(os.WriteFile(path, []byte(initial), 0o644)).To(Succeed())
					for _, cmd := range stage.Commands {
						// The stage writes /etc/login.defs; the test
						// runs the same command against the tempfile.
						scoped := strings.ReplaceAll(cmd, "/etc/login.defs", path)
						out, err := exec.Command("sh", "-c", scoped).CombinedOutput()
						Expect(err).ToNot(HaveOccurred(), "sh -c failed: %s\n%s", scoped, out)
					}
					got, err := os.ReadFile(path)
					Expect(err).ToNot(HaveOccurred())
					return string(got)
				}

				It("appends ENCRYPT_METHOD as a real tabbed line to a Hadron file with no trailing newline", func() {
					initial := "USERGROUPS_ENAB yes\nPREVENT_NO_AUTH superuser"
					got := runAgainst(initial)
					Expect(got).To(ContainSubstring("\nPREVENT_NO_AUTH superuser\n"),
						"missing-newline base image had its last line corrupted: %q", got)
					Expect(got).To(MatchRegexp(`(?m)^ENCRYPT_METHOD\tSHA512$`),
						"ENCRYPT_METHOD not appended as a live tab-separated line: %q", got)
					Expect(got).ToNot(ContainSubstring(`\t`),
						"literal backslash-t leaked into the file: %q", got)
				})

				It("keeps a base image's stricter value on Hadron-shape inputs and only tightens weaker keys", func() {
					// Hadron ships UMASK 077 (stricter than 027),
					// PASS_MAX_DAYS 60 (stricter than 365) and
					// PASS_MIN_DAYS 0 (weaker than 1). The stage must
					// leave the two stricter keys alone and only raise
					// PASS_MIN_DAYS.
					initial := "UMASK 077\nPASS_MAX_DAYS 60\nPASS_MIN_DAYS 0\nPASS_WARN_AGE 7\nENCRYPT_METHOD SHA512\n"
					got := runAgainst(initial)
					Expect(got).To(MatchRegexp(`(?m)^UMASK 077$`),
						"UMASK 077 was loosened: %q", got)
					Expect(got).To(MatchRegexp(`(?m)^PASS_MAX_DAYS 60$`),
						"PASS_MAX_DAYS 60 was loosened: %q", got)
					Expect(got).To(MatchRegexp(`(?m)^PASS_MIN_DAYS\t1$`),
						"PASS_MIN_DAYS 0 was not tightened to 1: %q", got)
					Expect(got).To(MatchRegexp(`(?m)^PASS_WARN_AGE 7$`),
						"PASS_WARN_AGE 7 was churned unnecessarily: %q", got)
					Expect(got).To(MatchRegexp(`(?m)^ENCRYPT_METHOD SHA512$`),
						"ENCRYPT_METHOD SHA512 was churned unnecessarily: %q", got)
					Expect(strings.Count(got, "UMASK")).To(Equal(1))
					Expect(strings.Count(got, "PASS_MAX_DAYS")).To(Equal(1))
				})

				It("tightens weaker values on Ubuntu-shape inputs", func() {
					// Ubuntu ships PASS_MAX_DAYS 99999 (weaker than CIS
					// 365), UMASK 022 (weaker than 027), PASS_MIN_DAYS 0
					// (weaker than 1). All three must be raised.
					initial := "PASS_MAX_DAYS 99999\nPASS_MIN_DAYS 0\nPASS_WARN_AGE 7\nUMASK 022\n"
					got := runAgainst(initial)
					Expect(got).To(MatchRegexp(`(?m)^PASS_MAX_DAYS\t365$`))
					Expect(got).To(MatchRegexp(`(?m)^PASS_MIN_DAYS\t1$`))
					Expect(got).To(MatchRegexp(`(?m)^UMASK\t027$`))
				})

				It("leaves commented documentation intact and does not create duplicates from prose", func() {
					initial := "# UMASK is the default umask value...\n# PASS_MAX_DAYS Maximum number of days...\nUSERGROUPS_ENAB yes\n"
					got := runAgainst(initial)
					Expect(got).To(ContainSubstring("# UMASK is the default umask value..."),
						"comment prose was rewritten: %q", got)
					Expect(got).To(ContainSubstring("# PASS_MAX_DAYS Maximum number of days..."),
						"comment prose was rewritten: %q", got)
					Expect(strings.Count(got, "\nUMASK")).To(Equal(1),
						"UMASK live line appeared more than once: %q", got)
					Expect(strings.Count(got, "\nPASS_MAX_DAYS")).To(Equal(1),
						"PASS_MAX_DAYS live line appeared more than once: %q", got)
				})

				It("appends when only a commented-out setting is present", func() {
					initial := "#UMASK 022\n#PASS_MAX_DAYS 99999\n"
					got := runAgainst(initial)
					Expect(got).To(ContainSubstring("#UMASK 022"),
						"commented default was rewritten: %q", got)
					Expect(got).To(MatchRegexp(`(?m)^UMASK\t027$`),
						"missing appended UMASK when only commented default present: %q", got)
				})
			})
		})

		Describe("the cron and at directory permissions", func() {
			It("tightens every path CIS 5.1 names", func() {
				for _, path := range []string{
					"/etc/crontab",
					"/etc/cron.hourly", "/etc/cron.daily",
					"/etc/cron.weekly", "/etc/cron.monthly",
					"/etc/cron.d",
					"/etc/cron.allow", "/etc/cron.deny",
					"/etc/at.allow", "/etc/at.deny",
				} {
					var found bool
					for _, st := range result {
						if st.If != "test -e "+path {
							continue
						}
						found = true
						Expect(st.Commands).To(ContainElement(MatchRegexp(`^chown root:root ` + regexp.QuoteMeta(path) + `$`)))
						Expect(st.Commands).To(ContainElement(MatchRegexp(`^chmod 0[67][04]0 ` + regexp.QuoteMeta(path) + `$`)))
					}
					Expect(found).To(BeTrue(), "expected a stage for "+path)
				}
			})

			It("guards every cron chmod on the path existing so a missing subsystem does not fail the build", func() {
				for _, st := range result {
					for _, cmd := range st.Commands {
						if !strings.HasPrefix(cmd, "chmod 0") {
							continue
						}
						fields := strings.Fields(cmd)
						if len(fields) != 3 {
							continue
						}
						if !strings.HasPrefix(fields[2], "/etc/cron") && !strings.HasPrefix(fields[2], "/etc/at.") && fields[2] != "/etc/crontab" {
							continue
						}
						Expect(st.If).To(HavePrefix("test -e "))
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
