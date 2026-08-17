package hook

import (
	"github.com/kairos-io/kairos-sdk/machine"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
	"github.com/mudler/yip/pkg/schema"
	yip "github.com/mudler/yip/pkg/schema"
)

// SshdHardeningAuthnDropIn is the content of the auth-mode drop-in the
// SSHHardening hook installs when `ssh_hardening: true` is set in the
// cloud-config. It sits alongside the crypto/forwarding/timeout drop-in
// kairos-init already ships (05-kairos-hardening.conf) and enforces the
// three DevSec ssh-baseline controls kairos-init intentionally omits so
// first-boot password login can still work if no hardening is asked for.
//
// The 50- prefix loads AFTER kairos-init's 05-* (which is intended;
// kairos-init's file sets no auth-mode directives, so there's no
// conflict) and after any base-image drop-ins that live at 99-* / 100-*
// (also intended; we override any weaker auth-mode setting those may
// have). Users who want to further tune should drop 01-*.conf.
const SshdHardeningAuthnDropIn = `# Managed by kairos-agent (ssh_hardening: true). Enforces the auth-mode
# part of the DevSec ssh-baseline that kairos-init leaves permissive.
PasswordAuthentication no
AuthenticationMethods publickey
ChallengeResponseAuthentication no
`

// SshdHardeningAuthnPath is where SshdHardeningAuthnDropIn is installed.
const SshdHardeningAuthnPath = "/etc/ssh/sshd_config.d/50-kairos-hardening-authn.conf"

// SSHHardening writes a yip cloud-config to /oem so that on first boot
// the auth-mode DevSec baseline drop-in is installed and sshd is
// reloaded. Semantic validation (must have an authorised key, warn on
// leftover passwd) has already run in pkg/config.Scan.
type SSHHardening struct{}

func (SSHHardening) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if c.Install == nil || !c.Install.SSHHardening {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running SSHHardening hook")

	if err := machine.Mount("COS_OEM", "/oem"); err != nil {
		return err
	}
	defer func() {
		_ = machine.Umount("/oem")
	}()

	cfg := yip.YipConfig{
		Stages: map[string][]schema.Stage{
			"boot": {
				{
					Name: "Install DevSec ssh-baseline auth-mode drop-in",
					Files: []schema.File{
						{
							Path:        SshdHardeningAuthnPath,
							Permissions: 0o644,
							Owner:       0,
							Group:       0,
							Content:     SshdHardeningAuthnDropIn,
						},
					},
					Commands: []string{
						// reload-or-restart handles both "sshd already running"
						// (reload picks up the drop-in) and "sshd not yet up"
						// (restart-if-enabled brings it online with the drop-in
						// already in effect). Try both service names since the
						// unit name varies by distro (ssh on debian-family,
						// sshd on RHEL/Alpine).
						"systemctl reload-or-restart ssh || systemctl reload-or-restart sshd || true",
					},
				},
			},
		},
	}

	if err := saveCloudConfig("ssh_hardening", cfg); err != nil {
		return err
	}
	c.Logger.Logger.Debug().Msg("Finish SSHHardening hook")
	return nil
}
