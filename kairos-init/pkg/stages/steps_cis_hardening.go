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
// SELinux enforcing on RHEL is left for a follow-up ticket. pam_faillock:
// Hadron's system-auth and RHEL 9's authselect default already wire it, so
// the shipped faillock.conf takes effect there on its own. Wiring the
// module into the PAM auth stack on Ubuntu, Debian and Alpine bases is
// left for a follow-up ticket for the same reason (per-distro boot testing
// needed to avoid locking accounts out).
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
			// Inert until pam_faillock is present in the PAM auth
			// stack. Distros where it is not, ignore the file; distros
			// where it is (RHEL 9 default), pick up CIS parameters
			// without further work.
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
	)

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

	return stages
}
