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
// remote login warning banner (1.7) - plus the section 6.1 "System File
// Permissions" modes on the account databases.
//
// The rest of the benchmark - SELinux enforcing, sysctl, auditd, PAM, time
// sync - needs either runtime state or decisions that change how a node boots,
// and is not covered here.
func GetCISHardeningStage(_ values.System, l logger.KairosLogger) []schema.Stage {
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

	return stages
}
