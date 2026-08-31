package stages

import (
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/schema"
)

// hasSntrup761Kex is a POSIX shell snippet that exits 0 when the
// installed OpenSSH is version 8.5 or newer. sntrup761x25519-sha512@openssh.com
// was added in OpenSSH 8.5 (https://www.openssh.com/txt/release-8.5)
// and appears in the hardening drop-in's KexAlgorithms allow-list.
// Older OpenSSH (Ubuntu 20.04's 8.2p1, notably) rejects unknown
// algorithm names as a config parse error and refuses to start.
//
// Reads the version from `sshd -h` because sshd is always installed on
// Kairos targets. On every OpenSSH version -h errors but prints the
// version string to stderr along with the usage line, so 2>&1 + grep
// works uniformly. sort -V handles the version comparison portably.
const hasSntrup761Kex = `[ "$(printf '8.5\n%s\n' "$(sshd -h 2>&1 | grep -oE 'OpenSSH_[0-9]+\.[0-9]+' | head -1 | cut -d_ -f2)" | sort -V | head -1)" = "8.5" ]`

// GetSshHardeningStage installs the sshd hardening drop-in and filters the
// moduli file so it stops offering DH groups below 2048 bits. See
// bundled.SshdHardeningConfig for the ruleset; the spec we track is the
// DevSec ssh-baseline.
func GetSshHardeningStage(_ values.System, l logger.KairosLogger) []schema.Stage {
	if config.ContainsSkipStep(values.SshHardeningStep) {
		l.Logger.Warn().Msg("Skipping ssh hardening stage")
		return []schema.Stage{}
	}

	return []schema.Stage{
		{
			Name: "Install sshd hardening drop-in",
			If:   "test -d /etc/ssh && " + hasSntrup761Kex,
			Files: []schema.File{
				{
					Path:        bundled.SshdHardeningPath,
					Permissions: 0644,
					Owner:       0,
					Group:       0,
					Content:     bundled.SshdHardeningConfig,
				},
			},
		},
		{
			// Loud in the build log so operators building on a base with
			// pre-8.5 OpenSSH (Ubuntu 20.04) know why they don't have
			// the drop-in: installing it there would make sshd refuse
			// to start on the KexAlgorithms parse.
			Name: "Warn: sshd hardening drop-in skipped (OpenSSH < 8.5)",
			If:   "test -d /etc/ssh && ! " + hasSntrup761Kex,
			Commands: []string{
				`echo "WARNING: kairos-init sshd hardening drop-in skipped: OpenSSH < 8.5 does not know sntrup761x25519-sha512@openssh.com, sshd would refuse to start with the KexAlgorithms this drop-in sets. Use a base image with OpenSSH >= 8.5 (Hadron, Ubuntu 22.04+, Debian 12+, RHEL 9+, Alpine 3.17+) to receive the hardening." >&2`,
			},
		},
		{
			// Drop <2048-bit Diffie-Hellman groups to counter Logjam-style
			// precomputation attacks (https://weakdh.org, 2015). Threshold
			// is 2047 because moduli(5) column 5 is the bit-size of prime
			// P, and a "2048-bit" group has a 2047-bit P.
			Name: "Filter weak Diffie-Hellman moduli",
			If:   "test -f /etc/ssh/moduli",
			Commands: []string{
				// Single chained command so a failing awk (or an
				// empty output) never leaves /etc/ssh/moduli as a
				// broken/empty file. `[ -s ]` guards against awk
				// succeeding but producing no rows (would render
				// group-exchange KEX unusable). The `|| { rm ...; false; }`
				// tail removes the orphan moduli.strong on any failure
				// while preserving the non-zero exit so yip surfaces
				// the error rather than silently swallowing it.
				"awk '$5 >= 2047' /etc/ssh/moduli > /etc/ssh/moduli.strong && [ -s /etc/ssh/moduli.strong ] && mv /etc/ssh/moduli.strong /etc/ssh/moduli || { rm -f /etc/ssh/moduli.strong; false; }",
			},
		},
	}
}
