package bundled

// SshdHardeningPath is the drop-in file we install so hardened defaults
// take effect on any distro whose sshd loads /etc/ssh/sshd_config.d/*.conf
// (all Kairos targets: OpenSSH >= 8.2).
//
// The 05- prefix is deliberate: sshd_config uses first-value-wins and
// loads drop-ins in lexical order. Some base images (notably Hadron)
// already ship 99-*/100-* drop-ins with their own crypto/timeout values;
// a lower-numbered filename means our hardening takes precedence over
// the vendor defaults on the directives we set. Operators who want to
// relax a specific control layer their own drop-in at 01-*.
const SshdHardeningPath = "/etc/ssh/sshd_config.d/05-kairos-hardening.conf"

// SshdHardeningConfig is Kairos' baseline sshd hardening drop-in, derived
// from the DevSec SSH baseline (https://github.com/dev-sec/ssh-baseline).
//
// Password- and authentication-method controls (PasswordAuthentication,
// AuthenticationMethods, ChallengeResponseAuthentication) are intentionally
// omitted so operators can still log in with the default password on first
// boot and provision their own key. Once a key is present, drop those in
// via a lower-numbered file (for example 01-my-auth.conf) that loads
// before this one under first-value-wins semantics.
const SshdHardeningConfig = `# Managed by kairos-init. Baseline follows the DevSec ssh-baseline:
# https://github.com/dev-sec/ssh-baseline

Protocol 2

Ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com,aes256-ctr,aes192-ctr,aes128-ctr
KexAlgorithms sntrup761x25519-sha512@openssh.com,curve25519-sha256@libssh.org,diffie-hellman-group-exchange-sha256
MACs hmac-sha2-512-etm@openssh.com,hmac-sha2-256-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-512,hmac-sha2-256
HostKeyAlgorithms ssh-ed25519,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-256,rsa-sha2-512,rsa-sha2-256-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com

PermitRootLogin no
IgnoreRhosts yes
HostbasedAuthentication no
PermitEmptyPasswords no

MaxAuthTries 2
MaxSessions 10
MaxStartups 10:30:60
LoginGraceTime 30
ClientAliveInterval 300
ClientAliveCountMax 3

AllowAgentForwarding no
AllowTcpForwarding no
X11Forwarding no
PermitTunnel no
GatewayPorts no
TCPKeepAlive no

SyslogFacility AUTH
LogLevel VERBOSE
UseDNS no
UsePAM yes
StrictModes yes
`
