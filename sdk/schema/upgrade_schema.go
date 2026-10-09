package schema

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// UpgradeSchema represents the upgrade block in the Kairos configuration. It
// drives what `kairos-agent upgrade` does on the node.
//
// The keys mirror what the runtime decodes into UpgradeSpec
// (agent/pkg/implementations/spec), which is where the block is read:
// unmarshallFullSpec(cfg, "upgrade", spec). A key that is not here is a key no
// validation can report on, so it has to stay in step with that struct.
type UpgradeSchema struct {
	_                       struct{}   `title:"Kairos Schema: Upgrade block" description:"The upgrade block configures what happens when upgrade is called."`
	Entry                   string     `json:"entry,omitempty" description:"Boot entry to upgrade. Empty upgrades the active system, recovery upgrades the recovery system, and on UKI any systemd-boot entry name (the name of its .efi file) is accepted" examples:"[\"recovery\"]"`
	Recovery                ConfigBool `json:"recovery,omitempty" description:"Upgrade the recovery system instead of the active one. Equivalent to entry: recovery"`
	Active                  Image      `json:"system,omitempty" description:"The image the active system is upgraded to"`
	RecoverySystem          Image      `json:"recovery-system,omitempty" description:"The image the recovery system is upgraded to"`
	GrubDefEntry            string     `json:"grub-entry-name,omitempty" description:"Override the GRUB menu entry name"`
	Reboot                  SpecBool   `json:"reboot,omitempty" description:"Reboot after the upgrade"`
	PowerOff                SpecBool   `json:"poweroff,omitempty" description:"Power off after the upgrade"`
	ExtraDirsRootfs         []string   `json:"extra-dirs-rootfs,omitempty" description:"Directories to create in the upgraded rootfs, so a read-only system can still offer them as mount points"`
	AllowInsecureRegistries SpecBool   `json:"allow-insecure-registries,omitempty" description:"Pull the upgrade image from a registry served over plain HTTP or presenting an untrusted certificate"`
	ExcludedPaths           []string   `json:"excluded-paths,omitempty" description:"Paths of the source image to leave out of the deployed system"`
}

var _ jsonschemago.Preparer = UpgradeSchema{}

// PrepareJSONSchema closes the block to keys it does not declare.
//
// kairos-io/kairos#4925 is about a typo being silently ignored, and a wrong
// value is only half of that. upgrade.recovery-system is the expensive one:
// misspell it and the node upgrades its active system, reports success, and
// leaves recovery on the old image. unmarshallFullSpec drops the key without
// a word, so the schema is the only place that can name it.
//
// Closing the block is safe because the list above is the whole of what the
// block's readers honour. UpgradeSpec's two remaining fields, Passive and
// Partitions, carry no struct tag and are filled by the agent. UpgradeUkiSpec,
// which reads the same block on Trusted Boot, declares one key more,
// efi-partition, and NewUkiUpgradeSpec overwrites it with GetEfiPartition
// before reading it, so setting it has never done anything.
func (UpgradeSchema) PrepareJSONSchema(schema *jsonschemago.Schema) error {
	schema.WithAdditionalProperties(*(&jsonschemago.SchemaOrBool{}).WithTypeBoolean(false))

	return nil
}
