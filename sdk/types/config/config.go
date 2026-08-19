package config

import (
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/types/bundles"
	"github.com/kairos-io/kairos-sdk/types/cloudinitrunner"
	"github.com/kairos-io/kairos-sdk/types/fs"
	"github.com/kairos-io/kairos-sdk/types/http"
	"github.com/kairos-io/kairos-sdk/types/images"
	"github.com/kairos-io/kairos-sdk/types/install"
	"github.com/kairos-io/kairos-sdk/types/logger"
	log "github.com/kairos-io/kairos-sdk/types/logs"
	"github.com/kairos-io/kairos-sdk/types/platform"
	"github.com/kairos-io/kairos-sdk/types/runner"
	"github.com/kairos-io/kairos-sdk/types/syscall"
	"k8s.io/mount-utils"
)

// You would probably be thinking, why is the Config struct in here? Well, the types
// package is already imported everywhere, so putting it here avoids cyclic imports
// and makes it easier to use across the codebase.
// Plus things like providers can import them in order to modify and send back the install info
// so its nice that its in a central place for providers to consume and be able to alter install behavior easily.

type Config struct {
	Install                   *install.Install                `yaml:"install,omitempty"`
	Collector                 collector.Config                `yaml:"-"`
	ConfigURL                 string                          `yaml:"config_url,omitempty"`
	Options                   map[string]string               `yaml:"options,omitempty"`
	FailOnBundleErrors        bool                            `yaml:"fail_on_bundles_errors,omitempty"`
	Bundles                   bundles.Bundles                 `yaml:"bundles,omitempty"`
	GrubOptions               map[string]string               `yaml:"grub_options,omitempty"`
	Env                       []string                        `yaml:"env,omitempty"`
	Debug                     bool                            `yaml:"debug,omitempty" mapstructure:"debug"`
	Strict                    bool                            `yaml:"strict,omitempty" mapstructure:"strict"`
	CloudInitPaths            []string                        `yaml:"cloud-init-paths,omitempty" mapstructure:"cloud-init-paths"`
	EjectCD                   bool                            `yaml:"eject-cd,omitempty" mapstructure:"eject-cd"`
	Logger                    logger.KairosLogger             `yaml:"-"`
	Fs                        fs.KairosFS                     `yaml:"-"`
	Mounter                   mount.Interface                 `yaml:"-"`
	Runner                    runner.Runner                   `yaml:"-"`
	Syscall                   syscall.Interface               `yaml:"-"`
	CloudInitRunner           cloudinitrunner.CloudInitRunner `yaml:"-"`
	ImageExtractor            images.ImageExtractor           `yaml:"-"`
	Client                    http.Client                     `yaml:"-"`
	Platform                  *platform.Platform              `yaml:"-"`
	Cosign                    bool                            `yaml:"cosign,omitempty" mapstructure:"cosign"`
	Verify                    bool                            `yaml:"verify,omitempty" mapstructure:"verify"`
	CosignPubKey              string                          `yaml:"cosign-key,omitempty" mapstructure:"cosign-key"`
	Arch                      string                          `yaml:"arch,omitempty" mapstructure:"arch"`
	SquashFsCompressionConfig []string                        `yaml:"squash-compression,omitempty" mapstructure:"squash-compression"`
	SquashFsNoCompression     bool                            `yaml:"squash-no-compression,omitempty" mapstructure:"squash-no-compression"`
	UkiMaxEntries             int                             `yaml:"uki-max-entries,omitempty" mapstructure:"uki-max-entries"`
	BindPCRs                  []string                        `yaml:"bind-pcrs,omitempty" mapstructure:"bind-pcrs"`
	BindPublicPCRs            []string                        `yaml:"bind-public-pcrs,omitempty" mapstructure:"bind-public-pcrs"`
	Logs                      *log.LogsConfig                 `yaml:"logs,omitempty"`
}
