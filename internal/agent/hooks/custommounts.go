package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/mudler/yip/pkg/schema"
	yip "github.com/mudler/yip/pkg/schema"
	"gopkg.in/yaml.v1"
)

type CustomMounts struct{}

func saveCloudConfig(name config.Stage, yc yip.YipConfig) error {
	yipYAML, err := yaml.Marshal(yc)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("oem", fmt.Sprintf("100_%s.yaml", name)), yipYAML, 0400)
}

// read the sections custom_mounts and custom_ephemeral_mounts from the user cloud
// info supplied to the agent.
// if not empty write environCUSTOM_PERSISTENT_STATE_PATHSment file to /run/cos/custom-layout.env
// that env file is in turn read by 11_persistency.yaml in fs.after stage
func (cm CustomMounts) Run(c config.Config) error {

	if len(c.Install.CustomBindMounts) == 0 && len(c.Install.CustomEphemeralMounts) == 0 {
		return nil
	}

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem") //nolint:errcheck
	}()

	var mountsList = map[string]string{}

	mountsList["CUSTOM_PERSISTENT_PATHS"] = strings.Join(c.Install.CustomBindMounts, " ")
	mountsList["CUSTOM_EPHEMERAL_PATHS"] = strings.Join(c.Install.CustomEphemeralMounts, " ")

	config := yip.YipConfig{Stages: map[string][]schema.Stage{
		"rootfs.after": []yip.Stage{{
			Name:            "user_custom_mounts",
			EnvironmentFile: "/run/cos/custom-layout.env",
			Environment:     mountsList,
		}},
	}}

	saveCloudConfig("user_custom_mounts", config)
	return nil
}
