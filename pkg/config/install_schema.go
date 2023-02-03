package config

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

type InstallSchema struct {
	_       struct{}      `title:"Kairos Schema: Install block" description:"The install block is to drive automatic installations without user interaction."`
	Device  string        `json:"device,omitempty" pattern:"^(auto|/|(/[a-zA-Z0-9_-]+)+)$" description:"Device for automated installs" examples:"[\"auto\",\"/dev/sda\"]"`
	Auto    bool          `json:"auto,omitempty" description:"Set to true when installing without Pairing"`
	Image   string        `json:"image,omitempty" description:"Use a different container image for the installation"`
	Bundles []interface{} `json:"bundles,omitempty" description:"Add bundles in runtime"`
	PowerManagement
	GrubOptions `json:"grub_options,omitempty"`
	Envs        []interface{} `json:"env,omitempty"`
}

type GrubOptions struct {
	ExtraCmdline        string `json:"extra_cmdline,omitempty" description:"Additional Kernel option cmdline to apply"`
	ExtraActiveCmdline  string `json:"extra_active_cmdline,omitempty" description:"Additional Kernel option cmdline to apply just for active"`
	ExtraPassiveCmdline string `json:"extra_passive_cmdline,omitempty" description:"Additional Kernel option cmdline to apply just for passive"`
	DefaultMenuEntry    string `json:"default_menu_entry,omitempty" description:"Change GRUB menu entry"`
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

func (PowerManagement) JSONSchemaAnyOf() []interface{} {
	return []interface{}{
		NoPowerManagement{}, RebootOnly{}, PowerOffOnly{},
	}
}
