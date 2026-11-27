package check

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/plugins"
	"github.com/mudler/yip/pkg/schema"
	"github.com/twpayne/go-vfs/v5"
)

// closedFS is a file system that refuses everything. rootVFS builds on it, so
// that a method of yip's that is not redirected into the root fails and never
// reads the host.
type closedFS struct{}

var errClosed = errors.New("not available when checking a root filesystem")

func (closedFS) Chmod(string, fs.FileMode) error                     { return errClosed }
func (closedFS) Chown(string, int, int) error                        { return errClosed }
func (closedFS) Chtimes(string, time.Time, time.Time) error          { return errClosed }
func (closedFS) Create(string) (*os.File, error)                     { return nil, errClosed }
func (closedFS) Glob(string) ([]string, error)                       { return nil, errClosed }
func (closedFS) Lchown(string, int, int) error                       { return errClosed }
func (closedFS) Link(string, string) error                           { return errClosed }
func (closedFS) Lstat(string) (fs.FileInfo, error)                   { return nil, errClosed }
func (closedFS) Mkdir(string, fs.FileMode) error                     { return errClosed }
func (closedFS) Open(string) (fs.File, error)                        { return nil, errClosed }
func (closedFS) OpenFile(string, int, fs.FileMode) (*os.File, error) { return nil, errClosed }
func (closedFS) PathSeparator() rune                                 { return '/' }
func (closedFS) RawPath(string) (string, error)                      { return "", errClosed }
func (closedFS) ReadDir(string) ([]fs.DirEntry, error)               { return nil, errClosed }
func (closedFS) ReadFile(string) ([]byte, error)                     { return nil, errClosed }
func (closedFS) Readlink(string) (string, error)                     { return "", errClosed }
func (closedFS) Remove(string) error                                 { return errClosed }
func (closedFS) RemoveAll(string) error                              { return errClosed }
func (closedFS) Rename(string, string) error                         { return errClosed }
func (closedFS) Stat(string) (fs.FileInfo, error)                    { return nil, errClosed }
func (closedFS) Symlink(string, string) error                        { return errClosed }
func (closedFS) Truncate(string, int64) error                        { return errClosed }
func (closedFS) WriteFile(string, []byte, fs.FileMode) error         { return errClosed }

// rootVFS gives yip the os-release of the root. yip opens /etc/os-release to
// choose the package manager, and an absolute link in an unpacked image has to
// point into the image. Reads go into the root, everything else is refused.
type rootVFS struct {
	closedFS
	root rootFS
}

var _ vfs.FS = rootVFS{}

func (v rootVFS) Open(name string) (fs.File, error) {
	f, err := v.root.open(name)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (v rootVFS) ReadFile(name string) ([]byte, error) {
	return v.root.readFile(name)
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
func packageManager(root rootFS) (string, error) {
	console := &recordingConsole{}
	stage := schema.Stage{Packages: schema.Packages{Install: []string{"kairos-check"}}}
	err := plugins.Packages(logger.NewNullLogger(), stage, rootVFS{root: root}, console)
	if err != nil {
		return "", err
	}
	if len(console.commands) == 0 {
		return "", errors.New("no command")
	}
	name, _, _ := strings.Cut(console.commands[0], " ")
	return name, nil
}
