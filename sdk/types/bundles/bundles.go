package bundles

import "github.com/kairos-io/kairos-sdk/bundles"

type Bundles []Bundle

type Bundle struct {
	Repository string   `yaml:"repository,omitempty" json:"repository,omitempty" mapstructure:"repository"`
	Rootfs     string   `yaml:"rootfs_path,omitempty" json:"rootfs_path,omitempty" mapstructure:"rootfs_path"`
	DB         string   `yaml:"db_path,omitempty" json:"db_path,omitempty" mapstructure:"db_path"`
	LocalFile  bool     `yaml:"local_file,omitempty" json:"local_file,omitempty" mapstructure:"local_file"`
	Targets    []string `yaml:"targets,omitempty" json:"targets,omitempty" mapstructure:"targets"`
}

func (b Bundles) Options() (res [][]bundles.BundleOption) {
	for _, bundle := range b {
		for _, t := range bundle.Targets {
			opts := []bundles.BundleOption{bundles.WithRepository(bundle.Repository), bundles.WithTarget(t)}
			if bundle.Rootfs != "" {
				opts = append(opts, bundles.WithRootFS(bundle.Rootfs))
			}
			if bundle.DB != "" {
				opts = append(opts, bundles.WithDBPath(bundle.DB))
			}
			if bundle.LocalFile {
				opts = append(opts, bundles.WithLocalFile(true))
			}
			res = append(res, opts)
		}
	}
	return
}
