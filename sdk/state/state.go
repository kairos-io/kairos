package state

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/jaypipes/ghw"
	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/pkg/utils"
	"gopkg.in/yaml.v3"
)

const (
	Active   Boot = "active_boot"
	Passive  Boot = "passive_boot"
	Recovery Boot = "recovery_boot"
	LiveCD   Boot = "livecd_boot"
	Unknown  Boot = "unknown"
)

type Boot string

type PartitionState struct {
	Mounted    bool   `yaml:"mounted" json:"mounted"`
	Name       string `yaml:"name" json:"name"`
	Label      string `yaml:"label" json:"label"`
	MountPoint string `yaml:"mount_point" json:"mount_point"`
	SizeBytes  uint64 `yaml:"size_bytes" json:"size_bytes"`
	Type       string `yaml:"type" json:"type"`
	IsReadOnly bool   `yaml:"read_only" json:"read_only"`
	Found      bool   `yaml:"found" json:"found"`
	UUID       string `yaml:"uuid" json:"uuid"` // This would be volume UUID on macOS, PartUUID on linux, empty on Windows
}

type Runtime struct {
	UUID       string         `yaml:"uuid" json:"uuid"`
	Persistent PartitionState `yaml:"persistent" json:"persistent"`
	Recovery   PartitionState `yaml:"recovery" json:"recovery"`
	OEM        PartitionState `yaml:"oem" json:"oem"`
	State      PartitionState `yaml:"state" json:"state"`
	BootState  Boot           `yaml:"boot" json:"boot"`
}

type FndMnt struct {
	Filesystems []struct {
		Target    string `json:"target,omitempty"`
		FsOptions string `json:"fs-options,omitempty"`
	} `json:"filesystems,omitempty"`
}

func detectPartition(b *block.Partition) PartitionState {
	// If mountpoint seems empty, try to get the mountpoint of the partition label also the RO status
	// This is a current shortcoming of ghw which only identifies mountpoints via device, not by label/uuid/anything else
	mountpoint := b.MountPoint
	readOnly := b.IsReadOnly
	if b.MountPoint == "" && b.Label != "" {
		out, err := utils.SH(fmt.Sprintf("findmnt /dev/disk/by-label/%s -f -J -o TARGET,FS-OPTIONS", b.Label))
		fmt.Println(out)
		mnt := &FndMnt{}
		if err == nil {
			err = json.Unmarshal([]byte(out), mnt)
			// This should not happen, if there were no targets, the command would have returned an error, but you never know...
			if err == nil && len(mnt.Filesystems) == 1 {
				mountpoint = mnt.Filesystems[0].Target
				// Don't assume its ro or rw by default, check both. One should match
				regexRW := regexp.MustCompile("^rw,|^rw$|,rw,|,rw$")
				regexRO := regexp.MustCompile("^ro,|^ro$|,ro,|,ro$")
				if regexRW.Match([]byte(mnt.Filesystems[0].FsOptions)) {
					readOnly = false
				}
				if regexRO.Match([]byte(mnt.Filesystems[0].FsOptions)) {
					readOnly = true
				}
			}
		}
	}
	return PartitionState{
		Type:       b.Type,
		IsReadOnly: readOnly,
		UUID:       b.UUID,
		Name:       fmt.Sprintf("/dev/%s", b.Name),
		SizeBytes:  b.SizeBytes,
		Label:      b.Label,
		MountPoint: mountpoint,
		Mounted:    mountpoint != "",
		Found:      true,
	}
}

func detectBoot() Boot {
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return Unknown
	}
	cmdlineS := string(cmdline)
	fmt.Println(cmdlineS)
	switch {
	case strings.Contains(cmdlineS, "COS_ACTIVE"):
		return Active
	case strings.Contains(cmdlineS, "COS_PASSIVE"):
		return Passive
	case strings.Contains(cmdlineS, "COS_RECOVERY"), strings.Contains(cmdlineS, "COS_SYSTEM"):
		return Recovery
	case strings.Contains(cmdlineS, "live:LABEL"), strings.Contains(cmdlineS, "live:CDLABEL"):
		return LiveCD
	default:
		return Unknown
	}
}

func detectRuntimeState(r *Runtime) error {
	blockDevices, err := block.New(ghw.WithDisableTools(), ghw.WithDisableWarnings())
	// ghw currently only detects if partitions are mounted via the device
	// If we mount them via label, then its set as not mounted.
	if err != nil {
		return err
	}
	for _, d := range blockDevices.Disks {
		for _, part := range d.Partitions {
			switch part.Label {
			case "COS_PERSISTENT":
				r.Persistent = detectPartition(part)
			case "COS_RECOVERY":
				r.Recovery = detectPartition(part)
			case "COS_OEM":
				r.OEM = detectPartition(part)
			case "COS_STATE":
				r.State = detectPartition(part)
			}
		}
	}
	return nil
}

func NewRuntime() (Runtime, error) {
	runtime := &Runtime{
		BootState: detectBoot(),
		UUID:      machine.UUID(),
	}
	err := detectRuntimeState(runtime)
	return *runtime, err
}

func (r Runtime) String() string {
	dat, err := yaml.Marshal(r)
	if err == nil {
		return string(dat)
	}
	return ""
}

func (r Runtime) Query(s string) (res string, err error) {
	s = fmt.Sprintf(".%s", s)
	jsondata := map[string]interface{}{}
	var dat []byte
	dat, err = json.Marshal(r)
	if err != nil {
		return
	}
	err = json.Unmarshal(dat, &jsondata)
	if err != nil {
		return
	}
	query, err := gojq.Parse(s)
	if err != nil {
		return res, err
	}
	iter := query.Run(jsondata) // or query.RunWithContext
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return res, err
		}
		res += fmt.Sprint(v)
	}
	return
}
