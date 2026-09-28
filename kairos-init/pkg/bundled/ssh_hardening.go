package bundled

// SshdHardeningPath is the drop-in file we install so hardened defaults
// take effect on any distro whose sshd loads /etc/ssh/sshd_config.d/*.conf
// (all Kairos targets: OpenSSH >= 8.2).
//
// sshd_config is first-value-wins and loads drop-ins in lexical order, so the
// numeric prefix decides who owns a directive when two files set it.
//
// The 05- prefix wins on base images that ship no sshd drop-ins of their own,
// which is every Kairos flavor except Hadron. It does NOT win on Hadron:
//
//	hadron <= v0.5.1  99-hadron-stig, 99-hadron, 100-hadron-crypto   we win
//	hadron >= v0.5.3  01-hadron-stig, 02-hadron-crypto, 03-hadron    hadron wins
//
// Hadron renumbered deliberately, and the 02- crypto file carries a FIPS
// rationale: on hadron-fips it is 02-hadron-fips.conf, pinning the
// FIPS-validated algorithms, and anything that sorts before it would replace
// them with the non-validated lists below. So do not "fix" this by moving to
// 00-. Every directive Hadron's 01-/02-/03- files also set is inert here on
// Hadron today; only the ones they omit (Protocol, SyslogFacility, UseDNS,
// TCPKeepAlive, ...) still apply.
//
// Who should own sshd hardening on Hadron is kairos-io/kairos#5041.
const SshdHardeningPath = "/etc/ssh/sshd_config.d/05-kairos-hardening.conf"

// SshdHardeningConfig is Kairos' baseline sshd hardening drop-in, derived
// from the DevSec SSH baseline (https://github.com/dev-sec/ssh-baseline).
//
// Password- and authentication-method controls (PasswordAuthentication,
// AuthenticationMethods, ChallengeResponseAuthentication) are intentionally
// omitted so operators can still log in with the default password on first
// boot and provision their own key. Once a key is present, drop those in
// via a lower-numbered file that loads before this one under
// first-value-wins semantics. Use 04-, not 01-: on Hadron 01- through 03-
// are taken by the base image (see SshdHardeningPath), while 04- sorts
// after those and before this file on every flavor.
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
