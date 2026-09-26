package schema

// UpgradeSchema represents the upgrade block in the Kairos configuration. It
// drives what `kairos-agent upgrade` does on the node.
//
// The keys mirror what the runtime decodes into UpgradeSpec
// (agent/pkg/implementations/spec), which is where the block is read:
// unmarshallFullSpec(cfg, "upgrade", spec). A key that is not here is a key no
// validation can report on, so it has to stay in step with that struct.
type UpgradeSchema struct {
	_                       struct{} `title:"Kairos Schema: Upgrade block" description:"The upgrade block configures what happens when upgrade is called."`
	Entry                   string   `json:"entry,omitempty" enum:"[\"cos\",\"recovery\"]" description:"Boot entry to upgrade. Defaults to the active system; set to recovery to upgrade the recovery system instead"`
	Recovery                bool     `json:"recovery,omitempty" description:"Upgrade the recovery system instead of the active one. Equivalent to entry: recovery"`
	Active                  Image    `json:"system,omitempty" description:"The image the active system is upgraded to"`
	RecoverySystem          Image    `json:"recovery-system,omitempty" description:"The image the recovery system is upgraded to"`
	GrubDefEntry            string   `json:"grub-entry-name,omitempty" description:"Override the GRUB menu entry name"`
	Reboot                  bool     `json:"reboot,omitempty" description:"Reboot after the upgrade"`
	PowerOff                bool     `json:"poweroff,omitempty" description:"Power off after the upgrade"`
	ExtraDirsRootfs         []string `json:"extra-dirs-rootfs,omitempty" description:"Directories to create in the upgraded rootfs, so a read-only system can still offer them as mount points"`
	AllowInsecureRegistries bool     `json:"allow-insecure-registries,omitempty" description:"Pull the upgrade image from a registry served over plain HTTP or presenting an untrusted certificate"`
	ExcludedPaths           []string `json:"excluded-paths,omitempty" description:"Paths of the source image to leave out of the deployed system"`
}
