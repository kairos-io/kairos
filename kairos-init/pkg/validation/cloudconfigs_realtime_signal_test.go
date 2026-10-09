package validation_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// execLineRe captures the executable of a systemd Exec* directive, after the
// optional prefix characters systemd allows there ("-", "@", ":", "+", "!").
var execLineRe = regexp.MustCompile(`^\s*Exec(?:Start|StartPre|StartPost|Stop|StopPost|Reload|Condition)=[-@:+!]*(\S+)`)

// killBinaryRe matches an invocation of a standalone kill binary, by absolute
// path or by the bare name systemd resolves against its own search path.
var killBinaryRe = regexp.MustCompile(`(?:^|/)kill$`)

// realtimeSignalShells are the shells whose kill builtin understands the
// SIGRTMIN+n names. dash, which is /bin/sh on the Debian family, does not:
// it answers "invalid signal number or name: RTMIN+21".
var realtimeSignalShells = []string{"/bin/bash", "/usr/bin/bash"}

func cloudConfigLines() map[string][]string {
	dir := filepath.Join("..", "bundled", "cloudconfigs")
	entries, err := os.ReadDir(dir)
	Expect(err).NotTo(HaveOccurred(), "read bundled cloudconfigs directory")

	lines := map[string][]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		Expect(err).NotTo(HaveOccurred(), "read cloudconfig %s", entry.Name())
		lines[entry.Name()] = strings.Split(string(content), "\n")
	}
	return lines
}

var _ = Describe("Bundled cloudconfigs realtime signals", func() {
	// The units that quiet systemd's console while a full screen agent TUI
	// owns the tty used to exec /usr/bin/kill directly. The Hadron base image,
	// which every core and standard Kairos image is built on, ships procps-ng
	// and util-linux but neither of their kill binaries, so the line failed
	// with 203/EXEC and the leading "-" swallowed it. See #4784.
	It("never execs a standalone kill binary", func() {
		var offenders []string

		for name, lines := range cloudConfigLines() {
			for i, line := range lines {
				match := execLineRe.FindStringSubmatch(line)
				if match == nil {
					continue
				}
				if killBinaryRe.MatchString(match[1]) {
					offenders = append(offenders, name+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(line))
				}
			}
		}

		Expect(offenders).To(BeEmpty(),
			"the default base image ships no kill binary: send the signal from a shell builtin instead")
	})

	// SIGRTMIN is 34 against glibc and 35 against musl, so the number cannot
	// be inlined: SIGRTMIN+20 turns console status messages back on. The
	// symbolic name has to reach a shell that resolves it.
	It("sends every realtime signal from a shell that understands the name", func() {
		found := 0

		for name, lines := range cloudConfigLines() {
			for i, line := range lines {
				if !strings.Contains(line, "SIGRTMIN") {
					continue
				}
				where := name + ":" + strconv.Itoa(i+1)

				match := execLineRe.FindStringSubmatch(line)
				Expect(match).NotTo(BeNil(), "%s: realtime signal outside an Exec directive: %s", where, strings.TrimSpace(line))
				Expect(realtimeSignalShells).To(ContainElement(match[1]),
					"%s: %s cannot resolve SIGRTMIN+n", where, match[1])
				Expect(line).To(ContainSubstring(` -c "kill -SIGRTMIN`),
					"%s: the signal must be sent by the shell builtin", where)

				found++
			}
		}

		Expect(found).To(BeNumerically(">=", 8),
			"the recovery, reset and installer units each quiet and unquiet the console")
	})
})
