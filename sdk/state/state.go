package state

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/jaypipes/ghw"
	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/kairos/pkg/machine"
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
	Partition block.Partition `yaml:"partition" json:"partition"`
	Mounted   bool            `yaml:"mounted" json:"mounted"`
}

type Runtime struct {
	UUID       string         `yaml:"uuid" json:"uuid"`
	Persistent PartitionState `yaml:"persistent" json:"persistent"`
	OEM        PartitionState `yaml:"oem" json:"oem"`
	State      PartitionState `yaml:"state" json:"state"`
	BootState  Boot           `yaml:"boot" json:"boot"`
}

func detectPartition(b *block.Partition) PartitionState {
	return PartitionState{
		Partition: *b,
		Mounted:   b.MountPoint != "",
	}
}

func detectBoot() Boot {
	cmdline, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return Unknown
	}
	cmdlineS := string(cmdline)
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
	if err != nil {
		return err
	}
	for _, d := range blockDevices.Disks {
		for _, part := range d.Partitions {
			switch part.Name {
			case "COS_PERSISTENT":
				r.Persistent = detectPartition(part)
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
