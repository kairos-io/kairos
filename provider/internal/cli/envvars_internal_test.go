package cli

import (
	"fmt"
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
)

// shellVariableName is the set of names a POSIX shell accepts in an assignment.
// A name outside it cannot be set with "NAME=value cmd" or exported, so a flag
// that names its environment fallback that way has no reachable fallback at
// all: the shell reads the whole word as a command name instead.
var shellVariableName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// collectEnvVars walks a command tree and returns one entry per declared
// environment fallback, labelled with the command path it was found on.
func collectEnvVars(path string, flags []cli.Flag, out map[string]string) {
	for _, f := range flags {
		d, ok := f.(cli.DocGenerationFlag)
		if !ok {
			continue
		}
		for _, name := range d.GetEnvVars() {
			out[name] = fmt.Sprintf("%s --%s", path, f.Names()[0])
		}
	}
}

func walkCommands(path string, cmds []*cli.Command, out map[string]string) {
	for _, c := range cmds {
		p := fmt.Sprintf("%s %s", path, c.Name)
		collectEnvVars(p, c.Flags, out)
		walkCommands(p, c.Subcommands, out)
	}
}

var _ = Describe("The environment fallbacks the provider CLI declares", func() {
	// NewApp resolves the --api default from a file on this machine and writes
	// it into the flag every API command shares, so put the flag back.
	BeforeEach(func() {
		original := apiFlagValue()
		DeferCleanup(func() { setAPIFlagDefault(original) })
	})

	It("names every one of them so a shell can set it", func() {
		app := NewApp()

		found := map[string]string{}
		collectEnvVars(app.Name, app.Flags, found)
		walkCommands(app.Name, app.Commands, found)

		Expect(found).NotTo(BeEmpty(), "no EnvVars at all means the walk is reading the wrong tree")

		for name, where := range found {
			Expect(shellVariableName.MatchString(name)).To(BeTrue(),
				"%s declares the environment fallback %q, which a shell cannot assign", where, name)
		}
	})

	It("gives --lease-dir the name edgevpn uses for the same flag", func() {
		app := NewApp()

		found := map[string]string{}
		walkCommands(app.Name, app.Commands, found)

		Expect(found).To(HaveKey("DHCPLEASEDIR"))
		Expect(found).NotTo(HaveKey("lease-dir"))
	})
})
