package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/internal/bus"
	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/uki"
	internalutils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	k8sutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/k8s"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	"github.com/kairos-io/kairos/v4/sdk/versioneer"
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
	source string, strictValidations bool, dirs []string, upgradeEntry string, allowInsecureRegistries bool, dryRun bool, excludes ...string) error {
	bus.Manager.Initialize()

	fixedDirs := hostConfigDirs(dirs)

	if internalutils.UkiBootMode() == internalutils.UkiHDD {
		return upgradeUki(source, fixedDirs, upgradeEntry, strictValidations, allowInsecureRegistries, dryRun)
	} else {
		return upgrade(source, fixedDirs, upgradeEntry, strictValidations, allowInsecureRegistries, dryRun, excludes...)
	}
}

// hostConfigDirs maps the config directories the agent scans onto the running
// system's filesystem. Under Kubernetes the upgrade runs in a pod with the host
// root bind-mounted somewhere else (/host by default, HOST_DIR when it is set),
// and without the prefix the upgrade would read the container's own configs
// instead of the node's. GetHostDirForK8s returns "" outside Kubernetes, which
// filepath.Join leaves the paths alone.
//
// The result holds exactly one entry per input. It is built with a zero length
// and a reserved capacity on purpose: `make([]string, len(dirs))` followed by
// append would prefix the real paths with len(dirs) empty strings, and the
// collector drops an unreadable scan directory without a word, so those would
// be scanned silently on every upgrade (kairos-io/kairos#5389).
func hostConfigDirs(dirs []string) []string {
	hostdir := k8sutils.GetHostDirForK8s()
	fixedDirs := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		fixedDirs = append(fixedDirs, filepath.Join(hostdir, dir))
	}
	return fixedDirs
}

func upgrade(sourceImageURL string, dirs []string, upgradeEntry string, strictValidations bool, allowInsecureRegistries bool, dryRun bool, excludes ...string) error {
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

	if dryRun {
		writeUpgradeSummary(os.Stdout, upgradeSpec)
		return nil
	}

	upgradeAction := action.NewUpgradeAction(c, upgradeSpec)

	err = upgradeAction.Run()
	if err != nil {
		return err
	}

	return hook.Run(*c, upgradeSpec, hook.FinishUpgrade...)
}

func upgradeUki(sourceImageURL string, dirs []string, upgradeEntry string, strictValidations bool, allowInsecureRegistries bool, dryRun bool) error {
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

	if dryRun {
		writeUkiUpgradeSummary(os.Stdout, upgradeSpec)
		return nil
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

// newerReleases lists the releases a node can upgrade to. It uses
// NewerAllVersions rather than NewerAnyVersion so that a newer Kairos version
// built against an older Kubernetes version is left out: Kubernetes does not
// support downgrades, so those tags are not upgrade candidates
// (kairos-io/kairos#3382). `list-releases --all` still shows every other tag.
func newerReleases(registry string) (versioneer.TagList, error) {
	artifact, err := versioneer.NewArtifactFromOSRelease()
	if err != nil {
		return versioneer.TagList{}, err
	}

	tagList, err := artifact.TagList(registry)
	if err != nil {
		return tagList, err
	}
	return tagList.NewerAllVersions().RSorted(), nil
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
