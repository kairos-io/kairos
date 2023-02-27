package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/kairos-io/kairos/pkg/config"
	schema "github.com/kairos-io/kairos/pkg/config/schemas"
	"github.com/kairos-io/kairos/pkg/machine"
	yip "github.com/mudler/yip/pkg/schema"
	"gopkg.in/yaml.v1"
)

type CustomMounts struct{}

func saveCloudConfig(name config.Stage, yc yip.YipConfig) error {
	yipYAML, err := yaml.Marshal(yc)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("/oem", fmt.Sprintf("10_%s.yaml", name)), yipYAML, 0400)
}

// Read the keys sections ephemeral_mounts and bind mounts from install key in the cloud config.
// If not empty write an environment file to /run/cos/custom-layout.env.
// That env file is in turn read by /overlay/files/system/oem/11_persistency.yaml in fs.after stage.
func (cm CustomMounts) Run(c config.Config) error {

	//fmt.Println("Custom mounts hook")
	//fmt.Println(strings.Join(c.Install.BindMounts, " "))
	//fmt.Println(strings.Join(c.Install.EphemeralMounts, " "))

	if len(c.Install.BindMounts) == 0 && len(c.Install.EphemeralMounts) == 0 {
		return nil
	}

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem") //nolint:errcheck
	}()

	var mountsList = map[string]string{}

	mountsList["CUSTOM_BIND_MOUNTS"] = strings.Join(c.Install.BindMounts, " ")
	mountsList["CUSTOM_EPHEMERAL_MOUNTS"] = strings.Join(c.Install.EphemeralMounts, " ")

	config := yip.YipConfig{Stages: map[string][]yip.Stage{
		"rootfs": {{
			Name:            "user_custom_mounts",
			EnvironmentFile: "/run/cos/custom-layout.env",
			Environment:     mountsList,
		}},
	}}

	saveCloudConfig("user_custom_mounts", config) //nolint:errcheck
	return nil
}

func (cm CustomMounts) KRun(kc schema.KConfig) error {

	//fmt.Println("Custom mounts hook")
	//fmt.Println(strings.Join(c.Install.BindMounts, " "))
	//fmt.Println(strings.Join(c.Install.EphemeralMounts, " "))

	if len(kc.Install.BindMounts) == 0 && len(kc.Install.EphemeralMounts) == 0 {
		return nil
	}

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem") //nolint:errcheck
	}()

	var mountsList = map[string]string{}

	mountsList["CUSTOM_BIND_MOUNTS"] = strings.Join(kc.Install.BindMounts, " ")
	mountsList["CUSTOM_EPHEMERAL_MOUNTS"] = strings.Join(kc.Install.EphemeralMounts, " ")

	config := yip.YipConfig{Stages: map[string][]yip.Stage{
		"rootfs": {{
			Name:            "user_custom_mounts",
			EnvironmentFile: "/run/cos/custom-layout.env",
			Environment:     mountsList,
		}},
	}}

	saveCloudConfig("user_custom_mounts", config) //nolint:errcheck
	return nil
}
