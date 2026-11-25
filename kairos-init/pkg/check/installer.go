package check

import (
	"io/fs"
	"os/exec"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/plugins"
	"github.com/mudler/yip/pkg/schema"
	"github.com/twpayne/go-vfs/v5"
)

// rootVFS gives yip the os-release of the root. yip opens /etc/os-release to
// choose the package manager, and an absolute link in an unpacked image has to
// point into the image.
type rootVFS struct {
	vfs.FS
	root rootFS
}

func (v rootVFS) Open(name string) (fs.File, error) {
	return v.root.open(name)
}

// recordingConsole is a yip console that runs nothing and keeps the commands.
type recordingConsole struct {
	commands []string
}

func (c *recordingConsole) Run(command string, _ ...func(*exec.Cmd)) (string, error) {
	c.commands = append(c.commands, command)
	return "", nil
}

func (c *recordingConsole) Start(*exec.Cmd, ...func(*exec.Cmd)) error { return nil }

func (c *recordingConsole) RunTemplate([]string, string) error { return nil }

// packageManager is the package manager yip uses on the root, which is the one
// kairos-init installs packages with. It asks yip's own package step, so the
// answer is the one a build gets, and it runs nothing. It is empty when yip
// stops with "unknown package manager".
func packageManager(root rootFS) string {
	console := &recordingConsole{}
	stage := schema.Stage{Packages: schema.Packages{Install: []string{"kairos-check"}}}
	err := plugins.Packages(logger.NewNullLogger(), stage, rootVFS{FS: vfs.OSFS, root: root}, console)
	if err != nil || len(console.commands) == 0 {
		return ""
	}
	name, _, _ := strings.Cut(console.commands[0], " ")
	return name
}
