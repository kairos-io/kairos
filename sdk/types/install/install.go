package install

import (
	"github.com/kairos-io/kairos/v4/sdk/types/bundles"
	"github.com/kairos-io/kairos/v4/sdk/types/extensions"
	"github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// You would probably be thinking, why is the Install struct in here? Well, the types
// package is already imported everywhere, so putting it here avoids cyclic imports
// and makes it easier to use across the codebase.
// Plus things like providers can import them in order to modify and send back the install info
// so its nice that its in a central place for providers to consume and be able to alter install behavior easily.

type Install struct {
	Auto                   bool                           `yaml:"auto,omitempty" json:"auto,omitempty"`
	Reboot                 bool                           `yaml:"reboot,omitempty" json:"reboot,omitempty"`
	NoFormat               bool                           `yaml:"no-format,omitempty" json:"no-format,omitempty"`
	Device                 string                         `yaml:"device,omitempty" json:"device,omitempty" pattern:"^(auto|/dev/.+|script://.+)$" description:"Device for automated installs" examples:"[\"auto\",\"/dev/sda\",\"script:///usr/local/bin/pick-disk.sh\"]"`
	Poweroff               bool                           `yaml:"poweroff,omitempty" json:"poweroff,omitempty"`
	GrubOptions            map[string]string              `yaml:"grub_options,omitempty" json:"grub_options,omitempty"`
	Selinux                SelinuxOptions                 `yaml:"selinux,omitempty" json:"selinux,omitempty"`
	Bundles                bundles.Bundles                `yaml:"bundles,omitempty" json:"bundles,omitempty"`
	Extensions             extensions.Extensions          `yaml:"extensions,omitempty" json:"extensions,omitempty" description:"System extensions to install onto the node. A catalog name, optionally with a version, or a URI or absolute path to an extension image."`
	Encrypt                []string                       `yaml:"encrypted_partitions,omitempty" json:"encrypted_partitions,omitempty"`
	SkipEncryptCopyPlugins bool                           `yaml:"skip_copy_kcrypt_plugin,omitempty" json:"skip_copy_kcrypt_plugin,omitempty"`
	Env                    []string                       `yaml:"env,omitempty" json:"env,omitempty"`
	Source                 string                         `yaml:"source,omitempty" json:"source,omitempty"`
	EphemeralMounts        []string                       `yaml:"ephemeral_mounts,omitempty" json:"ephemeral_mounts,omitempty"`
	BindMounts             []string                       `yaml:"bind_mounts,omitempty" json:"bind_mounts,omitempty"`
	Partitions             partitions.ElementalPartitions `yaml:"partitions,omitempty" json:"partitions,omitempty"`
	Active                 images.Image                   `yaml:"system,omitempty" json:"system,omitempty"`
	Recovery               images.Image                   `yaml:"recovery-system,omitempty" json:"recovery-system,omitempty"`
	Passive                images.Image                   `yaml:"passive,omitempty" json:"passive,omitempty"`
	GrubDefEntry           string                         `yaml:"grub-entry-name,omitempty" json:"grub-entry-name,omitempty"`
	ExtraPartitions        partitions.PartitionList       `yaml:"extra-partitions,omitempty" json:"extra-partitions,omitempty"`
	ExtraDirsRootfs        []string                       `yaml:"extra-dirs-rootfs,omitempty" json:"extra-dirs-rootfs,omitempty"`
	Force                  bool                           `yaml:"force,omitempty" json:"force,omitempty"`
	NoUsers                bool                           `yaml:"nousers,omitempty" json:"nousers,omitempty"`
	SSHHardening           bool                           `yaml:"ssh_hardening,omitempty" json:"ssh_hardening,omitempty"`
}

// SelinuxOptions controls SELinux on the installed system (RHEL and SUSE
// families, incl. openSUSE Tumbleweed). When Enabled, the system boots with
// selinux=1 and the kairos-selinux-relabel unit runs on every non-recovery
// boot.
type SelinuxOptions struct {
	Enabled bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Mode    string `yaml:"mode,omitempty" json:"mode,omitempty"`
}
