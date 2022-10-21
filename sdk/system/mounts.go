package system

import (
	"github.com/kairos-io/kairos/pkg/machine"
)

type Mountpoint string

func (m Mountpoint) Umount() {
	machine.Umount(string(m))
}
