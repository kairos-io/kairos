//go:build e2e

package e2e_test

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// freePort asks the kernel for an unused TCP port and returns it.
func freePort() (int, error) {
	a, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", a)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// dockerRun runs a docker command and returns combined output, wrapping errors
// with the output so failures are debuggable.
func dockerRun(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

// startRegistry pulls baseImage (if absent), starts a plain-HTTP registry:2
// container named containerName published on hostPort, then tags and pushes
// baseImage into it under the path "kairos/source:test". It returns the
// in-registry repository path (without host) so callers can build source URIs.
func startRegistry(containerName, baseImage string, hostPort int) (string, error) {
	if _, err := dockerRun("pull", baseImage); err != nil {
		return "", err
	}
	if _, err := dockerRun("run", "-d", "--name", containerName,
		"-p", fmt.Sprintf("%d:5000", hostPort), "registry:2"); err != nil {
		return "", err
	}
	repo := "kairos/source:test"
	localRef := fmt.Sprintf("localhost:%d/%s", hostPort, repo)
	if _, err := dockerRun("tag", baseImage, localRef); err != nil {
		return "", err
	}
	if _, err := dockerRun("push", localRef); err != nil {
		return "", err
	}
	return repo, nil
}

// stopRegistry force-removes the registry container. Errors are returned for
// logging but are non-fatal at teardown.
func stopRegistry(containerName string) error {
	_, err := dockerRun("rm", "-f", containerName)
	return err
}
