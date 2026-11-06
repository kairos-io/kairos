package debugbundle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/diagnostics"
)

// Runner runs an external command and returns its combined output. It is an
// interface so tests can inject a fake.
type Runner = diagnostics.Runner

// ExecRunner runs real commands via os/exec.
type ExecRunner struct{}

// Run executes name with args and returns combined stdout+stderr.
func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// Context carries install metadata included verbatim in installer-context.log.
type Context struct {
	AgentBin            string
	AgentArgs           []string
	Disk                string
	Source              string
	Version             string
	CloudConfigRedacted string
}

// contextGroup is what only the installer knows: which disk was picked, which
// source was going to be written to it, and the configuration it was given.
// The machine's own state comes from sdk/diagnostics, which `kairos-agent
// logs` collects too.
func contextGroup(c Context) diagnostics.Group {
	var b strings.Builder
	fmt.Fprintf(&b, "installer-version: %s\n", c.Version)
	fmt.Fprintf(&b, "boot-mode: %s\n", diagnostics.BootMode(diagnostics.OSFS{}))
	fmt.Fprintf(&b, "selected-disk: %s\n", c.Disk)
	fmt.Fprintf(&b, "install-source: %s\n", c.Source)
	fmt.Fprintf(&b, "agent-command: %s %s\n\n", c.AgentBin, strings.Join(c.AgentArgs, " "))

	b.WriteString("===== KAIROS_* environment =====\n")
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "KAIROS_") {
			b.WriteString(e + "\n")
		}
	}
	b.WriteString("\n===== rendered cloud-config (credentials redacted) =====\n")
	b.WriteString(c.CloudConfigRedacted + "\n\n")
	return diagnostics.Group{Name: "context", Body: b.String()}
}

// CollectExtras writes the grouped installer-*.log files into dir and returns
// the written file paths. Best-effort: a write error for one file does not
// stop the others; the first error (if any) is returned after attempting all
// of them.
func CollectExtras(r Runner, c Context, dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	groups := append(diagnostics.Collect(r, diagnostics.OSFS{}), contextGroup(c))
	var written []string
	var firstErr error
	for _, g := range groups {
		p := filepath.Join(dir, "installer-"+g.Name+".log")
		if err := os.WriteFile(p, []byte(g.Body), 0o644); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		written = append(written, p)
	}
	return written, firstErr
}
