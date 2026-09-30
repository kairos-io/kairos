package bundled

// CISModprobeBlocklistPath is the modprobe.d drop-in that makes the
// filesystem modules CIS Distribution Independent Linux v2.0.0 L1 sections
// 1.1.1.1-1.1.1.6 call out unavailable.
//
// Kept separate from blacklist_bpfilter.conf (installed by the 29_blacklist
// cloud config for the Alpine/openrc bpfilter bug) so the two can be audited
// and reverted independently.
const CISModprobeBlocklistPath = "/etc/modprobe.d/cis-blocklist.conf"

// CISModprobeBlocklist redirects the module loader to /bin/false for the
// filesystems CIS L1 requires to be unmountable. `install <mod> /bin/false`
// is the form the benchmark's own remediation uses, and matches the existing
// bpfilter drop-in, so an explicit `modprobe <mod>` fails rather than silently
// succeeding.
//
// The list is applied unconditionally. A base image whose kernel never built
// one of these as a module still gets the policy line: the control is about
// the module being unloadable, and a later kernel bump that starts shipping
// the module must not silently reopen the hole.
const CISModprobeBlocklist = `# Managed by kairos-init. Filesystem modules required to be unavailable by
# CIS Distribution Independent Linux v2.0.0 L1, sections 1.1.1.1-1.1.1.6.

install cramfs /bin/false
install freevxfs /bin/false
install jffs2 /bin/false
install hfs /bin/false
install hfsplus /bin/false
install udf /bin/false
`

// IssueNetPath is the banner shown before authentication on network logins,
// as opposed to /etc/issue (and the /etc/issue.d/01-KAIROS drop-in holding the
// Kairos art) which agetty renders on local consoles.
const IssueNetPath = "/etc/issue.net"

// IssueNetBanner is the pre-authentication warning banner for remote logins,
// covering CIS Distribution Independent Linux v2.0.0 L1 section 1.7 (remote
// login warning banner).
//
// Content is deliberately generic. The benchmark's audit fails the banner if
// it discloses the OS - it greps /etc/issue.net for the \m, \r, \s and \v
// agetty escapes and for the /etc/os-release ID - so nothing here names the
// distribution, the release or the kernel, not even in a leading comment
// (/etc/issue.net has no comment syntax; every line is printed verbatim).
//
// Wiring sshd to actually print this (the `Banner` directive) belongs to the
// benchmark's SSH section and is not done here.
const IssueNetBanner = `#############################################################################
#                                                                           #
#                               NOTICE                                      #
#                                                                           #
#  This system is for the use of authorized users only. Individuals using   #
#  this system without authority, or in excess of their authority, are      #
#  subject to having all of their activities on this system monitored and   #
#  recorded by system personnel.                                            #
#                                                                           #
#  Anyone using this system expressly consents to such monitoring and is    #
#  advised that if such monitoring reveals possible evidence of criminal    #
#  activity, system personnel may provide the evidence to law enforcement   #
#  officials.                                                               #
#                                                                           #
#############################################################################
`

const CISSysctlPath = "/etc/sysctl.d/99-kairos-cis.conf"

const CISSysctl = `kernel.randomize_va_space = 2

net.ipv4.conf.all.rp_filter = 2
net.ipv4.conf.default.rp_filter = 2
net.ipv4.tcp_syncookies = 1
net.ipv4.conf.all.accept_source_route = 0
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0

net.ipv6.conf.all.accept_ra = 0
net.ipv6.conf.all.accept_redirects = 0
`

const CISAuditRulesPath = "/etc/audit/rules.d/50-kairos.rules"

// The file opens with the base configuration upstream audit-userspace ships as
// rules/10-base-config.rules, because on the openrc path nothing else supplies
// it. Alpine's auditd init script runs `auditctl -R` on this one file (see
// CISAuditdConfDAlpine) and Alpine's audit package installs an empty
// /etc/audit/rules.d, so without these four lines the kernel keeps its default
// backlog_limit of 64 while the rules below audit open, chmod, chown and
// unlink for every non-system user, and failure mode 0 does not even log the
// overrun. `-e 2` at the end then makes that permanent: `auditctl -b` on a
// running node fails with EPERM.
//
// Repeating them here is also correct on the systemd path, which merges every
// /etc/audit/rules.d/*.rules with augenrules: that hoists -D, -b and -f to the
// top and moves -e to the last line, keeping the last value it processed,
// which is the same 8192 the base images already set.
//
// Syscall rules are paired b64+b32. On amd64 with CONFIG_IA32_EMULATION and on
// aarch64 with CONFIG_COMPAT (userspace's arch=b32 -> AUDIT_ARCH_ARM) a 32-bit
// binary would otherwise bypass every b64-only rule. `auditctl -R` warns and
// keeps loading past b32 lines the running kernel rejects, so shipping both
// pairs is safe on kernels without 32-bit compat.
const CISAuditRules = `-D
-b 8192
--backlog_wait_time 60000
-f 1

-a always,exit -F arch=b64 -S adjtimex,settimeofday,clock_settime -k time-change
-a always,exit -F arch=b32 -S adjtimex,settimeofday,clock_settime -k time-change
-w /etc/localtime -p wa -k time-change

-w /etc/group -p wa -k identity
-w /etc/passwd -p wa -k identity
-w /etc/gshadow -p wa -k identity
-w /etc/shadow -p wa -k identity
-w /etc/security/opasswd -p wa -k identity

-a always,exit -F arch=b64 -S sethostname,setdomainname -k system-locale
-a always,exit -F arch=b32 -S sethostname,setdomainname -k system-locale
-w /etc/issue -p wa -k system-locale
-w /etc/issue.net -p wa -k system-locale
-w /etc/hosts -p wa -k system-locale
-w /etc/networks -p wa -k system-locale

-w /etc/selinux/ -p wa -k MAC-policy
-w /usr/share/selinux/ -p wa -k MAC-policy

-w /var/log/faillog -p wa -k logins
-w /var/log/lastlog -p wa -k logins
-w /var/log/tallylog -p wa -k logins

-w /var/run/utmp -p wa -k session
-w /var/log/wtmp -p wa -k session
-w /var/log/btmp -p wa -k session

-a always,exit -F arch=b64 -S chmod,fchmod,fchmodat -F auid>=1000 -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b32 -S chmod,fchmod,fchmodat -F auid>=1000 -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b64 -S chown,fchown,lchown,fchownat -F auid>=1000 -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b32 -S chown,fchown,lchown,fchownat -F auid>=1000 -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b64 -S setxattr,lsetxattr,fsetxattr,removexattr,lremovexattr,fremovexattr -F auid>=1000 -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b32 -S setxattr,lsetxattr,fsetxattr,removexattr,lremovexattr,fremovexattr -F auid>=1000 -F auid!=unset -F key=perm_mod

-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F exit=-EACCES -F auid>=1000 -F auid!=unset -F key=access
-a always,exit -F arch=b32 -S creat,open,openat,truncate,ftruncate -F exit=-EACCES -F auid>=1000 -F auid!=unset -F key=access
-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F exit=-EPERM -F auid>=1000 -F auid!=unset -F key=access
-a always,exit -F arch=b32 -S creat,open,openat,truncate,ftruncate -F exit=-EPERM -F auid>=1000 -F auid!=unset -F key=access

-a always,exit -F arch=b64 -S mount -F auid>=1000 -F auid!=unset -F key=mounts
-a always,exit -F arch=b32 -S mount -F auid>=1000 -F auid!=unset -F key=mounts

-a always,exit -F arch=b64 -S unlink,unlinkat,rename,renameat -F auid>=1000 -F auid!=unset -F key=delete
-a always,exit -F arch=b32 -S unlink,unlinkat,rename,renameat -F auid>=1000 -F auid!=unset -F key=delete

-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d/ -p wa -k scope

-a always,exit -F arch=b64 -S init_module,delete_module,finit_module -F auid>=1000 -F auid!=unset -F key=modules
-a always,exit -F arch=b32 -S init_module,delete_module,finit_module -F auid>=1000 -F auid!=unset -F key=modules

-e 2
`

// CISAuditdConfDPath is the openrc /etc/conf.d/auditd drop-in Alpine's audit
// package ships. Alpine's openrc auditd script loads a single rules file at
// start via `auditctl -R $RULEFILE_STARTUP`; the default points at
// /etc/audit/audit.rules, which the audit package does not ship and which no
// one compiles from /etc/audit/rules.d/, so the CIS baseline drop-in never
// reaches the kernel. Override the default to load the drop-in directly.
const CISAuditdConfDPath = "/etc/conf.d/auditd"

// CISAuditdConfDAlpine keeps upstream's defaults but points RULEFILE_STARTUP
// at the CIS baseline drop-in so `rc-service auditd start` loads it. Ubuntu,
// SUSE and RHEL keep using the systemd auditd unit, which runs augenrules and
// picks up /etc/audit/rules.d/ on its own.
const CISAuditdConfDAlpine = `# Managed by kairos-init. See kairos-io/kairos#4907.
EXTRAOPTIONS=''

RULEFILE_STARTUP=/etc/audit/rules.d/50-kairos.rules

RULEFILE_STOP_PRE=/etc/audit/audit.rules.stop.pre
RULEFILE_STOP_POST=/etc/audit/audit.rules.stop.post

AUDITD_LANG=C
`

// CISPwqualityPath is the libpwquality config file. It is consulted by
// pam_pwquality (and by passwd on distros that link libpwquality directly),
// so it only ever affects password changes, never login.
const CISPwqualityPath = "/etc/security/pwquality.conf"

// CISPwquality covers CIS Distribution Independent Linux v2.0.0 L1 section
// 5.4.1 (password creation requirements). Values match the benchmark:
// 14-char minimum, at least one of each class, four-character difference
// from the old password.
const CISPwquality = `# Managed by kairos-init.
#
# CIS Distribution Independent Linux v2.0.0 L1, section 5.4.1
# (password creation requirements). Read by pam_pwquality.so on any PAM
# stack that includes the module (password type) and by passwd on distros
# that link libpwquality directly. Only affects password *changes*; a
# stricter policy here cannot lock out an existing operator, it just
# refuses a weak new password.
#
# minlen  = minimum accepted length in characters
# dcredit = digit credit (negative = at least |N| digits required)
# ucredit = uppercase letter credit (negative = at least |N| required)
# ocredit = other/symbol credit (negative = at least |N| required)
# lcredit = lowercase letter credit (negative = at least |N| required)
# difok   = minimum number of characters that must differ from the old
#           password
#
# kairos-init wires pam_pwquality into the password stack with the
# distro's own tool: pam-auth-update on Debian/Ubuntu (libpam-pwquality
# profile), authselect on the RHEL family (the local/minimal profiles
# load it), pam-config on SUSE releases whose pam-config knows the
# module. Hadron's system-auth loads it directly. Alpine images do not
# authenticate through PAM, so this file is inert there.
minlen = 14
dcredit = -1
ucredit = -1
ocredit = -1
lcredit = -1
difok = 4
`

// CISFaillockPath is the pam_faillock config file, read at PAM stack time.
// Hadron's /etc/pam.d/system-auth wires pam_faillock (preauth, authfail,
// authsucc) itself. On every other base the CIS hardening stage wires the
// module with the distro's own tool: pam-auth-update profiles on
// Debian/Ubuntu, `authselect ... with-faillock` on the RHEL family (stock
// RHEL-family images do not load pam_faillock). SUSE's pam-config has no
// faillock module and Alpine images do not authenticate through PAM, so
// the file stays inert on those two.
const CISFaillockPath = "/etc/security/faillock.conf"

// CISFaillock covers CIS Distribution Independent Linux v2.0.0 L1 section
// 5.4.2 (lockout on failed authentication). Root is included in the count
// because a network-facing root account under brute force is the case the
// control exists for; consoles that need recovery still have single-user
// mode.
const CISFaillock = `# Managed by kairos-init.
#
# CIS Distribution Independent Linux v2.0.0 L1, section 5.4.2 (lockout on
# failed authentication). Read by pam_faillock.so.
#
# kairos-init wires pam_faillock into the auth stack on Hadron,
# Debian/Ubuntu (pam-auth-update) and the RHEL family (authselect
# with-faillock). On SUSE (pam-config has no faillock module) and Alpine
# (no PAM in the login path) this file is inert.
#
# deny           = failed attempts before the account is locked
# unlock_time    = seconds the lock lasts (0 would mean forever)
# fail_interval  = seconds during which failed attempts are counted
# even_deny_root = apply the lockout to the root account too; the
#                  control exists for network-facing brute force and
#                  console recovery is still possible via single-user
#                  mode
deny = 5
unlock_time = 900
fail_interval = 900
even_deny_root
`

// CISPamConfigFaillockPath and CISPamConfigFaillockNotifyPath are the
// pam-auth-update profiles that wire pam_faillock into the Debian/Ubuntu
// common-auth and common-account stacks (CIS L1 5.4.2). Neither distro
// ships a faillock profile, only the module (libpam-modules, PAM >= 1.4),
// so kairos-init ships the pair the CIS Ubuntu benchmark describes and
// lets pam-auth-update compute the jump offsets. The profiles are only
// written when pam_faillock.so is present: pam-auth-update enables
// Default: yes profiles, and a stack line naming a missing module fails
// every authentication.
const CISPamConfigFaillockPath = "/usr/share/pam-configs/kairos-faillock"

// CISPamConfigFaillock records a failure after pam_unix rejects the
// password. Priority 0 places it after pam_unix (256) in common-auth.
const CISPamConfigFaillock = `Name: Kairos pam_faillock lockout on failure (CIS L1 5.4.2)
Default: yes
Priority: 0
Auth-Type: Primary
Auth:
	[default=die]	pam_faillock.so authfail
`

// CISPamConfigFaillockNotifyPath is the second half of the faillock pair.
const CISPamConfigFaillockNotifyPath = "/usr/share/pam-configs/kairos-faillock-notify"

// CISPamConfigFaillockNotify refuses a locked account before pam_unix runs
// (preauth, priority 1024 sorts it first) and clears the failure count in
// the account phase once a login succeeds.
const CISPamConfigFaillockNotify = `Name: Kairos pam_faillock preauth and reset on success (CIS L1 5.4.2)
Default: yes
Priority: 1024
Auth-Type: Primary
Auth:
	requisite	pam_faillock.so preauth
Account-Type: Primary
Account:
	required	pam_faillock.so
`

// CISAuthselectFaillock wires pam_faillock (and keeps pam_pwquality) on the
// RHEL family with authselect. Stock RHEL-family images do not load
// pam_faillock. When authselect already manages the stack (Fedora ships
// the `local` profile selected) the feature is enabled on the current
// profile. When it does not (Rocky/Alma/RHEL containers ship plain
// /etc/pam.d files) the local-users profile is selected: `local` on
// releases that have it, `minimal` on EL9 where it has not been renamed
// yet. Both load pam_pwquality in the password stack.
const CISAuthselectFaillock = `if authselect current >/dev/null 2>&1; then
  authselect enable-feature with-faillock
else
  profile=minimal
  authselect list | grep -q '^- local' && profile=local
  authselect select "$profile" with-faillock --force
fi`

// CISRHELFaillockFallback wires pam_faillock into system-auth and
// password-auth on a RHEL-family image that has no authselect binary.
// Only plain files are touched (an authselect-managed stack is a symlink
// into /etc/authselect and is left to CISAuthselectFaillock), only files
// that do not already load pam_faillock, and only when both anchor lines
// exist, so a stack of unexpected shape is left as is. The lines land in
// the same positions authselect's with-faillock feature puts them.
const CISRHELFaillockFallback = `for f in /etc/pam.d/system-auth /etc/pam.d/password-auth; do
  [ -f "$f" ] && [ ! -L "$f" ] || continue
  grep -q pam_faillock.so "$f" && continue
  grep -Eq '^auth[[:space:]]+[^[:space:]]+[[:space:]]+pam_unix.so' "$f" || continue
  grep -Eq '^account[[:space:]]+required[[:space:]]+pam_unix.so' "$f" || continue
  awk '
    !a && /^auth[[:space:]]+[^[:space:]]+[[:space:]]+pam_unix.so/ {
      print "auth        required      pam_faillock.so preauth silent"; print
      print "auth        required      pam_faillock.so authfail"; a=1; next }
    !c && /^account[[:space:]]+required[[:space:]]+pam_unix.so/ {
      print "account     required      pam_faillock.so"; print; c=1; next }
    { print }
  ' "$f" > "$f.kairos" && cat "$f.kairos" > "$f" && rm -f "$f.kairos"
done`

// CISSUSEPwquality wires pam_pwquality on SUSE with pam-config, replacing
// pam_cracklib (the SUSE default, which ignores pwquality.conf). Only
// pam-config builds that know the module (Tumbleweed; not Leap 15.6) are
// touched. pam-config has no faillock module, so faillock is not wired on
// SUSE.
const CISSUSEPwquality = `pam-config -a --pwquality
pam-config -q --cracklib >/dev/null 2>&1 && pam-config -d --cracklib || true`

// CISLoginDefsDirection tells the stage code how to compare the base
// image's shipped value against the CIS floor: which direction "stricter"
// is for that key.
type CISLoginDefsDirection string

const (
	// CISLoginDefsLowerStricter marks keys where a numerically lower value
	// is more restrictive (e.g. PASS_MAX_DAYS: 60 is stricter than 365).
	// The stage keeps the shipped value if it is already <= the CIS value.
	CISLoginDefsLowerStricter CISLoginDefsDirection = "lower-stricter"
	// CISLoginDefsHigherStricter marks keys where a numerically higher
	// value is more restrictive (PASS_MIN_DAYS, PASS_WARN_AGE, and UMASK
	// interpreted as octal digits: 077 masks more bits than 027 so it is
	// stricter). The stage keeps the shipped value if it is already >= the
	// CIS value.
	CISLoginDefsHigherStricter CISLoginDefsDirection = "higher-stricter"
	// CISLoginDefsSetIfUnset marks keys where the shipped value is opaque
	// (ENCRYPT_METHOD SHA512 vs YESCRYPT are both CIS-compliant), so the
	// stage only writes the CIS value when the base ships no value at all.
	CISLoginDefsSetIfUnset CISLoginDefsDirection = "set-if-unset"
)

// CISLoginDefsSetting is one key kairos-init pins in /etc/login.defs to
// satisfy the CIS L1 password-aging and umask controls. Applied with sed
// so the base distro's surrounding comments and unrelated defaults stay,
// and only tightened where the shipped value is weaker than CIS L1.
type CISLoginDefsSetting struct {
	Key       string
	Value     string
	Direction CISLoginDefsDirection
}

// CISLoginDefsSettings covers CIS Distribution Independent Linux v2.0.0 L1
// sections 5.4.1.1-5.4.1.5 (password aging) and 5.4.5 (default user umask).
// Each entry names the CIS floor and the direction "stricter" runs in for
// that key, so a base whose shipped value already meets or exceeds the
// benchmark keeps its own value rather than getting loosened to the floor.
// Only newly created accounts pick these up, so tightening cannot lock out
// an existing operator.
var CISLoginDefsSettings = []CISLoginDefsSetting{
	{Key: "PASS_MAX_DAYS", Value: "365", Direction: CISLoginDefsLowerStricter},
	{Key: "PASS_MIN_DAYS", Value: "1", Direction: CISLoginDefsHigherStricter},
	{Key: "PASS_WARN_AGE", Value: "7", Direction: CISLoginDefsHigherStricter},
	{Key: "UMASK", Value: "027", Direction: CISLoginDefsHigherStricter},
	{Key: "ENCRYPT_METHOD", Value: "SHA512", Direction: CISLoginDefsSetIfUnset},
}

// CISCronPath is one filesystem entry whose mode CIS L1 section 5.1
// (cron and at) pins down. Modes are octal because unlike the account
// databases in section 6.1 there is no PAM helper that needs a group
// bit preserved: cron and atd run as root, they read these paths as
// root, and everything else is out.
type CISCronPath struct {
	Path string
	Mode string
}

// CISCronPaths lists the cron and at paths CIS L1 sections 5.1.2-5.1.9
// require to be root-owned and inaccessible to non-root users. The
// permissions here match the benchmark; the chmods are guarded on the
// path existing because base images ship different subsets (Alpine has
// no /etc/cron.d, Ubuntu has no /etc/at.deny by default) and creating
// what a base did not ship would either enable a subsystem the image
// deliberately left out (cron.d) or lock everyone out of at (at.allow
// without at.deny).
var CISCronPaths = []CISCronPath{
	{Path: "/etc/crontab", Mode: "0600"},
	{Path: "/etc/cron.hourly", Mode: "0700"},
	{Path: "/etc/cron.daily", Mode: "0700"},
	{Path: "/etc/cron.weekly", Mode: "0700"},
	{Path: "/etc/cron.monthly", Mode: "0700"},
	{Path: "/etc/cron.d", Mode: "0700"},
	{Path: "/etc/cron.allow", Mode: "0640"},
	{Path: "/etc/cron.deny", Mode: "0640"},
	{Path: "/etc/at.allow", Mode: "0640"},
	{Path: "/etc/at.deny", Mode: "0640"},
}
