package install

import (
	"github.com/kairos-io/kairos-sdk/types/bundles"
	"github.com/kairos-io/kairos-sdk/types/images"
	"github.com/kairos-io/kairos-sdk/types/partitions"
)

// You would probably be thinking, why is the Install struct in here? Well, the types
// package is already imported everywhere, so putting it here avoids cyclic imports
// and makes it easier to use across the codebase.
// Plus things like providers can import them in order to modify and send back the install info
// so its nice that its in a central place for providers to consume and be able to alter install behavior easily.

type Install struct {
	Auto                   bool                           `yaml:"auto,omitempty" mapstructure:"auto" json:"auto,omitempty"`
	Reboot                 bool                           `yaml:"reboot,omitempty" mapstructure:"reboot" json:"reboot,omitempty"`
	NoFormat               bool                           `yaml:"no-format,omitempty" mapstructure:"no-format" json:"no-format,omitempty"`
	Device                 string                         `yaml:"device,omitempty" mapstructure:"device" json:"device,omitempty" pattern:"^(auto|/dev/.+|script://.+)$" description:"Device for automated installs" examples:"[\"auto\",\"/dev/sda\",\"script:///usr/local/bin/pick-disk.sh\"]"`
	Poweroff               bool                           `yaml:"poweroff,omitempty" mapstructure:"poweroff" json:"poweroff,omitempty"`
	GrubOptions            map[string]string              `yaml:"grub_options,omitempty" mapstructure:"grub_options" json:"grub_options,omitempty"`
	Bundles                bundles.Bundles                `yaml:"bundles,omitempty" mapstructure:"bundles" json:"bundles,omitempty"`
	Encrypt                []string                       `yaml:"encrypted_partitions,omitempty" mapstructure:"encrypted_partitions" json:"encrypted_partitions,omitempty"`
	SkipEncryptCopyPlugins bool                           `yaml:"skip_copy_kcrypt_plugin,omitempty" mapstructure:"skip_copy_kcrypt_plugin" json:"skip_copy_kcrypt_plugin,omitempty"`
	Env                    []string                       `yaml:"env,omitempty" mapstructure:"env" json:"env,omitempty"`
	Source                 string                         `yaml:"source,omitempty" mapstructure:"source" json:"source,omitempty"`
	EphemeralMounts        []string                       `yaml:"ephemeral_mounts,omitempty" mapstructure:"ephemeral_mounts" json:"ephemeral_mounts,omitempty"`
	BindMounts             []string                       `yaml:"bind_mounts,omitempty" mapstructure:"bind_mounts" json:"bind_mounts,omitempty"`
	Partitions             partitions.ElementalPartitions `yaml:"partitions,omitempty" mapstructure:"partitions" json:"partitions,omitempty"`
	Active                 images.Image                   `yaml:"system,omitempty" mapstructure:"system" json:"system,omitempty"`
	Recovery               images.Image                   `yaml:"recovery-system,omitempty" mapstructure:"recovery-system" json:"recovery-system,omitempty"`
	Passive                images.Image                   `yaml:"passive,omitempty" mapstructure:"recovery-system" json:"passive,omitempty"`
	GrubDefEntry           string                         `yaml:"grub-entry-name,omitempty" mapstructure:"grub-entry-name" json:"grub-entry-name,omitempty"`
	ExtraPartitions        partitions.PartitionList       `yaml:"extra-partitions,omitempty" mapstructure:"extra-partitions" json:"extra-partitions,omitempty"`
	ExtraDirsRootfs        []string                       `yaml:"extra-dirs-rootfs,omitempty" mapstructure:"extra-dirs-rootfs" json:"extra-dirs-rootfs,omitempty"`
	Force                  bool                           `yaml:"force,omitempty" mapstructure:"force" json:"force,omitempty"`
	NoUsers                bool                           `yaml:"nousers,omitempty" mapstructure:"nousers" json:"nousers,omitempty"`
	SSHHardening           bool                           `yaml:"ssh_hardening,omitempty" mapstructure:"ssh_hardening" json:"ssh_hardening,omitempty"`
}
