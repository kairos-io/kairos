package bundles

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/hashicorp/go-multierror"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/kairos-io/kairos-sdk/utils/image"
	registrytypes "github.com/moby/moby/api/types/registry"
)

const (
	filePrefix = "file://" //nolint:unused
)

type BundleConfig struct {
	Target     string
	Repository string
	DBPath     string
	RootPath   string
	LocalFile  bool
	Auth       *registrytypes.AuthConfig
	Transport  http.RoundTripper
	Platform   string
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

func WithLocalFile(p bool) BundleOption {
	return func(bc *BundleConfig) error {
		bc.LocalFile = p
		return nil
	}
}

func WithAuth(c *registrytypes.AuthConfig) BundleOption {
	return func(bc *BundleConfig) error {
		bc.Auth = c
		return nil
	}
}

func WithTransport(t http.RoundTripper) BundleOption {
	return func(bc *BundleConfig) error {
		bc.Transport = t
		return nil
	}
}

func WithPlatform(p string) BundleOption {
	return func(bc *BundleConfig) error {
		bc.Platform = p
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

func (bc *BundleConfig) TargetScheme() (string, error) {
	dat := strings.Split(bc.Target, "://")
	if len(dat) != 2 {
		return "", errors.New("invalid target")
	}
	return strings.ToLower(dat[0]), nil
}

func (bc *BundleConfig) TargetNoScheme() (string, error) {
	dat := strings.Split(bc.Target, "://")
	if len(dat) != 2 {
		return "", errors.New("invalid target")
	}
	return dat[1], nil
}

func defaultConfig() *BundleConfig {
	return &BundleConfig{
		DBPath:     "/usr/local/.kairos/db",
		RootPath:   "/",
		Repository: "docker://quay.io/kairos/packages",
		Auth:       nil,
		Transport:  http.DefaultTransport,
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

		err = installer.Install(config)
		if err != nil {
			resErr = multierror.Append(err)
			continue
		}
	}

	return resErr
}

func NewBundleInstaller(bc BundleConfig) (BundleInstaller, error) {
	scheme, err := bc.TargetScheme()
	if err != nil {
		return nil, err
	}

	switch scheme {
	case "container", "docker":
		return &OCIImageExtractor{
			Local: bc.LocalFile,
		}, nil
	case "run":
		return &OCIImageRunner{
			Local: bc.LocalFile,
		}, nil
	case "package":
		return &LuetInstaller{}, nil
	}

	return &LuetInstaller{}, nil
}

// OCIImageExtractor will extract an OCI image
type OCIImageExtractor struct {
	Local bool
}

func (e OCIImageExtractor) Install(config *BundleConfig) error {
	if !utils.Exists(config.RootPath) {
		err := os.MkdirAll(config.RootPath, os.ModeDir|os.ModePerm)
		if err != nil {
			return fmt.Errorf("could not create destination path %s: %s", config.RootPath, err)
		}
	}

	var img v1.Image
	var err error
	target, err := config.TargetNoScheme()
	if err != nil {
		return err
	}

	if e.Local {
		img, err = tarball.ImageFromPath(target, nil)
	} else {
		img, err = image.GetImage(target, config.Platform, config.Auth, config.Transport)
	}
	if err != nil {
		return err
	}

	return image.ExtractOCIImage(img, config.RootPath)
}

// OCIImageRunner will extract an OCI image and then run its run.sh
type OCIImageRunner struct {
	Local bool
}

func (e OCIImageRunner) Install(config *BundleConfig) error {
	tempDir, err := os.MkdirTemp("", "containerrunner")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	var img v1.Image
	target, err := config.TargetNoScheme()
	if err != nil {
		return err
	}

	if e.Local {
		img, err = tarball.ImageFromPath(target, nil)
	} else {
		img, err = image.GetImage(target, config.Platform, config.Auth, config.Transport)
	}
	if err != nil {
		return err
	}

	err = image.ExtractOCIImage(img, tempDir)
	if err != nil {
		return err
	}

	if err = os.Chmod(filepath.Join(tempDir, "run.sh"), 0700); err != nil {
		return err
	}

	// We want to expect tempDir as context
	out, err := utils.SHInDir(
		"./run.sh",
		tempDir,
		fmt.Sprintf("CONTAINERDIR=%s", tempDir), fmt.Sprintf("BUNDLE_TARGET=%s", target))
	if err != nil {
		return fmt.Errorf("could not execute container: %w - %s", err, out)
	}

	return err
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
	out, err := utils.SH(luetInstallCommand(config, repo, t))
	if err != nil {
		return fmt.Errorf("could not add repository: %w - %s", err, out)
	}

	target, err := config.TargetNoScheme()
	if err != nil {
		return err
	}
	out, err = utils.SH(
		fmt.Sprintf(
			`LUET_CONFIG_FROM_HOST=false luet install -y  --system-dbpath %s --system-target %s %s`,
			config.DBPath,
			config.RootPath,
			target,
		),
	)
	if err != nil {
		return fmt.Errorf("could not install bundle: %w - %s", err, out)
	}

	// copy bins to /usr/local/bin
	return nil
}

func luetInstallCommand(config *BundleConfig, repo, t string) string {
	baseCommand := fmt.Sprintf(
		`LUET_CONFIG_FROM_HOST=false luet repo add --system-dbpath %s --system-target %s kairos-system -y --description "Automatically generated kairos-system" --url "%s" --type "%s"`,
		config.DBPath,
		config.RootPath,
		repo,
		t,
	)

	if config.Auth != nil {
		baseCommand += fmt.Sprintf(` --username "%s" --passwd "%s"`, config.Auth.Username, config.Auth.Password)
	}

	return baseCommand
}
