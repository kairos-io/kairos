package testartifacts

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// AuroraBootVersion is the AuroraBoot release the helpers use, both as a
// release binary and as a container image. Renovate updates it together with
// the workflow pins.
const AuroraBootVersion = "v0.28.0"

// AuroraBootImage is the AuroraBoot container image the Docker helpers run.
const AuroraBootImage = "quay.io/kairos/auroraboot:" + AuroraBootVersion

// DockerAvailable reports whether a Docker daemon answers.
func DockerAvailable() bool {
	return exec.Command("docker", "info").Run() == nil
}

// docker runs the docker CLI and returns its combined output in the error
// when it fails, so a test failure shows what the container printed.
func docker(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, out.String())
	}
	return nil
}

// userArgs makes a container run as the calling user, so the files it
// writes into a bind mount belong to the caller.
func userArgs() []string {
	return []string{"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())}
}

// dockerSocket returns the path of the Docker daemon socket, honouring a
// unix:// DOCKER_HOST.
func dockerSocket() string {
	if host, ok := strings.CutPrefix(os.Getenv("DOCKER_HOST"), "unix://"); ok {
		return host
	}
	return "/var/run/docker.sock"
}

// socketArgs mounts the Docker socket into a container running as the
// calling user and adds the socket's group, which a non-root user needs
// to talk to the daemon.
func socketArgs() ([]string, error) {
	sock := dockerSocket()
	info, err := os.Stat(sock)
	if err != nil {
		return nil, fmt.Errorf("docker socket: %w", err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("docker socket %s: no ownership information", sock)
	}
	return []string{"-v", sock + ":/var/run/docker.sock", "--group-add", strconv.FormatUint(uint64(st.Gid), 10)}, nil
}

// GenerateKeySet writes a throwaway Secure Boot key set (PK, KEK and db as
// .key, .pem, .der, .esl and .auth) and the TPM PCR policy signing key
// tpm2-pcr-private.pem into dir, using AuroraBoot genkey from the release
// binary. It needs openssl and libpcsclite on the host, not Docker. The
// certificates expire after 30 days.
func GenerateKeySet(ctx context.Context, dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	bin, err := AuroraBootBinary(ctx)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "genkey", "-e", "30", "-o", dir, "kairos-test")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("auroraboot genkey: %w\n%s", err, out)
	}
	return nil
}
