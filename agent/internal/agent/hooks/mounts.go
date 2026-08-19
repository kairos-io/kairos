package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/machine"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
	"github.com/mudler/yip/pkg/schema"
	yip "github.com/mudler/yip/pkg/schema"
	"gopkg.in/yaml.v3"
)

type CustomMounts struct{}

func saveCloudConfig(name config.Stage, yc yip.YipConfig) error {
	yipYAML, err := yaml.Marshal(yc)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("/oem", fmt.Sprintf("10_%s.yaml", name)), yipYAML, 0400)
}

// Run Read the keys sections ephemeral_mounts and bind mounts from install key in the cloud config.
// If not empty write an environment file to /run/cos/custom-layout.env.
// That env file is in turn read by /overlay/files/system/oem/11_persistency.yaml in fs.after stage.
func (cm CustomMounts) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if len(c.Install.BindMounts) == 0 && len(c.Install.EphemeralMounts) == 0 {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running CustomMounts hook")

	err := machine.Mount("COS_OEM", "/oem")
	if err != nil {
		return err
	}
	defer func() {
		_ = machine.Umount("/oem")
	}()

	var mountsList = map[string]string{}

	mountsList["CUSTOM_BIND_MOUNTS"] = strings.Join(c.Install.BindMounts, " ")
	mountsList["CUSTOM_EPHEMERAL_MOUNTS"] = strings.Join(c.Install.EphemeralMounts, " ")

	cfg := yip.YipConfig{
		Stages: map[string][]schema.Stage{
			"rootfs": {
				{
					Name:            "user_custom_mounts",
					EnvironmentFile: "/run/cos/custom-layout.env",
					Environment:     mountsList,
				},
			},
		},
	}

	err = saveCloudConfig("user_custom_mounts", cfg)
	if err != nil {
		return err
	}
	c.Logger.Logger.Debug().Msg("Finish CustomMounts hook")
	return nil
}
