package config

import (
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/cloudinit"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/http"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/imageextractor"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/runner"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/syscall"
	"github.com/kairos-io/kairos-sdk/types/cloudinitrunner"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkFs "github.com/kairos-io/kairos-sdk/types/fs"
	sdkHttp "github.com/kairos-io/kairos-sdk/types/http"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	"github.com/kairos-io/kairos-sdk/types/install"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/platform"
	sdkRunner "github.com/kairos-io/kairos-sdk/types/runner"
	sdkSyscall "github.com/kairos-io/kairos-sdk/types/syscall"
	yip "github.com/mudler/yip/pkg/schema"
	"github.com/spf13/viper"
	"github.com/twpayne/go-vfs/v5"
	"gopkg.in/yaml.v3"
	"k8s.io/mount-utils"
	"runtime"
)

func NewConfig(opts ...GenericOptions) *sdkConfig.Config {
	log := logger.NewKairosLoggerWithExtraDirs("agent", "info", false, "/var/log/kairos/")
	// Get the viper config in case something in command line or env var has set it and set the level asap
	if viper.GetBool("debug") {
		log.SetLevel("debug")
	}

	hostPlatform, err := platform.NewPlatformFromArch(runtime.GOARCH)
	if err != nil {
		log.Errorf("error parsing default platform (%s): %s", runtime.GOARCH, err.Error())
		return nil
	}

	c := &sdkConfig.Config{
		Fs:                        vfs.OSFS,
		Logger:                    log,
		Syscall:                   &syscall.RealSyscall{},
		Client:                    http.NewClient(),
		Arch:                      hostPlatform.Arch,
		Platform:                  hostPlatform,
		SquashFsCompressionConfig: constants.GetDefaultSquashfsCompressionOptions(),
		ImageExtractor:            imageextractor.OCIImageExtractor{},
		SquashFsNoCompression:     true,
		Install:                   &install.Install{},
		UkiMaxEntries:             constants.UkiMaxEntries,
	}
	for _, o := range opts {
		o(c)
	}

	// delay runner creation after we have run over the options in case we use WithRunner
	if c.Runner == nil {
		c.Runner = &runner.RealRunner{Logger: &c.Logger}
	}

	// Now check if the runner has a logger inside, otherwise point our logger into it
	// This can happen if we set the WithRunner option as that doesn't set a logger
	l := c.Runner.GetLogger()
	if &l == nil {
		c.Runner.SetLogger(&c.Logger)
	}

	// Delay the yip runner creation, so we set the proper logger instead of blindly setting it to the logger we create
	// at the start of NewConfig, as WithLogger can be passed on init, and that would result in 2 different logger
	// instances, on the config.Logger and the other on config.CloudInitRunner
	if c.CloudInitRunner == nil {
		c.CloudInitRunner = cloudinit.NewYipCloudInitRunner(c.Logger, c.Runner, vfs.OSFS)
	}

	if c.Mounter == nil {
		c.Mounter = mount.New(constants.MountBinary)
	}

	err = sanitizeConfig(c)
	// This should never happen
	if err != nil {
		c.Logger.Warnf("Error sanitizing the config: %s", err)
	}

	return c
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

// CheckConfigForUsers will check the config for any users and validate that at least we have 1 admin.
// Since Kairos 3.3.x we don't ship a default user with the system, so before a system with no specific users
// was relying in our default cloud-configs which created a kairos user ALWAYS (with SUDO!)
// But now we don't ship it anymore. So a user upgrading from 3.2.x to 3.3.x that created no users, will end up with a blocked
// system.
// So we need to see if they are setting a user in their config and if not refuse to continue
func CheckConfigForUsers(c *sdkConfig.Config) (err error) {
	// If nousers is enabled we do not check for the validity of the users and such
	// At this point, the config should be fully parsed and the yip stages ready

	// Check if the sentinel is present
	_, sentinel := c.Fs.Stat("/etc/kairos/.nousers")
	if sentinel == nil {
		c.Logger.Logger.Debug().Msg("Sentinel file found, skipping user check")
		return nil
	}
	if !c.Install.NoUsers {
		anyAdmin := false
		cc, _ := c.Collector.String()
		yamlConfig, err := yip.Load(cc, vfs.OSFS, nil, nil)
		if err != nil {
			return err
		}
		for _, stage := range yamlConfig.Stages {
			for _, x := range stage {
				if len(x.Users) > 0 {
					for _, user := range x.Users {
						if contains(user.Groups, "admin") || user.PrimaryGroup == "admin" {
							anyAdmin = true
							break
						}
					}
				}
			}

		}
		if !anyAdmin {
			return fmt.Errorf("No users found in any stage that are part of the 'admin' group.\n" +
				"In Kairos 3.3.x we no longer ship a default hardcoded user with the system configs and require users to provide their own user." +
				"Please provide at least 1 user that is part of the 'admin' group(for sudo) with your cloud configs." +
				"If you still want to continue without creating any users in the system, set 'install.nousers: true' to be in the config in order to allow a system with no users.")
		}
	}
	return err
}

// CheckConfigForExtraPartitions will check that any extra partition defined has a name
// as its required to identify them later on while formatting and creating the partitions
func CheckConfigForExtraPartitions(c *sdkConfig.Config) error {
	// Check if extra partitions are defined and if so, if they have a name as its required
	if len(c.Install.ExtraPartitions) > 0 {
		for _, part := range c.Install.ExtraPartitions {
			if part.Name == "" {
				return fmt.Errorf("extra partition defined without a name, please define a name for the partition: %+v", part)
			}
		}
	}
	return nil
}

// sanitizeConfig checks the consistency of the struct, returns error
// if unsolvable inconsistencies are found
func sanitizeConfig(c *sdkConfig.Config) error {
	// If no squashcompression is set, zero the compression parameters
	// By default on NewConfig the SquashFsCompressionConfig is set to the default values, and then override
	// on config unmarshall.
	if c.SquashFsNoCompression {
		c.SquashFsCompressionConfig = []string{}
	}
	if c.Arch != "" && c.Platform == nil {
		p, err := platform.NewPlatformFromArch(c.Arch)
		if err != nil {
			return err
		}
		c.Platform = p
	}

	if c.Platform == nil {
		p, err := platform.NewPlatformFromArch(runtime.GOARCH)
		if err != nil {
			return err
		}
		c.Platform = p
	}

	return nil
}

type GenericOptions func(a *sdkConfig.Config)

func WithFs(fs sdkFs.KairosFS) func(r *sdkConfig.Config) {
	return func(r *sdkConfig.Config) {
		r.Fs = fs
	}
}

func WithLogger(logger logger.KairosLogger) func(r *sdkConfig.Config) {
	return func(r *sdkConfig.Config) {
		r.Logger = logger
	}
}

func WithSyscall(syscall sdkSyscall.Interface) func(r *sdkConfig.Config) {
	return func(r *sdkConfig.Config) {
		r.Syscall = syscall
	}
}

func WithMounter(mounter mount.Interface) func(r *sdkConfig.Config) {
	return func(r *sdkConfig.Config) {
		r.Mounter = mounter
	}
}

func WithRunner(runner sdkRunner.Runner) func(r *sdkConfig.Config) {
	return func(r *sdkConfig.Config) {
		r.Runner = runner
	}
}

func WithClient(client sdkHttp.Client) func(r *sdkConfig.Config) {
	return func(r *sdkConfig.Config) {
		r.Client = client
	}
}

func WithCloudInitRunner(ci cloudinitrunner.CloudInitRunner) func(r *sdkConfig.Config) {
	return func(r *sdkConfig.Config) {
		r.CloudInitRunner = ci
	}
}

func WithPlatform(plat string) func(r *sdkConfig.Config) {
	return func(r *sdkConfig.Config) {
		p, err := platform.ParsePlatform(plat)
		if err == nil {
			r.Platform = p
		}
	}
}

func WithImageExtractor(extractor sdkImages.ImageExtractor) func(r *sdkConfig.Config) {
	return func(r *sdkConfig.Config) {
		r.ImageExtractor = extractor
	}
}

type Stage string

const (
	NetworkStage   Stage = "network"
	InitramfsStage Stage = "initramfs"
)

func (n Stage) String() string {
	return string(n)
}

func MergeYAML(objs ...interface{}) ([]byte, error) {
	content := [][]byte{}
	for _, o := range objs {
		dat, err := yaml.Marshal(o)
		if err != nil {
			return []byte{}, err
		}
		content = append(content, dat)
	}

	finalData := make(map[string]interface{})

	for _, c := range content {
		if err := yaml.Unmarshal(c, &finalData); err != nil {
			return []byte{}, err
		}
	}

	return yaml.Marshal(finalData)
}

func AddHeader(header, data string) string {
	return fmt.Sprintf("%s\n%s", header, data)
}
