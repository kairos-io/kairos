package schema

import "github.com/deniswernert/go-fstab"

type Layout struct {
	Overlay  Overlay
	OEMLabel string
	//https://github.com/kairos-io/packages/blob/94aa3bef3d1330cb6c6905ae164f5004b6a58b8c/packages/system/dracut/immutable-rootfs/30cos-immutable-rootfs/cos-generator.sh#L71
	Mounts []string
}

type Overlay struct {
	// /run/overlay
	Base string
	// https://github.com/kairos-io/packages/blob/94aa3bef3d1330cb6c6905ae164f5004b6a58b8c/packages/system/dracut/immutable-rootfs/30cos-immutable-rootfs/cos-generator.sh#L22
	BackingBase string
}

type LsblkOutput struct {
	Blockdevices []struct {
		Name     string      `json:"name,omitempty"`
		Parttype interface{} `json:"parttype,omitempty"`
		Children []struct {
			Name     string `json:"name,omitempty"`
			Parttype string `json:"parttype,omitempty"`
		} `json:"children,omitempty"`
	} `json:"blockdevices,omitempty"`
}

type FsTabs []*fstab.Mount
