package machine

import (
	"fmt"
	"os"

	"github.com/c3os-io/c3os/internal/utils"
	process "github.com/mudler/go-processmanager"
)

type QEMU struct {
}

type MachineConfig struct {
	StateDir        string
	SSHPort         string
	ISO, DataSource string
	Drive           string
}

func (q *QEMU) Run(m *MachineConfig) error {
	genDrives := func(m *MachineConfig) []string {
		drives := []string{}
		if m.ISO != "" {
			drives = append(drives, "-drive", fmt.Sprintf("if=ide,media=cdrom,file=%s", m.ISO))
		}
		if m.DataSource != "" {
			drives = append(drives, "-drive", fmt.Sprintf("if=ide,media=cdrom,file=%s", m.DataSource))
		}
		if m.Drive != "" {
			drives = append(drives, "-drive", fmt.Sprintf("if=virtio,media=disk,file=%s", m.Drive))
		}
		return drives
	}

	qemu := process.New(
		process.WithName("/usr/bin/qemu-system-x86_64"),
		process.WithArgs(
			"-m", "2096",
			"-smp", "cores=2",
			"-rtc", "base=utc,clock=rt",
			"-nographic",
			"-device", "virtio-serial", "-nic", fmt.Sprintf("user,hostfwd=tcp::%s-:22", m.SSHPort),
		),
		process.WithArgs(genDrives(m)...),
		process.WithStateDir(m.StateDir),
	)
	return qemu.Run()
}

func (q *QEMU) Stop(m *MachineConfig) error {
	return process.New(process.WithStateDir(m.StateDir)).Stop()
}

func (q *QEMU) Clean(m *MachineConfig) error {
	return os.RemoveAll(m.StateDir)
}

func (q *QEMU) Alive(m *MachineConfig) bool {
	return process.New(process.WithStateDir(m.StateDir)).IsAlive()
}

func CreateDisk(imageName, size string) error {
	_, err := utils.SH(fmt.Sprintf("qemu-img create -f qcow2 %s %s", imageName, size))
	return err
}
