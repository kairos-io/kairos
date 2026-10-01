package bundled

// SshdHardeningPath is the drop-in file we install so hardened defaults
// take effect on any distro whose sshd loads /etc/ssh/sshd_config.d/*.conf
// (all Kairos targets: OpenSSH >= 8.2).
//
// The 05- prefix is deliberate: sshd_config uses first-value-wins and
// loads drop-ins in lexical order. Kairos-init owns the baseline
// hardening for every distribution Kairos supports; distributions or
// operators that need to layer a stricter override can drop a file with
// a lower-numbered prefix.
//
// Hadron is the one base image that does, so on Hadron this file does NOT
// own the directives it sets. Hadron ships three drop-ins that all sort
// before 05-:
//
//	01-hadron-stig.conf     the access-control, session and login block
//	02-hadron-crypto.conf   Ciphers, KexAlgorithms, MACs, HostKeyAlgorithms
//	                        (02-hadron-fips.conf instead, in FIPS images)
//	03-hadron.conf          UsePAM
//
// The numbering is deliberate on both sides, and the 02- file is the reason
// not to "fix" this by moving to 00-: in a FIPS image it pins the
// FIPS-validated algorithms, and anything sorting before it would replace
// them with the non-validated lists below. The values here are lined up with
// Hadron's, so the effective config is the same either way; what differs is
// which file a change has to land in. On Hadron only the directives Hadron
// omits (Protocol, SyslogFacility, UseDNS, TCPKeepAlive, ...) come from here.
//
// Who should own sshd hardening on Hadron is kairos-io/kairos#5041.
const SshdHardeningPath = "/etc/ssh/sshd_config.d/05-kairos-hardening.conf"

// SshdHardeningConfig is Kairos' baseline sshd hardening drop-in. It is
// the single source of truth for sshd hardening across every base image
// Kairos runs on (Ubuntu, Rocky, Alpine and so on). Hadron is the
// exception: it carries its own drop-ins, which sort first and win. See
// SshdHardeningPath.
//
// Password- and authentication-method controls (PasswordAuthentication,
// AuthenticationMethods, ChallengeResponseAuthentication) are intentionally
// omitted so operators can still log in with the default password on first
// boot and provision their own key. Once a key is present, drop those in
// via a lower-numbered file that loads before this one under
// first-value-wins semantics; the kairos-agent ssh_hardening flow does
// exactly that at install time via the 50- prefixed drop-in the agent
// writes. Use 04-, not 01-: on Hadron 01- through 03- are taken by the base
// image (see SshdHardeningPath), while 04- sorts after those and before
// this file on every flavor.
//
// The crypto directives lead with the two post-quantum hybrids OpenSSH
// offers by default (mlkem768x25519-sha256 since 9.9, sntrup761x25519-sha512
// since 8.5 under its @openssh.com name, since 10.0 under the plain one).
// Without one of them an OpenSSH 10.1+ client prints "connection is not
// using a post-quantum key exchange algorithm" on every login, because
// this file pre-empts the OpenSSH default. Both names for sntrup761 are
// listed so clients that only know the older @openssh.com spelling still
// land on a PQ hybrid.
const SshdHardeningConfig = `# Managed by kairos-init. Baseline sshd hardening for every Kairos base
# image. Draws on the DevSec ssh-baseline (https://github.com/dev-sec/ssh-baseline)
# for the general shape, tightened where CIS Distribution Independent Linux
# v2.0.0 L1 asks for more, and lined up with the Hadron STIG values so a
# Hadron image's own acceptance tests keep passing whichever of the two
# drop-ins ends up owning a directive.

Protocol 2

# --- Crypto (post-quantum first for OpenSSH 10.x) ---
Ciphers aes256-gcm@openssh.com,chacha20-poly1305@openssh.com,aes256-ctr,aes128-gcm@openssh.com,aes128-ctr
KexAlgorithms mlkem768x25519-sha256,sntrup761x25519-sha512,sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256
MACs hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256,hmac-sha2-512,umac-128@openssh.com
HostKeyAlgorithms ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,rsa-sha2-512,rsa-sha2-256

# --- Access control ---
PermitRootLogin prohibit-password
PermitEmptyPasswords no
PermitUserEnvironment no
IgnoreRhosts yes
IgnoreUserKnownHosts yes
HostbasedAuthentication no

# --- Session / forwarding ---
X11Forwarding no
AllowTcpForwarding no
AllowAgentForwarding no
GatewayPorts no
PermitTunnel no
Compression no
StrictModes yes
TCPKeepAlive no

# --- Login policy ---
MaxAuthTries 4
MaxSessions 10
MaxStartups 10:30:60
LoginGraceTime 60
ClientAliveInterval 600
ClientAliveCountMax 1

# --- Logging ---
SyslogFacility AUTH
LogLevel VERBOSE

# --- Rekey ---
RekeyLimit 1G 1h

# --- Misc ---
UseDNS no
UsePAM yes
PrintMotd no

# CIS Distribution Independent Linux v2.0.0 L1 section 5.2.19 (remote
# login warning banner). The banner file itself is written by the CIS
# hardening stage.
Banner /etc/issue.net
`
