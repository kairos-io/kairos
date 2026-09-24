package schema

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// InstallSchema represents the install block in the Kairos configuration. It is used to drive automatic installations without user interaction.
type InstallSchema struct {
	_                   struct{}          `title:"Kairos Schema: Install block" description:"The install block is to drive automatic installations without user interaction."`
	Auto                bool              `json:"auto,omitempty" description:"Set to true when installing without Pairing"`
	BindMounts          []string          `json:"bind_mounts,omitempty"`
	Bundles             []BundleSchema    `json:"bundles,omitempty" description:"Add bundles in runtime"`
	NoFormat            bool              `json:"no-format,omitempty" description:"Skip formatting the partitions and reuse the existing layout"`
	NoFormatDeprecated  bool              `json:"no_format,omitempty" deprecated:"true" description:"Deprecated and ignored: it was never read by the installer. Use no-format instead"`
	Device              string            `json:"device,omitempty" pattern:"^(auto|/dev/.+|script://.+)$" description:"Device for automated installs" examples:"[\"auto\",\"/dev/sda\",\"script:///usr/local/bin/pick-disk.sh\"]"`
	EphemeralMounts     []string          `json:"ephemeral_mounts,omitempty"`
	EncryptedPartitions []string          `json:"encrypted_partitions,omitempty"`
	Env                 []interface{}     `json:"env,omitempty"`
	Extensions          []ExtensionSchema `json:"extensions,omitempty" description:"System extensions to install onto the node."`
	GrubOptionsSchema   `json:"grub_options,omitempty"`
	SelinuxOptions      `json:"selinux,omitempty"`
	Image               string `json:"image,omitempty" description:"Use a different container image for the installation"`
	PowerManagement
	SkipEncryptCopyPlugins  bool                `json:"skip_copy_kcrypt_plugin,omitempty"`
	Partitions              ElementalPartitions `json:"partitions,omitempty"`
	GrubDefEntry            string              `json:"grub-entry-name,omitempty"`
	ExtraPartitions         []*Partition        `json:"extra-partitions,omitempty"`
	Force                   bool                `json:"force,omitempty"`
	ExtraDirsRootfs         []string            `json:"extra-dirs-rootfs,omitempty"`
	SSHHardening            bool                `json:"ssh_hardening,omitempty" description:"Enforce the DevSec ssh-baseline auth-mode controls on the installed system (PasswordAuthentication no, AuthenticationMethods publickey, ChallengeResponseAuthentication no). Requires at least one user with ssh_authorized_keys; a password on the same user is unusable and flagged as a warning."`
	AllowInsecureRegistries bool                `json:"allow-insecure-registries,omitempty" description:"Allow image pulls from registries using plain HTTP or untrusted TLS certificates."`
	RegistryAuth            *RegistryAuthSchema `json:"registry-auth,omitempty" nullable:"true" description:"Explicit credentials for this operation. Credentials may be persisted in the copied cloud-config."`
	Active                  Image               `json:"system,omitempty"`
	Recovery                Image               `json:"recovery-system,omitempty"`
	Passive                 Image               `json:"passive,omitempty"`
}

// RegistryAuthSchema describes one explicit registry credential form. Exactly
// one form is accepted by the agent at runtime.
type RegistryAuthSchema struct {
	File          string `json:"file,omitempty" description:"Path to a YAML credential object available before the operation starts. Cannot be combined with inline credentials."`
	Username      string `json:"username,omitempty" description:"Registry username. Must be provided with password."`
	Password      string `json:"password,omitempty" description:"Registry password."`
	Auth          string `json:"auth,omitempty" description:"Base64 encoded username:password."`
	IdentityToken string `json:"identity-token,omitempty" description:"Registry identity token."`
	RegistryToken string `json:"registry-token,omitempty" description:"Registry bearer token."`
}

// UpgradeSchema documents the upgrade registry settings consumed by the agent.
// Other upgrade keys remain unconstrained by this schema.
type UpgradeSchema struct {
	AllowInsecureRegistries bool                `json:"allow-insecure-registries,omitempty" description:"Allow image pulls from registries using plain HTTP or untrusted TLS certificates."`
	RegistryAuth            *RegistryAuthSchema `json:"registry-auth,omitempty" nullable:"true" description:"Explicit credentials for this operation. Credentials may be persisted in the copied cloud-config."`
}

type Image struct {
	Size   uint   `json:"size,omitempty"`
	Source string `json:"uri,omitempty"`
}

type Partition struct {
	Name string `json:"name,omitempty"`
	Size uint   `json:"size,omitempty"`
	FS   string `json:"fs,omitempty"`
}

type ElementalPartitions struct {
	OEM        *Partition `json:"oem,omitempty"`
	Recovery   *Partition `json:"recovery,omitempty"`
	State      *Partition `json:"state,omitempty"`
	Persistent *Partition `json:"persistent,omitempty"`
}

// BundleSchema represents the bundle block which can be used in different places of the Kairos configuration. It is used to reference a bundle and its confguration.
type BundleSchema struct {
	DB         string   `json:"db_path,omitempty"`
	LocalFile  bool     `json:"local_file,omitempty"`
	Repository string   `json:"repository,omitempty"`
	Rootfs     string   `json:"rootfs_path,omitempty"`
	Targets    []string `json:"targets,omitempty"`
}

// GrubOptionsSchema represents the grub options block which can be used in different places of the Kairos configuration. It is used to configure grub.
type GrubOptionsSchema struct {
	DefaultFallback      string `json:"default_fallback,omitempty" description:"Sets default fallback logic"`
	DefaultMenuEntry     string `json:"default_menu_entry,omitempty" description:"Change GRUB menu entry"`
	ExtraActiveCmdline   string `json:"extra_active_cmdline,omitempty" description:"Additional Kernel option cmdline to apply just for active"`
	ExtraCmdline         string `json:"extra_cmdline,omitempty" description:"Additional Kernel option cmdline to apply"`
	ExtraPassiveCmdline  string `json:"extra_passive_cmdline,omitempty" description:"Additional Kernel option cmdline to apply just for passive"`
	ExtraRecoveryCmdline string `json:"extra_recovery_cmdline,omitempty" description:"Set additional boot commands when booting into recovery"`
	NextEntry            string `json:"next_entry,omitempty" description:"Set the next reboot entry."`
	SavedEntry           string `json:"saved_entry,omitempty" description:"Set the default boot entry."`
}

// SelinuxOptions controls SELinux on the installed system (RHEL and SUSE
// families). When enabled, the system boots with selinux=1
// and the kairos-selinux-relabel unit runs on every non-recovery boot.
type SelinuxOptions struct {
	Enabled bool   `json:"enabled,omitempty" description:"Install SELinux packages and boot with SELinux active (RHEL and SUSE families, incl. openSUSE Tumbleweed). GRUB-only: not supported under UKI"`
	Mode    string `json:"mode,omitempty" enum:"[\"enforcing\",\"permissive\"]" description:"SELinux mode: enforcing or permissive (default permissive). Enforcing is applied after the post-boot relabel, not from early boot"`
}

// PowerManagement is a meta structure to hold the different rules for managing power, which are not compatible between each other.
type PowerManagement struct{}

// NoPowerManagement is a meta structure used when the user does not define any power management options or when the user does not want to reboot or poweroff the machine.
type NoPowerManagement struct {
	Reboot   bool `json:"reboot,omitempty" const:"false" default:"false" description:"Reboot after installation"`
	Poweroff bool `json:"poweroff,omitempty" const:"false" default:"false" description:"Power off after installation"`
}

// RebootOnly is a meta structure used to enforce that when the reboot option is set, the poweroff option is not set.
type RebootOnly struct {
	Reboot   bool `json:"reboot,omitempty" const:"true" default:"false" required:"true" description:"Reboot after installation"`
	Poweroff bool `json:"poweroff,omitempty" const:"false" default:"false" description:"Power off after installation"`
}

// PowerOffOnly is a meta structure used to enforce that when the poweroff option is set, the reboot option is not set.
type PowerOffOnly struct {
	Reboot   bool `json:"reboot,omitempty" const:"false" default:"false" description:"Reboot after installation"`
	Poweroff bool `json:"poweroff,omitempty" const:"true" default:"false" required:"true" description:"Power off after installation"`
}

var _ jsonschemago.OneOfExposer = PowerManagement{}

// The OneOfModel interface is only needed for the tests that check the new schema contain all needed fields
// it can be removed once the new schema is the single source of truth.
type OneOfModel interface {
	JSONSchemaOneOf() []interface{}
}

// JSONSchemaOneOf defines that different which are the different valid power management rules and states that one and only one of them needs to be validated for the entire schema to be valid.
func (PowerManagement) JSONSchemaOneOf() []interface{} {
	return []interface{}{
		NoPowerManagement{}, RebootOnly{}, PowerOffOnly{},
	}
}
