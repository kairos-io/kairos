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

const CISAuditRules = `-a always,exit -F arch=b64 -S adjtimex,settimeofday,clock_settime -k time-change
-w /etc/localtime -p wa -k time-change

-w /etc/group -p wa -k identity
-w /etc/passwd -p wa -k identity
-w /etc/gshadow -p wa -k identity
-w /etc/shadow -p wa -k identity
-w /etc/security/opasswd -p wa -k identity

-a always,exit -F arch=b64 -S sethostname,setdomainname -k system-locale
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
-a always,exit -F arch=b64 -S chown,fchown,lchown,fchownat -F auid>=1000 -F auid!=unset -F key=perm_mod
-a always,exit -F arch=b64 -S setxattr,lsetxattr,fsetxattr,removexattr,lremovexattr,fremovexattr -F auid>=1000 -F auid!=unset -F key=perm_mod

-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F exit=-EACCES -F auid>=1000 -F auid!=unset -F key=access
-a always,exit -F arch=b64 -S creat,open,openat,truncate,ftruncate -F exit=-EPERM -F auid>=1000 -F auid!=unset -F key=access

-a always,exit -F arch=b64 -S mount -F auid>=1000 -F auid!=unset -F key=mounts

-a always,exit -F arch=b64 -S unlink,unlinkat,rename,renameat -F auid>=1000 -F auid!=unset -F key=delete

-w /etc/sudoers -p wa -k scope
-w /etc/sudoers.d/ -p wa -k scope

-a always,exit -F arch=b64 -S init_module,delete_module,finit_module -F auid>=1000 -F auid!=unset -F key=modules

-e 2
`
