package config

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

type InstallSchema struct {
	_                   struct{}       `title:"Kairos Schema: Install block" description:"The install block is to drive automatic installations without user interaction."`
	Auto                bool           `json:"auto,omitempty" description:"Set to true when installing without Pairing"`
	Bundles             []BundleSchema `json:"bundles,omitempty" description:"Add bundles in runtime"`
	Device              string         `json:"device,omitempty" pattern:"^(auto|/|(/[a-zA-Z0-9_-]+)+)$" description:"Device for automated installs" examples:"[\"auto\",\"/dev/sda\"]"`
	EncryptedPartitions []string       `json:"encrypted_partitions,omitempty"`
	Env                 []interface{}  `json:"env,omitempty"`
	GrubOptions         `json:"grub_options,omitempty"`
	Image               string `json:"image,omitempty" description:"Use a different container image for the installation"`
	PowerManagement
}

type BundleSchema struct {
	DB         string   `json:"db_path,omitempty"`
	Repository string   `json:"repository,omitempty"`
	Rootfs     string   `json:"rootfs_path,omitempty"`
	Targets    []string `json:"targets,omitempty"`
}

type GrubOptions struct {
	DefaultFallback      string `json:"default_fallback,omitempty" description:"Sets default fallback logic"`
	DefaultMenuEntry     string `json:"default_menu_entry,omitempty" description:"Change GRUB menu entry"`
	ExtraActiveCmdline   string `json:"extra_active_cmdline,omitempty" description:"Additional Kernel option cmdline to apply just for active"`
	ExtraCmdline         string `json:"extra_cmdline,omitempty" description:"Additional Kernel option cmdline to apply"`
	ExtraPassiveCmdline  string `json:"extra_passive_cmdline,omitempty" description:"Additional Kernel option cmdline to apply just for passive"`
	ExtraRecoveryCmdline string `json:"extra_recovery_cmdline,omitempty" description:"Set additional boot commands when booting into recovery"`
	NextEntry            string `json:"next_entry,omitempty" description:"Set the next reboot entry."`
	SavedEntry           string `json:"saved_entry,omitempty" description:"Set the default boot entry."`
}

type PowerManagement struct {
}

type NoPowerManagement struct {
	Reboot   bool `json:"reboot,omitempty" const:"false" default:"false" description:"Reboot after installation"`
	Poweroff bool `json:"poweroff,omitempty" const:"false" default:"false" description:"Power off after installation"`
}

type RebootOnly struct {
	Reboot   bool `json:"reboot,omitempty" const:"true" default:"false" required:"true" description:"Reboot after installation"`
	Poweroff bool `json:"poweroff,omitempty" const:"false" default:"false" description:"Power off after installation"`
}

type PowerOffOnly struct {
	Reboot   bool `json:"reboot,omitempty" const:"false" default:"false" description:"Reboot after installation"`
	Poweroff bool `json:"poweroff,omitempty" const:"true" default:"false" required:"true" description:"Power off after installation"`
}

var _ jsonschemago.AnyOfExposer = PowerManagement{}

// The AnyOfModel interface is only needed for the tests that check the new schemas contain all needed fields
// it can be removed once the new schema is the single source of truth
type AnyOfModel interface {
	JSONSchemaAnyOf() []interface{}
}

func (PowerManagement) JSONSchemaAnyOf() []interface{} {
	return []interface{}{
		NoPowerManagement{}, RebootOnly{}, PowerOffOnly{},
	}
}
