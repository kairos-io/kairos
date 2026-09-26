package schema

// ResetSchema represents the reset block in the Kairos configuration. It
// drives what `kairos-agent reset` does on the node.
//
// The keys mirror what the runtime decodes into ResetSpec
// (agent/pkg/implementations/spec), which is where the block is read:
// unmarshallFullSpec(cfg, "reset", spec).
type ResetSchema struct {
	_                struct{} `title:"Kairos Schema: Reset block" description:"The reset block configures what happens when reset is called."`
	FormatPersistent bool     `json:"reset-persistent,omitempty" description:"Format the oem and persistent partitions, discarding everything on them"`
	FormatOEM        bool     `json:"reset-oem,omitempty" description:"Format the oem partition"`
	Reboot           bool     `json:"reboot,omitempty" description:"Reboot after the reset"`
	PowerOff         bool     `json:"poweroff,omitempty" description:"Power off after the reset"`
	GrubDefEntry     string   `json:"grub-entry-name,omitempty" description:"Override the GRUB menu entry name"`
	Tty              string   `json:"tty,omitempty" description:"Console the reset writes its output to" examples:"[\"ttyS0\",\"tty1\"]"`
	ExtraDirsRootfs  []string `json:"extra-dirs-rootfs,omitempty" description:"Directories to create in the reset rootfs, so a read-only system can still offer them as mount points"`
	Active           Image    `json:"system,omitempty" description:"The image the system is reset to"`
}
