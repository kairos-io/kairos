package bundles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/kairos-io/kairos/pkg/utils"
)

const (
	filePrefix = "file://"
)

type BundleConfig struct {
	Target     string
	Repository string
	DBPath     string
	RootPath   string
	LocalFile  bool
}

// BundleOption defines a configuration option for a bundle.
type BundleOption func(bc *BundleConfig) error

// Apply applies bundle options to the config.
func (bc *BundleConfig) Apply(opts ...BundleOption) error {
	for _, o := range opts {
		if err := o(bc); err != nil {
			return err
		}
	}
	return nil
}

// WithDBPath sets the DB path for package installs.
// In case of luet packages will contain the db of the installed packages.
func WithDBPath(r string) BundleOption {
	return func(bc *BundleConfig) error {
		bc.DBPath = r
		return nil
	}
}

func WithRootFS(r string) BundleOption {
	return func(bc *BundleConfig) error {
		bc.RootPath = r
		return nil
	}
}

func WithRepository(r string) BundleOption {
	return func(bc *BundleConfig) error {
		bc.Repository = r
		return nil
	}
}

func WithTarget(p string) BundleOption {
	return func(bc *BundleConfig) error {
		bc.Target = p
		return nil
	}
}

func (bc *BundleConfig) extractRepo() (string, string, error) {
	s := strings.Split(bc.Repository, "://")
	if len(s) != 2 {
		return "", "", fmt.Errorf("invalid repo schema")
	}
	return s[0], s[1], nil
}

func defaultConfig() *BundleConfig {
	return &BundleConfig{
		DBPath:     "/usr/local/.kairos/db",
		RootPath:   "/",
		Repository: "docker://quay.io/kairos/packages",
	}
}

type BundleInstaller interface {
	Install(*BundleConfig) error
}

// RunBundles runs bundles in a system.
// Accept a list of bundles options, which gets applied based on the bundle configuration.
func RunBundles(bundles ...[]BundleOption) error {

	// TODO:
	// - Make provider consume bundles when bins are not detected in the rootfs
	// - Default bundles preset in case of no binaries detected and version specified via config.

	var resErr error
	for _, b := range bundles {
		config := defaultConfig()
		if err := config.Apply(b...); err != nil {
			resErr = multierror.Append(err)
			continue
		}

		installer, err := NewBundleInstaller(*config)
		if err != nil {
			resErr = multierror.Append(err)
			continue
		}
		dat := strings.Split(config.Target, "://")
		if len(dat) != 2 {
			resErr = multierror.Append(fmt.Errorf("invalid target"))
			continue
		}
		config.Target = dat[1]

		err = installer.Install(config)
		if err != nil {
			resErr = multierror.Append(err)
			continue
		}
	}

	return resErr
}

func NewBundleInstaller(bc BundleConfig) (BundleInstaller, error) {

	dat := strings.Split(bc.Target, "://")
	if len(dat) != 2 {
		return nil, fmt.Errorf("could not decode scheme")
	}
	switch strings.ToLower(dat[0]) {
	case "container":
		return &ContainerInstaller{}, nil
	case "run":
		return &ContainerRunner{}, nil
	case "package":
		return &LuetInstaller{}, nil

	}

	return &LuetInstaller{}, nil
}

// BundleInstall installs a bundle from a luet repo or a container image.
type ContainerRunner struct{}

func (l *ContainerRunner) Install(config *BundleConfig) error {

	tempDir, err := os.MkdirTemp("", "containerrunner")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	target := config.Target
	if config.LocalFile {
		target = strings.Join([]string{filePrefix, target}, "")
	}

	out, err := utils.SH(
		fmt.Sprintf(
			`luet util unpack %s %s`,
			target,
			tempDir,
		),
	)
	if err != nil {
		return fmt.Errorf("could not unpack container: %w - %s", err, out)
	}

	// We want to expect tempDir as context
	out, err = utils.SHInDir(fmt.Sprintf("CONTAINERDIR=%s %s/run.sh", tempDir, tempDir), tempDir)
	if err != nil {
		return fmt.Errorf("could not execute container: %w - %s", err, out)
	}
	return nil
}

type ContainerInstaller struct{}

func (l *ContainerInstaller) Install(config *BundleConfig) error {

	target := config.Target
	if config.LocalFile {
		target = strings.Join([]string{filePrefix, target}, "")
	}

	//mkdir -p test/etc/luet/repos.conf.d
	out, err := utils.SH(
		fmt.Sprintf(
			`luet util unpack %s %s`,
			target,
			config.RootPath,
		),
	)
	if err != nil {
		return fmt.Errorf("could not unpack bundle: %w - %s", err, out)
	}

	return nil
}

type LuetInstaller struct{}

func (l *LuetInstaller) Install(config *BundleConfig) error {

	t, repo, err := config.extractRepo()
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Join(config.RootPath, "etc/luet/repos.conf.d/"), os.ModePerm)
	if err != nil {
		return err
	}
	out, err := utils.SH(
		fmt.Sprintf(
			`LUET_CONFIG_FROM_HOST=false luet repo add --system-dbpath %s --system-target %s kairos-system -y --description "Automatically generated kairos-system" --url "%s" --type "%s"`,
			config.DBPath,
			config.RootPath,
			repo,
			t,
		),
	)
	if err != nil {
		return fmt.Errorf("could not add repository: %w - %s", err, out)
	}

	out, err = utils.SH(
		fmt.Sprintf(
			`LUET_CONFIG_FROM_HOST=false luet install -y  --system-dbpath %s --system-target %s %s`,
			config.DBPath,
			config.RootPath,
			config.Target,
		),
	)
	if err != nil {
		return fmt.Errorf("could not install bundle: %w - %s", err, out)
	}

	// copy bins to /usr/local/bin
	return nil
}
