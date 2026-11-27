package images

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const (
	// SSHPathBooted, an empty path, selects the image the remote node booted from.
	SSHPathBooted = ""
	// SSHPathActive and SSHPathPassive select the active and the passive image
	// in the state partition of the remote node.
	SSHPathActive  = "/active"
	SSHPathPassive = "/passive"

	// DefaultSSHPort is the port used when the URI names none.
	DefaultSSHPort = "22"
)

// SSHTarget is a parsed ssh://[user@]host[:port][/path] image source.
//
// Path is one of the SSHPath constants, or the absolute path of an image file
// on the remote node. Nothing is enforced on the remote node: the account that
// connects reads whatever it is allowed to read.
type SSHTarget struct {
	// User is empty when the URI names none, the configured user or the current one applies.
	User string
	Host string
	// Port is empty when the URI did not name one, DefaultSSHPort applies.
	Port string
	Path string
}

// Addr returns host:port ready to dial, with the default port filled in.
func (t SSHTarget) Addr() string {
	port := t.Port
	if port == "" {
		port = DefaultSSHPort
	}
	return net.JoinHostPort(t.Host, port)
}

// value is the form kept inside the ImageSource: user@host[:port][/path].
func (t SSHTarget) value() string {
	host := t.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	v := host
	if t.User != "" {
		v = t.User + "@" + host
	}
	if t.Port != "" {
		v += ":" + t.Port
	}
	v += t.Path
	return v
}

// String returns the ssh:// URI of the target.
func (t SSHTarget) String() string {
	return ssh + "://" + t.value()
}

// SSHTarget returns the parsed target of an ssh source.
func (i ImageSource) SSHTarget() (SSHTarget, error) {
	if !i.IsSSH() {
		return SSHTarget{}, fmt.Errorf("%s is not an ssh source", i.String())
	}
	u, err := url.Parse(ssh + "://" + i.source)
	if err != nil {
		return SSHTarget{}, err
	}
	return sshTargetFromURL(u)
}

func (i *ImageSource) parseSSHURI(uri string, u *url.URL) error {
	// Never echo the URI back, it may carry a password the user should not have put there.
	if !strings.HasPrefix(uri, ssh+"://") {
		return fmt.Errorf("invalid ssh source: use ssh://user@host[:port][/active|/passive]")
	}
	t, err := sshTargetFromURL(u)
	if err != nil {
		return err
	}
	i.srcType = ssh
	i.source = t.value()
	return nil
}

func sshTargetFromURL(u *url.URL) (SSHTarget, error) {
	var t SSHTarget
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			return t, fmt.Errorf("invalid ssh source: a password in the URI is not supported, authenticate with a key")
		}
		if u.User.Username() == "" || strings.HasPrefix(u.User.Username(), "-") {
			return t, fmt.Errorf("invalid ssh source: the user is not valid")
		}
		t.User = u.User.Username()
	}
	if strings.HasPrefix(u.Hostname(), "-") {
		return t, fmt.Errorf("invalid ssh source: the host is not valid")
	}
	if u.Hostname() == "" {
		return t, fmt.Errorf("invalid ssh source: the host is required, use ssh://user@host")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return t, fmt.Errorf("invalid ssh source: query and fragment are not supported")
	}
	t.Host = u.Hostname()
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return t, fmt.Errorf("invalid ssh source: port %q is not valid", p)
		}
		t.Port = strconv.Itoa(n)
	}
	p := u.Path
	if p == "/" {
		p = SSHPathBooted
	}
	if p != SSHPathBooted && !validRemotePath(p) {
		return t, fmt.Errorf("invalid ssh source: the path must be /active, /passive or the absolute, clean path of an image file")
	}
	t.Path = p
	return t, nil
}

func validRemotePath(p string) bool {
	if !path.IsAbs(p) || path.Clean(p) != p || p == "/" {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// SSHSourceOptions is how the node authenticates the server and itself when
// it fetches an ssh source. It never holds key material, only where to find it.
type SSHSourceOptions struct {
	// User is the account to connect with when the URI names none.
	User string
	// IdentityFile is the path to the private key. Without one ssh uses its own
	// configuration and agent.
	IdentityFile string
	// KnownHostsFile is a known_hosts file holding the key of the serving node. Without one
	// and without fingerprints the known_hosts files of the user running the agent apply.
	KnownHostsFile string
	// HostKeyFingerprints are accepted host key fingerprints, SHA256:<base64>.
	HostKeyFingerprints []string
}
