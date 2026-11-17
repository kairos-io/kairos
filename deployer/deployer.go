package deployer

import (
	"os"

	"github.com/hashicorp/go-multierror"
	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	sdklogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

	"github.com/spectrocloud-labs/herd"
	"gopkg.in/yaml.v3"
)

type Deployer struct {
	*herd.Graph
	Config   schema.Config
	Artifact schema.ReleaseArtifact
	// Log is where every step in this deployer emits progress. Callers who
	// want the deployer's zerolog output to reach a specific sink (for
	// example an AuroraBoot build's live-log pane) set this before Run.
	// NewDeployer defaults it to internal.Log so callers that don't care
	// (the CLI) keep their existing terminal output.
	Log sdklogger.KairosLogger
	// WorkDir is a private directory that holds the helper files of one run:
	// the cloud config copy, the unpacked rootfs and the netboot dir. It is set
	// by build-iso so that State only receives the finished artifacts. When
	// empty, those files keep their State based layout.
	WorkDir string
	// generatedISO is the ISO the GenISO step of this run wrote, empty
	// until it has written one.
	generatedISO string
}

func NewDeployer(c schema.Config, a schema.ReleaseArtifact, opts ...herd.GraphOption) *Deployer {
	d := &Deployer{Config: c, Artifact: a, Log: internal.Log}
	d.Graph = herd.DAG(opts...)

	return d
}

func (d *Deployer) CollectErrors() error {
	var err error
	for _, layer := range d.Analyze() {
		for _, op := range layer {
			if op.Error != nil {
				err = multierror.Append(err, op.Error)
			}
		}
	}

	return err
}

func (d *Deployer) WriteDag() {
	graph := d.Analyze()
	for i, layer := range graph {
		d.Log.Printf("%d.", (i + 1))
		for _, op := range layer {
			if !op.Ignored {
				if op.Error != nil {
					d.Log.Printf(" <%s> (error: %s) (background: %t)", op.Name, op.Error.Error(), op.Background)
				} else {
					d.Log.Printf(" <%s> (background: %t)", op.Name, op.Background)
				}
			}
		}
		d.Log.Print("")
	}
}

func LoadByte(b []byte) (*schema.Config, *schema.ReleaseArtifact, error) {
	config := &schema.Config{}
	release := &schema.ReleaseArtifact{}

	if err := yaml.Unmarshal(b, config); err != nil {
		return nil, nil, err
	}

	if err := yaml.Unmarshal(b, release); err != nil {
		return nil, nil, err
	}

	config.HandleDeprecations(internal.Log)

	return config, release, nil
}

// LoadFile loads a configuration file and returns the AuroraBoot configuration
// and release artifact information
func LoadFile(file string) (*schema.Config, *schema.ReleaseArtifact, error) {

	dat, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}

	return LoadByte(dat)
}
