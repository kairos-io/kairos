package validation_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// openrcCommandRe matches the command= assignment of an openrc-run service
// embedded in a bundled cloud-config.
var openrcCommandRe = regexp.MustCompile(`^\s*command="([^"]*)"\s*$`)

// openrcShippedBinaries lists the absolute paths an openrc service is allowed
// to name. supervise-daemon and start-stop-daemon both resolve command= and
// refuse to start when it is not there, so anything outside this set is a
// service that can never run. See kairos-io/kairos#4709.
//
// /bin/sh is the busybox shell every openrc image has: openrc only runs on the
// Alpine family here, since Hadron is systemd.
var openrcShippedBinaries = []string{constants.AgentDefaultPath, "/bin/sh"}

// openrcRespawnDelayRe and openrcRespawnMaxRe match the supervise-daemon
// respawn knobs of a service embedded in a bundled cloud-config.
var (
	openrcRespawnDelayRe = regexp.MustCompile(`^\s*respawn_delay=([0-9]+)\s*$`)
	openrcRespawnMaxRe   = regexp.MustCompile(`^\s*respawn_max=([0-9]+)\s*$`)
)

// openrcRespawnDelayCapSeconds is supervise-daemon's --respawn-delay-cap
// default, TM_SEC(30). openrc 0.62 and later clamp every respawn sleep to it
// whenever --respawn-delay-step is non-zero, which it always is here because
// sh/supervise-daemon.sh passes neither flag. A larger respawn_delay is
// therefore silently shortened to this on Alpine 3.23 while being honoured on
// Alpine 3.21, so it can never be the interval a service relies on.
const openrcRespawnDelayCapSeconds = 30

// eachCloudconfigLine calls fn for every line of every bundled cloud-config,
// with a "file:line" label for the failure message.
func eachCloudconfigLine(fn func(where, line string)) {
	dir := filepath.Join("..", "bundled", "cloudconfigs")
	entries, err := os.ReadDir(dir)
	Expect(err).NotTo(HaveOccurred(), "read bundled cloudconfigs directory")

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		content, err := os.ReadFile(filepath.Join(dir, name))
		Expect(err).NotTo(HaveOccurred(), "read cloudconfig %s", name)

		for lineNum, line := range strings.Split(string(content), "\n") {
			fn(name+":"+strconv.Itoa(lineNum+1), line)
		}
	}
}

var _ = Describe("Bundled cloudconfigs openrc services", func() {
	It("points every command= at a binary the image ships", func() {
		found := 0
		eachCloudconfigLine(func(where, line string) {
			match := openrcCommandRe.FindStringSubmatch(line)
			if match == nil {
				return
			}
			found++

			command := match[1]

			// A command= carrying arguments is resolved by its first word
			// only, so the arguments belong in command_args.
			Expect(strings.Fields(command)).To(HaveLen(1),
				"%s: command=%q must be a single binary, put arguments in command_args", where, command)
			Expect(command).To(HavePrefix("/"),
				"%s: command=%q must be an absolute path", where, command)
			Expect(openrcShippedBinaries).To(ContainElement(command),
				"%s: command=%q is not a binary the image ships", where, command)
		})

		// Guard against the assertions above passing because the regexp
		// stopped matching anything. One is the honest floor: #5070 retired
		// the kairos-webui service, so cos-setup-reconcile is the only openrc
		// service left that declares a command=.
		Expect(found).To(BeNumerically(">=", 1),
			"expected the bundled openrc services to declare command=")
	})

	// supervise-daemon's respawn knobs are not portable across the openrc
	// releases Kairos builds Alpine flavours from, so neither of them can be
	// load-bearing. Alpine 3.21 ships openrc 0.55.1, Alpine 3.23 ships 0.63.
	It("sets supervise-daemon respawn knobs both openrc releases agree on", func() {
		delays, maxes := 0, 0
		eachCloudconfigLine(func(where, line string) {
			if match := openrcRespawnDelayRe.FindStringSubmatch(line); match != nil {
				delays++
				delay, err := strconv.Atoi(match[1])
				Expect(err).NotTo(HaveOccurred(), where)

				// openrc 0.63 clamps the sleep to respawn_delay_cap, so a
				// larger delay is a different interval on the two releases.
				// Put the interval inside the supervised command instead.
				Expect(delay).To(BeNumerically("<=", openrcRespawnDelayCapSeconds),
					"%s: respawn_delay=%d is above openrc's %ds respawn_delay_cap, so it is honoured on Alpine 3.21 and clamped on 3.23",
					where, delay, openrcRespawnDelayCapSeconds)
			}

			if match := openrcRespawnMaxRe.FindStringSubmatch(line); match != nil {
				maxes++

				// openrc 0.55.1 defaults respawn_period to 0, so the respawn
				// counter never resets and the default respawn_max of 10
				// retires the service for the rest of the boot. 0 is
				// unlimited on both releases.
				Expect(match[1]).To(Equal("0"),
					"%s: respawn_max=%s retires the service after that many respawns on openrc 0.55.1, which never resets the counter; use 0",
					where, match[1])
			}
		})

		// Every supervise-daemon service that sets a delay has to set the
		// maximum too, since leaving it out is the broken default.
		Expect(maxes).To(Equal(delays),
			"every respawn_delay needs a respawn_max=0 next to it")
		Expect(delays).To(BeNumerically(">=", 1),
			"expected a bundled openrc service to set respawn_delay")
	})
})
