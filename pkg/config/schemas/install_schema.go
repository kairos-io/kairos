package config

import (
	"encoding/json"

	jsonschemago "github.com/swaggest/jsonschema-go"
)

// InstallSchema represents the install block in the Kairos configuration. It is used to drive automatic installations without user interaction.
type InstallSchema struct {
	_                   struct{}      `title:"Kairos Schema: Install block" description:"The install block is to drive automatic installations without user interaction."`
	Auto                bool          `json:"auto,omitempty" description:"Set to true when installing without Pairing"`
	BindMounts          []string      `json:"bind_mounts,omitempty"`
	Bundles             BBundles      `json:"bundles,omitempty" description:"Add bundles in runtime"`
	Device              string        `json:"device,omitempty" pattern:"^(auto|/|(/[a-zA-Z0-9_-]+)+)$" description:"Device for automated installs" examples:"[\"auto\",\"/dev/sda\"]"`
	EphemeralMounts     []string      `json:"ephemeral_mounts,omitempty"`
	EncryptedPartitions []string      `json:"encrypted_partitions,omitempty"`
	Env                 []interface{} `json:"env,omitempty"`
	GrubOptionsSchema   `json:"grub_options,omitempty"`
	Image               string `json:"image,omitempty" description:"Use a different container image for the installation"`
	PowerManagement
	SkipEncryptCopyPlugins bool `json:"skip_copy_kcrypt_plugin,omitempty"`
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

// PowerManagement is a meta structure to hold the different rules for managing power, which are not compatible between each other.
type PowerManagement struct {
}

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

// The OneOfModel interface is only needed for the tests that check the new schemas contain all needed fields
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

type PowerAny struct {
	Reboot   bool
	Poweroff bool
}

func (PowerManagement) Foo() PowerAny {
	return PowerAny{}
}

func (is InstallSchema) GrubOptions() map[string]string {
	var myMap map[string]string
	data, _ := json.Marshal(is.GrubOptionsSchema)
	json.Unmarshal(data, &myMap)
	return myMap
}
