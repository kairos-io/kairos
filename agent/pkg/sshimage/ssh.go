package sshimage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

// remote runs commands on the other node with the ssh client of the system, so
// the ssh configuration, agent and known_hosts of the user running the agent
// apply: ProxyJump, Host entries, certificates and so on.
//
// Whatever that configuration says, the options set here win, they come first
// on the command line: no password or keyboard prompt (BatchMode), the host key
// is always checked (StrictHostKeyChecking=yes), and nothing is forwarded.
type remote struct {
	opts   Options
	target images.SSHTarget
	// pinned is a known_hosts file holding the keys that matched the configured
	// fingerprints, when there are any.
	pinned string
	bin    string
}

// newRemote checks the options and prepares the ssh invocation. cleanup removes
// what it created.
func newRemote(ctx context.Context, o Options, t images.SSHTarget) (*remote, func(), error) {
	bin, err := exec.LookPath("ssh")
	if err != nil {
		return nil, nil, fmt.Errorf("an ssh source needs the ssh client, which is not in the PATH of the agent")
	}
	if o.IdentityFile != "" {
		if err := checkIdentity(o.IdentityFile); err != nil {
			return nil, nil, err
		}
	}
	for _, f := range append([]string{o.IdentityFile, o.KnownHostsFile}, o.HostKeyFingerprints...) {
		if strings.ContainsAny(f, " \t\r\n\"'\\") {
			return nil, nil, fmt.Errorf("%q cannot be used in an ssh option, no spaces or quotes", f)
		}
	}
	for _, fp := range o.HostKeyFingerprints {
		if !strings.HasPrefix(fp, "SHA256:") {
			return nil, nil, fmt.Errorf("host key fingerprint %q must look like SHA256:<base64>, as printed by ssh-keygen -lf", fp)
		}
	}
	if t.User == "" {
		t.User = o.User
	}
	r := &remote{opts: o, target: t, bin: bin}
	cleanup := func() {}
	if len(o.HostKeyFingerprints) > 0 {
		dir, err := os.MkdirTemp("", "kairos-ssh-source-")
		if err != nil {
			return nil, nil, err
		}
		cleanup = func() { os.RemoveAll(dir) }
		if r.pinned, err = pinHostKeys(ctx, o, t, dir); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	return r, cleanup, nil
}

// checkIdentity refuses a key file other users can read, as ssh itself does.
func checkIdentity(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("identity file: %w", err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("identity file %s is readable by other users (mode %04o), chmod 600 it", path, st.Mode().Perm())
	}
	return nil
}

// pinHostKeys connects to the node only to read its host keys, keeps those whose
// fingerprint is configured and writes them to a known_hosts file. ssh then
// connects against that file, so the key it talks to is the key that was pinned.
func pinHostKeys(ctx context.Context, o Options, t images.SSHTarget, dir string) (string, error) {
	scan, err := exec.LookPath("ssh-keyscan")
	if err != nil {
		return "", fmt.Errorf("host-key-fingerprints needs ssh-keyscan, which is not in the PATH of the agent")
	}
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		return "", fmt.Errorf("host-key-fingerprints needs ssh-keygen, which is not in the PATH of the agent")
	}
	port := t.Port
	if port == "" {
		port = images.DefaultSSHPort
	}
	out, err := exec.CommandContext(ctx, scan, "-T", strconv.Itoa(int(o.connectTimeout().Seconds())), "-p", port, t.Host).Output()
	if err != nil {
		return "", fmt.Errorf("reading the host keys of %s: %w", t.Host, err)
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("%s offered no host keys", t.Host)
	}
	all := filepath.Join(dir, "scanned")
	if err := os.WriteFile(all, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return "", err
	}
	fps, err := exec.CommandContext(ctx, keygen, "-l", "-E", "sha256", "-f", all).Output()
	if err != nil {
		return "", fmt.Errorf("fingerprinting the host keys of %s: %w", t.Host, err)
	}
	pins := map[string]bool{}
	for _, fp := range o.HostKeyFingerprints {
		pins[fp] = true
	}
	var keep, seen []string
	for i, l := range strings.Split(strings.TrimSpace(string(fps)), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || i >= len(lines) {
			continue
		}
		seen = append(seen, f[1])
		if pins[f[1]] {
			keep = append(keep, lines[i])
		}
	}
	if len(keep) == 0 {
		return "", fmt.Errorf("the host key of %s (%s) does not match a configured fingerprint", t.Host, strings.Join(seen, ", "))
	}
	p := filepath.Join(dir, "known_hosts")
	return p, os.WriteFile(p, []byte(strings.Join(keep, "\n")+"\n"), 0o600)
}

func (o Options) connectTimeout() time.Duration { return o.DialTimeout }

// args builds the ssh command line for a remote command.
func (r *remote) args(cmd string) []string {
	a := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "ForwardAgent=no",
		"-o", "ForwardX11=no",
		"-o", "PermitLocalCommand=no",
		"-o", "UpdateHostKeys=no",
		"-o", "ConnectTimeout=" + strconv.Itoa(int(r.opts.DialTimeout.Seconds())),
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
	}
	if r.opts.IdentityFile != "" {
		a = append(a, "-i", r.opts.IdentityFile, "-o", "IdentitiesOnly=yes")
	}
	var known []string
	if r.pinned != "" {
		known = append(known, r.pinned)
	}
	if r.opts.KnownHostsFile != "" {
		known = append(known, r.opts.KnownHostsFile)
	}
	if len(known) > 0 {
		// Only what was configured, not whatever else the system trusts.
		a = append(a, "-o", "UserKnownHostsFile="+strings.Join(known, " "), "-o", "GlobalKnownHostsFile=/dev/null")
	}
	if r.target.Port != "" {
		a = append(a, "-p", r.target.Port)
	}
	if r.target.User != "" {
		a = append(a, "-l", r.target.User)
	}
	return append(a, "--", r.target.Host, cmd)
}

// run executes a command and returns its standard output.
func (r *remote) run(ctx context.Context, cmd string) (string, error) {
	c := exec.CommandContext(ctx, r.bin, r.args(cmd)...)
	var out bytes.Buffer
	stderr := &limitedBuffer{}
	c.Stdout, c.Stderr = &out, stderr
	if err := c.Run(); err != nil {
		return "", failure(firstWord(cmd), stderr, err)
	}
	return out.String(), nil
}

// stream starts a command and returns its standard output. wait reports how
// the command ended, call it once the output has been read. stop ends it.
func (r *remote) stream(ctx context.Context, cmd string) (io.Reader, func() error, func(), error) {
	c := exec.CommandContext(ctx, r.bin, r.args(cmd)...)
	stderr := &limitedBuffer{}
	c.Stderr = stderr
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := c.Start(); err != nil {
		return nil, nil, nil, err
	}
	stop := func() { _ = c.Process.Kill(); _ = c.Wait() }
	return stdout, func() error {
		if err := c.Wait(); err != nil {
			return failure(firstWord(cmd), stderr, err)
		}
		return nil
	}, stop, nil
}

func failure(what string, stderr *limitedBuffer, err error) error {
	if msg := stderr.String(); msg != "" {
		return fmt.Errorf("%s: %s", what, msg)
	}
	return fmt.Errorf("%s: %w", what, err)
}

func firstWord(cmd string) string {
	if f := strings.Fields(cmd); len(f) > 0 {
		return f[0]
	}
	return cmd
}

// limitedBuffer keeps the first bytes written to it, for error messages.
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if room := 2048 - l.buf.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		l.buf.Write(p[:room])
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(bytes.TrimSpace(l.buf.Bytes()))
}

var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
