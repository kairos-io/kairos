package bundled_test

import (
	"io/fs"
	"regexp"
	"strings"

	agentcmd "github.com/kairos-io/kairos/v4/agent/pkg/cmd"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// runStageInvocation matches the arguments of a `kairos-agent run-stage` call
// anywhere on a line: a yip command, a systemd ExecStart or a shell script.
var runStageInvocation = regexp.MustCompile(`kairos-agent run-stage(.*)$`)

// shellSeparator ends a command inside a shell line, so the words after it
// belong to the next command and not to run-stage, as in
// `sh -c 'while true; do kairos-agent run-stage reconcile; sleep 3600; done'`.
var shellSeparator = regexp.MustCompile(`;|&&|\|\|`)

type runStageCall struct {
	file string
	line string
	args []string
}

// bundledRunStageCalls returns every `kairos-agent run-stage` call written in
// the bundled cloud-configs, with its arguments split as the shell would for
// these simple, unescaped command lines, up to the end of that command.
func bundledRunStageCalls() []runStageCall {
	var calls []runStageCall
	err := fs.WalkDir(bundled.EmbeddedConfigs, "cloudconfigs", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := bundled.EmbeddedConfigs.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(raw), "\n") {
			match := runStageInvocation.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			args := []string{"run-stage"}
			own := shellSeparator.Split(match[1], 2)[0]
			for _, field := range strings.Fields(own) {
				args = append(args, strings.Trim(field, `"'`))
			}
			calls = append(calls, runStageCall{file: path, line: strings.TrimSpace(line), args: args})
		}
		return nil
	})
	Expect(err).ToNot(HaveOccurred())
	return calls
}

var _ = Describe("kairos-agent run-stage calls in the bundled cloud-configs", func() {
	It("finds the datasource calls, so the check below covers them", func() {
		var datasource []runStageCall
		for _, call := range bundledRunStageCalls() {
			if call.file == "cloudconfigs/00_datasource.yaml" {
				datasource = append(datasource, call)
			}
		}
		Expect(datasource).To(HaveLen(2))
	})

	// The agent stops reading flags at the first positional argument, so a
	// flag written after the stage name reaches run-stage as an extra
	// argument and the call fails before running anything.
	It("passes every flag before the stage name, so the agent accepts the call", func() {
		calls := bundledRunStageCalls()
		Expect(calls).ToNot(BeEmpty())

		for _, call := range calls {
			root := agentcmd.NewCommand()
			command, rest, err := root.Find(call.args)
			Expect(err).ToNot(HaveOccurred(), "%s: %s", call.file, call.line)
			Expect(command.Name()).To(Equal("run-stage"), "%s: %s", call.file, call.line)

			Expect(command.ParseFlags(rest)).To(Succeed(), "%s: %s", call.file, call.line)
			// run-stage takes the stage name as its only argument and refuses
			// any other count with "Stage to run missing".
			Expect(command.Flags().Args()).To(HaveLen(1), "%s: %s", call.file, call.line)
		}
	})
})
