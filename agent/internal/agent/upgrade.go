package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/uki"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	k8sutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/k8s"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/kairos-io/kairos-sdk/versioneer"
	"github.com/mudler/go-pluggable"
)

func CurrentImage(registry string) (string, error) {
	artifact, err := versioneer.NewArtifactFromOSRelease()
	if err != nil {
		return "", fmt.Errorf("creating an Artifact from kairos-release: %w", err)
	}

	return artifact.ContainerName(registry)
}

func ListAllReleases(includePrereleases bool, registry string) ([]string, error) {
	var err error

	tagList, err := allReleases(registry)
	if err != nil {
		return []string{}, err
	}

	if !includePrereleases {
		tagList = tagList.NoPrereleases()
	}

	return tagList.FullImages()
}

func ListNewerReleases(includePrereleases bool, registry string) ([]string, error) {
	var err error

	tagList, err := newerReleases(registry)
	if err != nil {
		return []string{}, err
	}

	if !includePrereleases {
		tagList = tagList.NoPrereleases()
	}

	return tagList.FullImages()
}

func Upgrade(
	source string, strictValidations bool, dirs []string, upgradeEntry string, allowInsecureRegistries bool, excludes ...string) error {
	bus.Manager.Initialize()

	fixedDirs := make([]string, len(dirs))
	// Check and fix dirs if we are under k8s, so we read the actual running system configs instead of only
	// the container configs
	// we can run it blindly as it will return an empty string if not under k8s
	hostdir := k8sutils.GetHostDirForK8s()
	for _, dir := range dirs {
		fixedDirs = append(fixedDirs, filepath.Join(hostdir, dir))
	}

	if internalutils.UkiBootMode() == internalutils.UkiHDD {
		return upgradeUki(source, fixedDirs, upgradeEntry, strictValidations, allowInsecureRegistries)
	} else {
		return upgrade(source, fixedDirs, upgradeEntry, strictValidations, allowInsecureRegistries, excludes...)
	}
}

func upgrade(sourceImageURL string, dirs []string, upgradeEntry string, strictValidations bool, allowInsecureRegistries bool, excludes ...string) error {
	c, err := getConfig(sourceImageURL, dirs, upgradeEntry, strictValidations, allowInsecureRegistries, excludes...)
	if err != nil {
		return err
	}
	utils.SetEnv(c.Env)

	err = config.CheckConfigForUsers(c)
	if err != nil {
		return err
	}

	// Load the upgrade Config from the system
	upgradeSpec, err := config.ReadUpgradeSpecFromConfig(c)
	if err != nil {
		return err
	}
	err = upgradeSpec.Sanitize()
	if err != nil {
		return err
	}

	upgradeAction := action.NewUpgradeAction(c, upgradeSpec)

	err = upgradeAction.Run()
	if err != nil {
		return err
	}

	return hook.Run(*c, upgradeSpec, hook.FinishUpgrade...)
}

func upgradeUki(sourceImageURL string, dirs []string, upgradeEntry string, strictValidations bool, allowInsecureRegistries bool) error {
	c, err := getConfig(sourceImageURL, dirs, upgradeEntry, strictValidations, allowInsecureRegistries)
	if err != nil {
		return err
	}
	utils.SetEnv(c.Env)

	err = config.CheckConfigForUsers(c)
	if err != nil {
		return err
	}

	// Load the upgrade Config from the system
	upgradeSpec, err := config.ReadUkiUpgradeSpecFromConfig(c)
	if err != nil {
		return err
	}

	err = upgradeSpec.Sanitize()
	if err != nil {
		return err
	}

	upgradeAction := uki.NewUpgradeAction(c, upgradeSpec)

	err = upgradeAction.Run()
	if err != nil {
		return err
	}

	return hook.Run(*c, upgradeSpec, hook.FinishUpgrade...)
}

func getConfig(sourceImageURL string, dirs []string, upgradeEntry string, strictValidations bool, allowInsecureRegistries bool, excludes ...string) (*sdkConfig.Config, error) {
	cliConf, err := generateUpgradeConfForCLIArgs(sourceImageURL, upgradeEntry, allowInsecureRegistries, excludes...)
	if err != nil {
		return nil, err
	}

	c, err := config.Scan(collector.Directories(dirs...),
		collector.Readers(strings.NewReader(cliConf)),
		collector.StrictValidation(strictValidations))
	if err != nil {
		return nil, err
	}
	return c, err

}

func allReleases(registry string) (versioneer.TagList, error) {
	artifact, err := versioneer.NewArtifactFromOSRelease()
	if err != nil {
		return versioneer.TagList{}, err
	}

	tagList, err := artifact.TagList(registry)
	if err != nil {
		return tagList, err
	}

	return tagList.OtherAnyVersion().RSorted(), nil
}

func newerReleases(registry string) (versioneer.TagList, error) {
	artifact, err := versioneer.NewArtifactFromOSRelease()
	if err != nil {
		return versioneer.TagList{}, err
	}

	tagList, err := artifact.TagList(registry)
	if err != nil {
		return tagList, err
	}
	return tagList.NewerAnyVersion().RSorted(), nil
}

// generateUpgradeConfForCLIArgs creates a kairos configuration for `--source` and `--recovery` and `--excluded-paths`
// command line arguments. It will be added to the rest of the configurations.
func generateUpgradeConfForCLIArgs(source, upgradeEntry string, allowInsecureRegistries bool, excludes ...string) (string, error) {
	upgradeConfig := ExtraConfigUpgrade{}

	upgradeConfig.Upgrade.Entry = upgradeEntry
	upgradeConfig.Upgrade.AllowInsecureRegistries = allowInsecureRegistries

	// Set uri both for active and recovery because we don't know what we are
	// actually upgrading. The "upgradeRecovery" is just the command line argument.
	// The user might have set it to "true" in the kairos config. Since we don't
	// have access to that yet, we just set both uri values which shouldn't matter
	// anyway, the right one will be used later in the process.
	if source != "" {
		upgradeConfig.Upgrade.RecoverySystem.Source = source
		upgradeConfig.Upgrade.System.Source = source
	}
	if len(excludes) > 0 {
		upgradeConfig.Upgrade.ExcludedPaths = excludes
	}

	d, err := json.Marshal(upgradeConfig)
	return string(d), err
}

func getReleasesFromProvider(includePrereleases bool) ([]string, error) {
	var result []string
	bus.Manager.Response(events.EventAvailableReleases, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		if r.Data == "" {
			return
		}
		if err := json.Unmarshal([]byte(r.Data), &result); err != nil {
			fmt.Printf("warn: failed unmarshalling data: '%s'\n", err.Error())
		}
	})

	configYAML := "IncludePreReleases: true"
	_, err := bus.Manager.Publish(events.EventAvailableReleases, events.EventPayload{Config: configYAML})
	if err != nil {
		return result, fmt.Errorf("failed publishing event: %w", err)
	}

	return result, nil
}

// ExtraConfigUpgrade is the struct that holds the upgrade options that come from flags and events
type ExtraConfigUpgrade struct {
	Upgrade struct {
		Entry          string `json:"entry,omitempty"`
		RecoverySystem struct {
			Source string `json:"source,omitempty"`
		} `json:"recovery-system,omitempty"`
		System struct {
			Source string `json:"source,omitempty"`
		} `json:"system,omitempty"`
		ExcludedPaths           []string `json:"excluded-paths,omitempty"`
		AllowInsecureRegistries bool     `json:"allow-insecure-registries,omitempty"`
	} `json:"upgrade,omitempty"`
}
