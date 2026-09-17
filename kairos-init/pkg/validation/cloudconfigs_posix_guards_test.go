package validation_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// yip runs a stage's `if` through `sh -c` (yip/pkg/console/console.go), so a
// guard may only use POSIX shell. `/bin/sh` is dash on the Debian family, bash
// on the RPM family and busybox ash on Alpine, and the three disagree: bash and
// busybox take `==` inside `[ ]` and provide `[[`, dash does neither. A guard
// that leans on either silently evaluates false on the images whose `/bin/sh`
// is dash, and a false guard is indistinguishable from a stage that does not
// apply.

var (
	// doubleBracket matches the bash `[[` / `]]` keywords as whole words.
	doubleBracket = regexp.MustCompile(`(^|\s)(\[\[|]])(\s|$)`)
	// bracketTest captures the body of a single-bracket `[ ... ]` test.
	bracketTest = regexp.MustCompile(`\[([^\[\]]*)]`)
	// equalEqual matches `==` used as a test operator.
	equalEqual = regexp.MustCompile(`(^|\s)==(\s|$)`)
)

// bundledGuard is one `if:` expression and where it came from.
type bundledGuard struct {
	file string
	expr string
}

func (g bundledGuard) String() string {
	return fmt.Sprintf("%s: %s", g.file, g.expr)
}

// collectGuards walks a decoded cloudconfig and returns every `if` expression
// in it. Nested cloud-configs written out through a `content:` block (12_nvidia
// lays one down for the Jetson) are descended into as well, since yip runs
// those guards the same way.
func collectGuards(file string, node any, out *[]bundledGuard) {
	switch v := node.(type) {
	case map[string]any:
		for key, val := range v {
			if key == "if" {
				if expr, ok := val.(string); ok {
					*out = append(*out, bundledGuard{file: file, expr: expr})
				}
				continue
			}
			if key == "content" {
				if nested, ok := val.(string); ok && strings.Contains(nested, "stages:") {
					var doc any
					if yaml.Unmarshal([]byte(nested), &doc) == nil {
						collectGuards(file+" (embedded cloud-config)", doc, out)
					}
					continue
				}
			}
			collectGuards(file, val, out)
		}
	case []any:
		for _, item := range v {
			collectGuards(file, item, out)
		}
	}
}

// bundledGuards returns every guard in every bundled cloudconfig.
func bundledGuards() []bundledGuard {
	dir := filepath.Join("..", "bundled", "cloudconfigs")
	entries, err := os.ReadDir(dir)
	Expect(err).NotTo(HaveOccurred())

	var guards []bundledGuard
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		Expect(err).NotTo(HaveOccurred(), entry.Name())

		var doc any
		Expect(yaml.Unmarshal(content, &doc)).To(Succeed(), entry.Name())
		collectGuards(entry.Name(), doc, &guards)
	}
	sort.Slice(guards, func(i, j int) bool { return guards[i].String() < guards[j].String() })
	return guards
}

// posixShells returns the shells present on this machine, as argv prefixes.
// `sh` is whichever shell the box points it at, so on a Debian-family runner
// the first entry is dash and the matrix below is the real thing rather than a
// stand-in.
func posixShells() map[string][]string {
	shells := map[string][]string{}
	for _, name := range []string{"sh", "dash", "bash", "ash"} {
		if path, err := exec.LookPath(name); err == nil {
			shells[name] = []string{path, "-c"}
		}
	}
	if path, err := exec.LookPath("busybox"); err == nil {
		shells["busybox sh"] = []string{path, "sh", "-c"}
	}
	return shells
}

// stubAgentDir writes a `kairos-agent` that answers `state get <key>` from the
// supplied table, and returns a PATH containing only it. Anything the guard
// reaches for beyond the stub is therefore absent, which is what makes `[[`
// resolve to nothing on a dash box: busybox installs `[[` as an applet, so a
// guard that looks broken can pass by borrowing it off the real PATH.
func stubAgentDir(state map[string]string) string {
	dir := GinkgoT().TempDir()

	script := "#!/bin/sh\ncase \"$3\" in\n"
	keys := make([]string, 0, len(state))
	for key := range state {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		script += fmt.Sprintf("  %s) printf '%%s\\n' %q ;;\n", key, state[key])
	}
	script += "  *) exit 1 ;;\nesac\n"

	Expect(os.WriteFile(filepath.Join(dir, "kairos-agent"), []byte(script), 0o755)).To(Succeed())
	return dir
}

// evalUnder runs expr under every shell on the box with only the stub on PATH,
// and returns whether the guard fired, per shell.
func evalUnder(expr string, state map[string]string) map[string]bool {
	dir := stubAgentDir(state)

	fired := map[string]bool{}
	for name, argv := range posixShells() {
		cmd := exec.Command(argv[0], append(argv[1:], expr)...) //nolint:gosec // fixed argv, test-only
		cmd.Env = []string{"PATH=" + dir}
		out, err := cmd.CombinedOutput()
		if err == nil {
			fired[name] = true
			continue
		}
		_, isExit := err.(*exec.ExitError)
		Expect(isExit).To(BeTrue(), "%s could not run the guard: %v", name, err)
		// A guard that fails to parse or calls a missing builtin is not a
		// false guard, it is a broken one. Surface the message rather than
		// recording a quiet false.
		Expect(string(out)).To(BeEmpty(), "%s errored on the guard %q", name, expr)
		fired[name] = false
	}
	return fired
}

var _ = Describe("Bundled cloudconfig guards", func() {
	// The lint is the invariant: it holds whatever this machine points
	// /bin/sh at, where the shell matrix below can only test the shells that
	// happen to be installed.
	It("only uses POSIX shell", func() {
		guards := bundledGuards()
		Expect(guards).NotTo(BeEmpty(), "no guards found, the collector is broken")

		for _, guard := range guards {
			Expect(doubleBracket.MatchString(guard.expr)).To(BeFalse(),
				"%s uses the bash [[ keyword, which dash does not have", guard)

			for _, test := range bracketTest.FindAllStringSubmatch(guard.expr, -1) {
				Expect(equalEqual.MatchString(test[1])).To(BeFalse(),
					"%s uses == inside [ ], which dash rejects; use =", guard)
			}
		}
	})

	It("finds the guards in embedded cloud-configs too", func() {
		var files []string
		for _, guard := range bundledGuards() {
			files = append(files, guard.file)
		}
		Expect(files).To(ContainElement("12_nvidia.yaml (embedded cloud-config)"))
	})

	Describe("21_kcrypt.yaml", func() {
		// The guard gates the only stage that refreshes the OEM copy of the
		// kcrypt discovery plugins after an upgrade, and /oem/system/discovery
		// is one of the paths kcrypt searches (sdk/kcrypt/bus/bus.go). With
		// `==` in it the stage never ran on Ubuntu or Debian.
		var guard string

		BeforeEach(func() {
			guard = readStage("21_kcrypt.yaml", "after-upgrade")[0].If
			Expect(guard).NotTo(BeEmpty())
		})

		It("fires when the OEM partition was found", func() {
			for shell, fired := range evalUnder(guard, map[string]string{"oem.found": "true"}) {
				Expect(fired).To(BeTrue(), "%s did not run the stage with oem.found=true", shell)
			}
		})

		It("does not fire when there is no OEM partition, or no answer at all", func() {
			for _, found := range []string{"false", ""} {
				for shell, fired := range evalUnder(guard, map[string]string{"oem.found": found}) {
					Expect(fired).To(BeFalse(), "%s ran the stage with oem.found=%q", shell, found)
				}
			}
		})
	})

	Describe("00_rootfs.yaml", func() {
		// Two boot.before stages mount tmp and bpffs on Alpine only. They ran
		// under busybox ash and bash but printed "[[: not found" under dash on
		// every boot of every Debian-family image.
		var guards []string

		BeforeEach(func() {
			guards = nil
			for _, stage := range readStage("00_rootfs.yaml", "boot.before") {
				if strings.Contains(strings.ToLower(stage.Name), "alpine") {
					guards = append(guards, stage.If)
				}
			}
			Expect(guards).To(HaveLen(2))
		})

		It("fires on an Alpine flavor", func() {
			for _, guard := range guards {
				for _, flavor := range []string{"alpine", "alpine-3.21"} {
					for shell, fired := range evalUnder(guard, map[string]string{"kairos.flavor": flavor}) {
						Expect(fired).To(BeTrue(), "%s did not match flavor %q for %q", shell, flavor, guard)
					}
				}
			}
		})

		It("does not fire on any other flavor", func() {
			for _, guard := range guards {
				for _, flavor := range []string{"ubuntu-24.04", "rocky-9", "opensuse-leap-15.6", ""} {
					for shell, fired := range evalUnder(guard, map[string]string{"kairos.flavor": flavor}) {
						Expect(fired).To(BeFalse(), "%s matched flavor %q for %q", shell, flavor, guard)
					}
				}
			}
		})
	})

	It("runs the matrix against more than one shell", func() {
		// Otherwise the two Describes above pass by testing nothing.
		Expect(len(posixShells())).To(BeNumerically(">=", 2), "found %v", posixShells())
	})
})
