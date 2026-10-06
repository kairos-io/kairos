package stages

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/schema"
)

// cisAccountFile is one of the account databases whose mode CIS Distribution
// Independent Linux v2.0.0 L1 section 6.1 pins down.
type cisAccountFile struct {
	path string
	mode string
}

// cisAccountFiles lists those databases with the chmod expression that brings
// them in line with the benchmark.
//
// The modes are symbolic, not octal, because the bits the base distros ship
// differ and only some of them are safe to flatten. /etc/shadow is root:shadow
// mode 0640 on Debian-family and Alpine bases, where the setgid unix_chkpwd
// helper reads it through that group for PAM password auth; it is root:root
// mode 0000 on RedHat-family ones, where unix_chkpwd is setuid instead.
// Clearing `o` and the group write/execute bits satisfies the control on both
// without deciding which of the two layouts the base is using - and a
// subtractive expression can only ever tighten, so a base that already ships
// something stricter keeps it.
//
// The backups are taken down to owner-only: nothing reads them at runtime,
// they are written by the shadow utilities running as root, and the benchmark
// wants 0600 on all four.
var cisAccountFiles = []cisAccountFile{
	{path: "/etc/passwd", mode: "u-x,go-wx"},
	{path: "/etc/group", mode: "u-x,go-wx"},
	{path: "/etc/shadow", mode: "u-x,g-wx,o-rwx"},
	{path: "/etc/gshadow", mode: "u-x,g-wx,o-rwx"},
	{path: "/etc/passwd-", mode: "u-x,go-rwx"},
	{path: "/etc/group-", mode: "u-x,go-rwx"},
	{path: "/etc/shadow-", mode: "u-x,go-rwx"},
	{path: "/etc/gshadow-", mode: "u-x,go-rwx"},
}

// GetCISHardeningStage applies the CIS Distribution Independent Linux v2.0.0
// Level 1 controls that can be baked into the image: the section 1 "Initial
// Setup" ones - the filesystem module blocklist (1.1.1.1-1.1.1.6) and the
// remote login warning banner (1.7) - the section 3 kernel and network
// sysctl hardening, the section 4.1 baseline audit rules and auditd enable,
// the section 5.1 cron/at path permissions, the section 5.4 password quality,
// lockout and aging defaults, plus the section 6.1 "System File Permissions"
// modes on the account databases.
//
// Section 6 (time sync) is not covered here: steps_init.go already enables
// systemd-timesyncd on Debian/Ubuntu/SUSE/Hadron and chronyd on the RHEL
// family, and both ship distro-default NTP sources that satisfy CIS 6.
// SELinux enforcing on RHEL is left for a follow-up ticket. pam_faillock and
// pam_pwquality are wired into the PAM stack with each distro's own tool
// (pam-auth-update on Debian/Ubuntu, authselect on the RHEL family,
// pam-config for pwquality on SUSE); Hadron's system-auth loads both
// itself. SUSE gets no faillock (pam-config has no module for it) and
// Alpine images do not authenticate through PAM.
func GetCISHardeningStage(sis values.System, l logger.KairosLogger) []schema.Stage {
	if config.ContainsSkipStep(values.CISHardeningStep) {
		l.Logger.Warn().Msg("Skipping CIS hardening stage")
		return []schema.Stage{}
	}

	stages := []schema.Stage{
		{
			Name: "Blocklist uncommon filesystem modules",
			Files: []schema.File{
				{
					Path:        bundled.CISModprobeBlocklistPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISModprobeBlocklist,
				},
			},
		},
		{
			// Overwrites whatever the base shipped: Ubuntu and Debian
			// put their release name in here, which is exactly what the
			// control forbids.
			Name: "Install the remote login warning banner",
			Files: []schema.File{
				{
					Path:        bundled.IssueNetPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.IssueNetBanner,
				},
			},
		},
		{
			Name: "Install CIS kernel and network sysctl hardening",
			Files: []schema.File{
				{
					Path:        bundled.CISSysctlPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISSysctl,
				},
			},
		},
		{
			Name: "Install CIS baseline audit rules",
			Files: []schema.File{
				{
					Path:        bundled.CISAuditRulesPath,
					Permissions: 0640,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISAuditRules,
				},
			},
		},
		{
			Name:                 "Enable auditd service",
			OnlyIfServiceManager: serviceManagerSystemd,
			If:                   "test -f /usr/lib/systemd/system/auditd.service -o -f /lib/systemd/system/auditd.service",
			Systemctl: schema.Systemctl{
				Enable: []string{"auditd"},
			},
		},
		{
			// Alpine's openrc auditd loads a single rules file at start,
			// with no augenrules step; point it at the CIS drop-in
			// directly so `-e 2` and the 30 rules reach the kernel.
			Name:     "Point Alpine openrc auditd at the CIS rules drop-in",
			OnlyIfOs: values.AlpineRegex,
			Files: []schema.File{
				{
					Path:        bundled.CISAuditdConfDPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISAuditdConfDAlpine,
				},
			},
		},
	}

	stages = append(stages,
		schema.Stage{
			// CIS 6.1.5 checks /etc/gshadow, which Hadron does not ship.
			Name:     "Create /etc/gshadow",
			If:       "test ! -e /etc/gshadow && command -v grpconv >/dev/null",
			Commands: []string{"grpconv"},
		},
		schema.Stage{
			// CIS 6.1.6-6.1.9 check the `-` backups too. A copy of the live
			// database is what a shadow utility would have left there.
			Name: "Create missing account database backups",
			Commands: []string{
				"for f in passwd group shadow gshadow; do [ -e /etc/$f- ] || [ ! -e /etc/$f ] || cp -p /etc/$f /etc/$f-; done",
			},
		},
	)

	for _, f := range cisAccountFiles {
		stages = append(stages, schema.Stage{
			// Guarded on existence rather than done with a yip file
			// entry: the `-` backups only appear once a shadow utility
			// has rotated them, and a file entry would create the
			// missing ones as empty account databases.
			Name: fmt.Sprintf("Tighten permissions on %s", f.path),
			If:   fmt.Sprintf("test -f %s", f.path),
			Commands: []string{
				fmt.Sprintf("chmod %s %s", f.mode, f.path),
			},
		})
	}

	stages = append(stages,
		schema.Stage{
			Name: "Install CIS pwquality password policy",
			Files: []schema.File{
				{
					Path:        bundled.CISPwqualityPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISPwquality,
				},
			},
		},
		schema.Stage{
			// Read by pam_faillock once the wiring stages below (or
			// Hadron's own system-auth) put the module in the stack.
			Name: "Install CIS faillock lockout policy",
			Files: []schema.File{
				{
					Path:        bundled.CISFaillockPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISFaillock,
				},
			},
		},
		schema.Stage{
			Name: "Install the faillock tally reset for first-boot provisioning",
			Files: []schema.File{
				{
					Path:        bundled.CISFaillockResetCloudConfigPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISFaillockResetCloudConfig,
				},
			},
		},
	)

	stages = append(stages, getCISPamWiringStages()...)

	// login.defs: per-key "only tighten" logic. The stage never loosens a
	// base image's stricter value (Hadron ships PASS_MAX_DAYS 60 and
	// UMASK 077, both tighter than CIS; overwriting with CIS's 365 and
	// 027 would weaken the shipped policy and break Hadron's own
	// acceptance suite). Detection deliberately does not match commented
	// documentation (`# UMASK is the default...`) or commented defaults
	// (`#UMASK 022`); missing-key path uses printf (echo does not expand
	// `\t` under sh) and guards the trailing newline (Hadron's stock
	// /etc/login.defs has none, so a naked append glues the new key onto
	// the last line).
	loginDefsCommands := []string{}
	for _, s := range bundled.CISLoginDefsSettings {
		var keepIf string
		switch s.Direction {
		case bundled.CISLoginDefsLowerStricter:
			// Keep the shipped value when it is already <= the CIS floor.
			keepIf = fmt.Sprintf("[ \"$cur\" -le %s ]", s.Value)
		case bundled.CISLoginDefsHigherStricter:
			// UMASK is written as `077` and treated as a decimal integer
			// by shell arithmetic (`[ 077 -gt 027 ]` compares 77 vs 27),
			// which happens to give the right ordering for umask masks
			// too: more bits masked means a higher three-digit decimal.
			keepIf = fmt.Sprintf("[ \"$cur\" -ge %s ]", s.Value)
		case bundled.CISLoginDefsSetIfUnset:
			// Any shipped value is kept; only fill in when unset.
			keepIf = "[ -n \"$cur\" ]"
		}
		loginDefsCommands = append(loginDefsCommands, fmt.Sprintf(
			"cur=$(awk '/^[[:space:]]*%s[[:space:]]/{print $2; exit}' /etc/login.defs); "+
				"if %s; then :; "+
				"elif [ -n \"$cur\" ]; then "+
				"sed -i -E 's|^([[:space:]]*)%s[[:space:]]+.*$|\\1%s\\t%s|' /etc/login.defs; "+
				"else "+
				"[ -n \"$(tail -c1 /etc/login.defs)\" ] && printf '\\n' >> /etc/login.defs; "+
				"printf '%s\\t%s\\n' >> /etc/login.defs; "+
				"fi",
			s.Key, keepIf, s.Key, s.Key, s.Value, s.Key, s.Value,
		))
	}
	stages = append(stages, schema.Stage{
		Name:     "Set CIS password aging and umask defaults in /etc/login.defs",
		If:       "test -f /etc/login.defs",
		Commands: loginDefsCommands,
	})

	for _, c := range bundled.CISCronPaths {
		stages = append(stages, schema.Stage{
			// Guarded on existence: base images ship different
			// subsets and creating what a base did not ship would
			// either enable a subsystem (cron.d) or lock everyone
			// out of at (at.allow without at.deny).
			Name: fmt.Sprintf("Tighten permissions on %s", c.Path),
			If:   fmt.Sprintf("test -e %s", c.Path),
			Commands: []string{
				fmt.Sprintf("chown root:root %s", c.Path),
				fmt.Sprintf("chmod %s %s", c.Mode, c.Path),
			},
		})
	}

	stages = append(stages, getCISBenchmarkGapStages()...)

	return stages
}

// getCISBenchmarkGapStages covers the controls the CIS DIL benchmark run
// against an installed Hadron system (tests/cis_test.go) found failing.
func getCISBenchmarkGapStages() []schema.Stage {
	journald := []string{}
	for _, kv := range bundled.CISJournaldSettings {
		// Drop any active value, make sure there is a [Journal] section, then
		// put the key right under it. Commented defaults are left alone.
		journald = append(journald, fmt.Sprintf(
			"f=%[1]s; sed -i -E '/^[[:space:]]*%[2]s=/d' \"$f\"; "+
				"if ! grep -q '^\\[Journal\\]' \"$f\"; then [ -n \"$(tail -c1 \"$f\")\" ] && printf '\\n' >> \"$f\"; printf '[Journal]\\n' >> \"$f\"; fi; "+
				"sed -i '/^\\[Journal\\]/a %[2]s=%[3]s' \"$f\"",
			bundled.CISJournaldConfPath, kv[0], kv[1]))
	}

	return []schema.Stage{
		{
			Name: "Install the CIS core dump limit",
			Files: []schema.File{
				{
					Path:        bundled.CISCoreDumpLimitsPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISCoreDumpLimits,
				},
			},
		},
		{
			Name:     "Set CIS journald compression and persistent storage",
			If:       "test -f " + bundled.CISJournaldConfPath,
			Commands: journald,
		},
		{
			// CIS 4.2.3. Files created later by kairos itself are opened
			// 0640 by the sdk logger.
			Name:     "Restrict log file permissions",
			If:       "test -d /var/log",
			Commands: []string{"find /var/log -type f -exec chmod g-wx,o-rwx {} +"},
		},
		{
			// CIS 5.3.3. Hadron's system-auth is plain PAM; the other
			// families manage their stacks with their own tools. The
			// benchmark wants single spaces after pam_unix.so, so the
			// arguments are rewritten with one.
			Name:     "Remember previous passwords in the Hadron PAM stack",
			OnlyIfOs: "Hadron.*",
			If:       "test -f /etc/pam.d/system-auth",
			Commands: []string{
				"grep -qE '^password[[:space:]].*pam_unix\\.so.*remember=' /etc/pam.d/system-auth || " +
					"sed -i -E 's/^(password[[:space:]]+[^[:space:]]+[[:space:]]+pam_unix\\.so)[[:space:]]+(.*[^[:space:]])[[:space:]]*$/\\1 \\2 remember=5/' /etc/pam.d/system-auth",
				"[ -e /etc/security/opasswd ] || install -m 0600 /dev/null /etc/security/opasswd",
			},
		},
		{
			// CIS 5.4.4 reads the shell startup files, not login.defs. Use
			// the login.defs UMASK when it already meets the 027 floor
			// (Hadron ships 077), else 027.
			Name: "Set the CIS default umask for shells",
			Commands: []string{
				"u=$(awk '/^[[:space:]]*UMASK[[:space:]]/{print $2; exit}' /etc/login.defs 2>/dev/null); " +
					"case \"$u\" in [0-7][2367]7) ;; *) u=027 ;; esac; " +
					"for f in /etc/profile /etc/bash.bashrc /etc/bashrc; do " +
					"[ -f \"$f\" ] || continue; " +
					"grep -qE '^[[:space:]]*umask[[:space:]]+[0-7][2367]7[[:space:]]*$' \"$f\" && continue; " +
					"[ -n \"$(tail -c1 \"$f\")\" ] && printf '\\n' >> \"$f\"; " +
					"printf 'umask %s\\n' \"$u\" >> \"$f\"; " +
					"done",
			},
		},
		{
			// CIS 5.6. Root still gets in through pam_rootok and sudo is
			// not affected.
			Name: "Restrict su to the wheel group",
			If:   "test -f /etc/pam.d/su",
			Commands: []string{
				"if grep -qE '^auth[[:space:]]+required[[:space:]]+pam_wheel\\.so use_uid$' /etc/pam.d/su; then :; " +
					"elif grep -qE '^#[[:space:]]*auth[[:space:]]+required[[:space:]]+pam_wheel\\.so[[:space:]]+use_uid' /etc/pam.d/su; then " +
					"sed -i -E 's/^#[[:space:]]*auth[[:space:]]+required[[:space:]]+pam_wheel\\.so[[:space:]]+use_uid.*$/auth required pam_wheel.so use_uid/' /etc/pam.d/su; " +
					"else sed -i '/pam_rootok\\.so/a auth required pam_wheel.so use_uid' /etc/pam.d/su; fi",
			},
		},
		{
			// CIS 6.1.12. Hadron ships the helper with a gid that has no
			// group. dbus expects it owned by messagebus; chgrp clears the
			// setuid bit, so the mode is put back.
			Name: "Give the dbus launch helper its messagebus group",
			If:   "command -v getent >/dev/null && getent group messagebus >/dev/null",
			Commands: []string{
				"for f in /usr/libexec/dbus-daemon-launch-helper /usr/lib/dbus-1.0/dbus-daemon-launch-helper; do " +
					"[ -e \"$f\" ] || continue; " +
					"getent group \"$(stat -c %g \"$f\")\" >/dev/null && continue; " +
					"m=$(stat -c %a \"$f\"); chgrp messagebus \"$f\"; chmod \"$m\" \"$f\"; " +
					"done",
			},
		},
		{
			Name: "Install the CIS boot-time hardening config",
			Files: []schema.File{
				{
					Path:        bundled.CISBootCloudConfigPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISBootCloudConfig,
				},
			},
		},
	}
}

// debianFamilyRegex matches the Debian-family bases pam-auth-update
// manages.
const debianFamilyRegex = "Ubuntu.*|Debian.*"

// pamModulePresent builds an If condition that is true only when the given
// PAM module is installed under any of the multiarch or lib64 security
// directories the supported distros use. Wiring a module that is not on
// disk into a stack fails every authentication through it.
func pamModulePresent(module string) string {
	return fmt.Sprintf("ls /usr/lib/*/security/%[1]s /lib/*/security/%[1]s /usr/lib64/security/%[1]s /lib64/security/%[1]s /usr/lib/security/%[1]s /lib/security/%[1]s 2>/dev/null | grep -q .", module)
}

// getCISPamWiringStages wires pam_faillock (CIS L1 5.4.2) and
// pam_pwquality (5.4.1) into the auth and password stacks so the shipped
// faillock.conf and pwquality.conf take effect. Each stage runs only on its
// distro family and only when the tool and the module are both present;
// a stage that does not apply is skipped, never approximated.
func getCISPamWiringStages() []schema.Stage {
	return []schema.Stage{
		{
			// Profiles are written in the same stage that enables them,
			// guarded on the module: pam-auth-update enables Default: yes
			// profiles on any later run, so a profile left on disk
			// without the module would lock everyone out later.
			Name:     "Wire pam_faillock into the Debian/Ubuntu PAM stack",
			OnlyIfOs: debianFamilyRegex,
			If:       "command -v pam-auth-update >/dev/null && " + pamModulePresent("pam_faillock.so"),
			Files: []schema.File{
				{
					Path:        bundled.CISPamConfigFaillockPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISPamConfigFaillock,
				},
				{
					Path:        bundled.CISPamConfigFaillockNotifyPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.CISPamConfigFaillockNotify,
				},
			},
			Commands: []string{
				"DEBIAN_FRONTEND=noninteractive pam-auth-update --enable kairos-faillock kairos-faillock-notify",
			},
		},
		{
			// libpam-pwquality ships its own profile; enabling it again
			// is idempotent and covers an image where it was disabled.
			Name:     "Wire pam_pwquality into the Debian/Ubuntu PAM stack",
			OnlyIfOs: debianFamilyRegex,
			If:       "command -v pam-auth-update >/dev/null && test -f /usr/share/pam-configs/pwquality && " + pamModulePresent("pam_pwquality.so"),
			Commands: []string{
				"DEBIAN_FRONTEND=noninteractive pam-auth-update --enable pwquality",
			},
		},
		{
			Name:     "Wire pam_faillock into the RHEL-family PAM stack with authselect",
			OnlyIfOs: values.RHELFamilyRegex,
			If:       "command -v authselect >/dev/null && " + pamModulePresent("pam_faillock.so"),
			Commands: []string{bundled.CISAuthselectFaillock},
		},
		{
			Name:     "Wire pam_faillock into the RHEL-family PAM stack without authselect",
			OnlyIfOs: values.RHELFamilyRegex,
			If:       "! command -v authselect >/dev/null && " + pamModulePresent("pam_faillock.so"),
			Commands: []string{bundled.CISRHELFaillockFallback},
		},
		{
			Name:     "Wire pam_pwquality into the SUSE PAM stack with pam-config",
			OnlyIfOs: values.AllSuseRegex,
			If:       "command -v pam-config >/dev/null && pam-config --help 2>&1 | grep -Eq -- '--pwquality( |$)' && " + pamModulePresent("pam_pwquality.so"),
			Commands: []string{bundled.CISSUSEPwquality},
		},
	}
}
