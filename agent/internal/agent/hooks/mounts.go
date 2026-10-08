package hook

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
	"github.com/mudler/yip/pkg/schema"
	"gopkg.in/yaml.v3"
)

type CustomMounts struct{}

func saveCloudConfig(fs sdkFs.KairosFS, name config.Stage, yc schema.YipConfig) error {
	yipYAML, err := yaml.Marshal(yc)
	if err != nil {
		return err
	}
	path := filepath.Join(constants.OEMPath, fmt.Sprintf("10_%s.yaml", name))
	return writeCloudConfig(fs, path, string(yipYAML))
}

// Run Read the keys sections ephemeral_mounts and bind mounts from install key in the cloud config.
// If not empty write an environment file to /run/cos/custom-layout.env.
// That env file is in turn read by /overlay/files/system/oem/11_persistency.yaml in fs.after stage.
func (cm CustomMounts) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if len(c.Install.BindMounts) == 0 && len(c.Install.EphemeralMounts) == 0 {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running CustomMounts hook")

	umount, err := mountOEM()
	if err != nil {
		return err
	}
	defer umount()

	var mountsList = map[string]string{}

	mountsList["CUSTOM_BIND_MOUNTS"] = strings.Join(c.Install.BindMounts, " ")
	mountsList["CUSTOM_EPHEMERAL_MOUNTS"] = strings.Join(c.Install.EphemeralMounts, " ")

	cfg := schema.YipConfig{
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

	err = saveCloudConfig(c.Fs, "user_custom_mounts", cfg)
	if err != nil {
		return err
	}
	c.Logger.Logger.Debug().Msg("Finish CustomMounts hook")
	return nil
}
