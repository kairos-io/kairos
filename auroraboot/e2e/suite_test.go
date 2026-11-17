package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var getVersionCmd = ". /etc/kairos-release; [ ! -z \"$KAIROS_VERSION\" ] && echo $KAIROS_VERSION"

var stateAssertVM = func(vm testVM, query, expected string) {
	By(fmt.Sprintf("Expecting state %s to be %s", query, expected))
	out, err := vm.SudoOut(fmt.Sprintf("kairos-agent state get %s", query))
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), out)
	ExpectWithOffset(1, out).To(ContainSubstring(expected))
}

var stateContains = func(vm testVM, query string, expected ...string) {
	var or []types.GomegaMatcher
	for _, e := range expected {
		or = append(or, ContainSubstring(e))
	}
	out, err := vm.SudoOut(fmt.Sprintf("kairos-agent state get %s", query))
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), out)
	ExpectWithOffset(1, strings.ToLower(out)).To(Or(or...))
}

type Auroraboot struct {
	ContainerImage string
	Dirs           []string          // directories to mount from host
	ManualDirs     map[string]string // directories to mount from host to an specific path in the container
	WorkDir        string            // working directory inside the container (docker run -w), empty keeps the image default
}

func TestAurorabootE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Auroraboot end to end test suite")
}

func NewAuroraboot(dirs ...string) *Auroraboot {
	newAurorabootImage()
	return &Auroraboot{ContainerImage: "auroraboot:test", Dirs: dirs}
}

// newAurorabootImage builds the auroraboot image for testing.
func newAurorabootImage() {
	// Get root path of the project
	testDir, err := os.Getwd()
	Expect(err).ToNot(HaveOccurred())
	parentDir := path.Join(testDir, "..")
	rootDir, err := filepath.Abs(parentDir)
	Expect(err).ToNot(HaveOccurred())
	// Build auroraboot image. This build runs on the docker driver, so the
	// network=host driver-opt the workflow gives buildx does not reach it and
	// every RUN step gets a veth on the default bridge: on the CI runners the
	// npm and luet steps then fail with "bridge port not forwarding after
	// 200ms". No RUN step publishes a port, they only fetch, so the host
	// network is enough.
	output, err := exec.Command("docker", "build", "--network", "host", "--target", "default", "-t", "auroraboot:test", "-f", filepath.Join(rootDir, "Dockerfile"), rootDir).CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(output))
}

// auroraboot relies on various external binaries. To make sure those dependencies
// are in place (or to test the behavior of auroraboot when they are not), we run auroraboot
// in a container using this function.
func (e *Auroraboot) Run(aurorabootArgs ...string) (string, error) {
	return e.ContainerRun("auroraboot", aurorabootArgs...)
}

// --privileged is for auroraboot itself, which reaches for loop devices and
// device nodes while it builds artifacts. The build_uki_test.go helpers no
// longer need it: they read the ISO with xorriso and mtools, not by mounting.
func (e *Auroraboot) ContainerRun(entrypoint string, args ...string) (string, error) {
	dockerArgs := []string{
		"run", "--rm", "--privileged",
		// The container publishes no ports and only needs outbound access to
		// pull images, so the default bridge buys nothing and costs a veth: on
		// the CI runners docker fails the run with "bridge port not forwarding
		// after 200ms" before auroraboot starts.
		"--network", "host",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"--entrypoint", entrypoint,
	}

	if e.WorkDir != "" {
		dockerArgs = append(dockerArgs, "-w", e.WorkDir)
	}

	for _, d := range e.Dirs {
		dockerArgs = append(dockerArgs, "-v", fmt.Sprintf("%[1]s:%[1]s", d))
	}

	for k, v := range e.ManualDirs {
		dockerArgs = append(dockerArgs, "-v", fmt.Sprintf("%s:%s", k, v))
	}

	dockerArgs = append(dockerArgs, e.ContainerImage)
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.Command("docker", dockerArgs...)
	out, err := cmd.CombinedOutput()

	return string(out), err
}

// withOutput folds a command's combined output into its error. Without it a
// helper that discards the output leaves Ginkgo printing only "exit status 1",
// which says nothing about why the command failed.
func withOutput(what, out string, err error) error {
	if err == nil {
		return nil
	}
	if out = strings.TrimSpace(out); out != "" {
		return fmt.Errorf("%s: %w\n%s", what, err, out)
	}
	return fmt.Errorf("%s: %w", what, err)
}

// pullTimeout caps a single `docker pull`. Healthy runs of this suite finish
// the whole raw-disk spec, pull included, in about five minutes, so this only
// fires on a stalled registry connection. Without it a pull that never returns
// holds the serial spec slot until ginkgo's 2h suite budget runs out and every
// spec after it is dropped (kairos-io/kairos#4655).
const pullTimeout = 10 * time.Minute

// runWithTimeout runs a command with its combined output captured and kills it
// once d elapses, so a stalled child costs one spec instead of the suite.
func runWithTimeout(d time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	// The killed child reports "signal: killed", never context.DeadlineExceeded,
	// so the deadline has to be read off the context itself.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		cmdline := strings.Join(append([]string{name}, args...), " ")
		return string(out), fmt.Errorf("%s timed out after %s", cmdline, d)
	}
	return string(out), err
}

// PullImage pulls image, invoking docker directly rather than through
// utils.SH: SH wraps the command in `sh -c`, so cancelling it would kill the
// shell and leave docker running. The error carries the pull output, so a
// failure reads as more than "exit status 1" (#810).
func PullImage(image string) (string, error) {
	out, err := runWithTimeout(pullTimeout, "docker", "pull", image)
	return out, withOutput(fmt.Sprintf("docker pull %s", image), out, err)
}

func WriteConfig(config, dir string) error {
	os.RemoveAll(filepath.Join(dir, "config.yaml"))
	f, err := os.Create(filepath.Join(dir, "config.yaml"))
	if err != nil {
		return err
	}

	_, err = f.WriteString(config)
	return err
}
