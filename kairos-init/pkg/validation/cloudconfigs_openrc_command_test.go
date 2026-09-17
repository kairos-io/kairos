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
var openrcShippedBinaries = []string{constants.AgentDefaultPath}

var _ = Describe("Bundled cloudconfigs openrc services", func() {
	It("points every command= at a binary the image ships", func() {
		dir := filepath.Join("..", "bundled", "cloudconfigs")
		entries, err := os.ReadDir(dir)
		Expect(err).NotTo(HaveOccurred(), "read bundled cloudconfigs directory")

		found := 0
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			content, err := os.ReadFile(filepath.Join(dir, name))
			Expect(err).NotTo(HaveOccurred(), "read cloudconfig %s", name)

			for lineNum, line := range strings.Split(string(content), "\n") {
				match := openrcCommandRe.FindStringSubmatch(line)
				if match == nil {
					continue
				}
				found++

				command := match[1]
				where := func() string {
					return name + ":" + strconv.Itoa(lineNum+1)
				}

				// A command= carrying arguments is resolved by its first word
				// only, so the arguments belong in command_args.
				Expect(strings.Fields(command)).To(HaveLen(1),
					"%s: command=%q must be a single binary, put arguments in command_args", where(), command)
				Expect(command).To(HavePrefix("/"),
					"%s: command=%q must be an absolute path", where(), command)
				Expect(openrcShippedBinaries).To(ContainElement(command),
					"%s: command=%q is not a binary the image ships", where(), command)
			}
		}

		// Guard against the assertions above passing because the regexp
		// stopped matching anything.
		Expect(found).To(BeNumerically(">=", 2),
			"expected the bundled openrc services to declare command=")
	})
})
