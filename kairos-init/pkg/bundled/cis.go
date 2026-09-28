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

// Syscall rules are paired b64+b32. On amd64 with CONFIG_IA32_EMULATION and on
// aarch64 with CONFIG_COMPAT (userspace's arch=b32 -> AUDIT_ARCH_ARM) a 32-bit
// binary would otherwise bypass every b64-only rule. `auditctl -R` warns and
// keeps loading past b32 lines the running kernel rejects, so shipping both
// pairs is safe on kernels without 32-bit compat.
const CISAuditRules = `-a always,exit -F arch=b64 -S adjtimex,settimeofday,clock_settime -k time-change
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
# Wiring pam_pwquality into the PAM password stack is distro-specific
# (authselect on RHEL, pam-auth-update on Debian/Ubuntu, hand-edited on
# Alpine) and left to the base image; RHEL 9 and Ubuntu 22.04+ enable
# the module by default once the pwquality package is present.
minlen = 14
dcredit = -1
ucredit = -1
ocredit = -1
lcredit = -1
difok = 4
`

// CISFaillockPath is the pam_faillock config file, read at PAM stack time.
// Whether shipping the file also enforces lockout depends on the base:
// Hadron's /etc/pam.d/system-auth already wires pam_faillock (preauth,
// authfail, authsucc), so the CIS parameters take effect on Hadron as
// soon as this file lands. RHEL 9's default authselect profile also
// loads pam_faillock. On Ubuntu, Debian and Alpine bases the module
// is not in the auth stack out of the box; the file has no effect
// there until pam_faillock is wired in through the distro's standard
// mechanism (pam-auth-update on Debian, authselect on RHEL, hand-edited
// common-auth on Alpine). That wiring is distro-specific and a wrong
// edit locks every account out, so it is left for a follow-up ticket
// with proper per-distro boot testing.
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
# Whether this file changes runtime behavior depends on the base:
# Hadron's system-auth already wires pam_faillock, and RHEL 9's default
# authselect profile also loads it, so the CIS parameters take effect on
# those bases as soon as this file lands. On Ubuntu, Debian and Alpine
# bases pam_faillock is not in the auth stack out of the box, so the
# file is inert there until the module is wired in. That wiring is
# distro-specific and a wrong edit locks every account out, so it is
# left for a follow-up ticket with proper per-distro boot testing.
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

// CISLoginDefsSetting is one key kairos-init pins in /etc/login.defs to
// satisfy the CIS L1 password-aging and umask controls. Applied with sed
// so the base distro's surrounding comments and unrelated defaults stay.
type CISLoginDefsSetting struct {
	Key   string
	Value string
}

// CISLoginDefsSettings covers CIS Distribution Independent Linux v2.0.0 L1
// sections 5.4.1.1-5.4.1.5 (password aging) and 5.4.5 (default user umask).
// Only newly created accounts pick these up, so tightening them cannot lock
// out an existing operator. UMASK 027 matches the benchmark; a stricter 077
// breaks group-shared directories on the base images.
var CISLoginDefsSettings = []CISLoginDefsSetting{
	{Key: "PASS_MAX_DAYS", Value: "365"},
	{Key: "PASS_MIN_DAYS", Value: "1"},
	{Key: "PASS_WARN_AGE", Value: "7"},
	{Key: "UMASK", Value: "027"},
	{Key: "ENCRYPT_METHOD", Value: "SHA512"},
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
