// Package service manages system services without naming an init system.
//
// A Spec describes a service in the terms every init system shares: a name, an
// optional instance, the unit body to install, and the file that unit sources
// for its environment. New builds a Service for the init system of the running
// host, so callers never branch on the init system themselves. The parts that
// genuinely differ between init systems live in Spec.Init, keyed by flavor.
//
// Supporting another init system means adding a Backend and registering it.
// Nothing outside this package has to change.
package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// Flavor names an init system. The values match what utils.GetInit reports.
type Flavor string

const (
	Systemd Flavor = "systemd"
	OpenRC  Flavor = "openrc"
)

// EnvFilePlaceholder is replaced with the service's env file path everywhere it
// appears in a unit body, at the moment WriteUnit writes it. Use it so a unit
// that sources its env file does not repeat a path that Spec already carries:
// the two cannot then drift apart.
const EnvFilePlaceholder = "@ENVFILE@"

// Service is what an init system can do with a single service.
//
// Backends implement all of it. Where an init system cannot express one of
// these, the backend returns an error instead of reporting success for
// something that did not happen.
type Service interface {
	// Name is the service name, with no instance and no unit suffix.
	Name() string

	// WriteUnit installs the unit body for the running init system.
	WriteUnit() error

	// OverrideCmd changes the command line the service runs. cmd is the whole
	// command line, binary included, so the same string works on every init
	// system regardless of how that init system splits it up.
	OverrideCmd(cmd string) error

	// EnvFile is the file the unit sources for its environment, as the unit
	// sees it at runtime. It is not prefixed with the Spec's Root, so it is the
	// path to write into a unit body, not the path to write to from here: use
	// SetEnv for that. Empty when the service has no env file.
	EnvFile() string

	// SetEnv merges values into the service's env file, keeping the ones
	// already there. It fails when the service has no env file.
	SetEnv(env map[string]string) error

	Start() error
	StartBlocking() error
	Stop() error
	Restart() error
	Enable() error
	Disable() error
}

// InitSpec holds the parts of a service that cannot be stated once for every
// init system.
type InitSpec struct {
	// Unit is the unit body to install: an openrc script, a systemd unit. Any
	// EnvFilePlaceholder in it is substituted when it is written.
	Unit string

	// EnvFile is the path the unit sources for its environment, and for the
	// arguments OverrideCmd writes on init systems that pass them that way.
	// Leave it empty to take the backend's default, which is the convention of
	// that init system rather than of any one service.
	EnvFile string
}

// Spec describes a service.
type Spec struct {
	// Name is the service name, e.g. "k3s".
	Name string

	// Instance selects one instance of a templated service. Init systems
	// without templated units ignore it.
	Instance string

	// Root is the filesystem root to write under, for building an image or a
	// chroot rather than configuring the running host. Empty means /.
	Root string

	// NoReload skips the reload of the init system's own configuration after
	// WriteUnit. Set it when writing units for a system that is not running,
	// where there is no init system to talk to.
	NoReload bool

	// Init carries the per-init-system parts of the service. A flavor with no
	// entry gets an empty InitSpec, which is right for a service whose unit is
	// packaged by the distro and only needs to be started.
	Init map[Flavor]InitSpec
}

// For returns the InitSpec for a flavor, or the zero value when the Spec says
// nothing about it.
func (s Spec) For(f Flavor) InitSpec {
	return s.Init[f]
}

// Backend builds services for one init system.
type Backend interface {
	// Flavor is the init system this backend drives.
	Flavor() Flavor
	// New builds a service from a spec. It is called after the spec has been
	// validated, so it can assume a non-empty Name.
	New(Spec) (Service, error)
}

var backends = map[Flavor]Backend{}

// Register makes a backend available to New and NewFor, replacing any backend
// already registered for the same flavor.
func Register(b Backend) {
	backends[b.Flavor()] = b
}

func init() {
	Register(systemdBackend{})
	Register(openrcBackend{})
}

// Flavors lists the init systems with a registered backend, sorted.
func Flavors() []Flavor {
	out := make([]Flavor, 0, len(backends))
	for f := range backends {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Detect reports the init system of the running host.
//
// An init system with no backend, including one utils.GetInit cannot name, is
// reported as systemd. That is what the code this replaced did by only ever
// testing for openrc and treating everything else as systemd.
func Detect() Flavor {
	f := Flavor(utils.GetInit())
	if _, ok := backends[f]; ok {
		return f
	}
	return Systemd
}

// New builds a service for the init system of the running host.
func New(spec Spec) (Service, error) {
	return NewFor(Detect(), spec)
}

// NewFor builds a service for a named init system, whatever the host runs.
// Tests and image builders want this; ordinary callers want New.
func NewFor(f Flavor, spec Spec) (Service, error) {
	if spec.Name == "" {
		return nil, fmt.Errorf("service has no name")
	}

	b, ok := backends[f]
	if !ok {
		names := make([]string, 0, len(backends))
		for _, k := range Flavors() {
			names = append(names, string(k))
		}
		return nil, fmt.Errorf("no service backend for init system %q, have %s", f, strings.Join(names, ", "))
	}

	return b.New(spec)
}

// Noop implements Service by doing nothing at all. Embed it to implement only
// the parts of Service that mean something for a given service, instead of
// writing out the whole interface.
type Noop struct {
	ServiceName string
}

func (n Noop) Name() string                 { return n.ServiceName }
func (Noop) WriteUnit() error               { return nil }
func (Noop) OverrideCmd(string) error       { return nil }
func (Noop) EnvFile() string                { return "" }
func (Noop) SetEnv(map[string]string) error { return nil }
func (Noop) Start() error                   { return nil }
func (Noop) StartBlocking() error           { return nil }
func (Noop) Stop() error                    { return nil }
func (Noop) Restart() error                 { return nil }
func (Noop) Enable() error                  { return nil }
func (Noop) Disable() error                 { return nil }

// substituteEnvFile fills EnvFilePlaceholder in a unit body with the runtime
// path of the service's env file.
func substituteEnvFile(unit, envFile string) string {
	if !strings.Contains(unit, EnvFilePlaceholder) {
		return unit
	}
	return strings.ReplaceAll(unit, EnvFilePlaceholder, envFile)
}

// unitToWrite returns the unit body to install, or an error when the spec
// carries none for this init system.
//
// Writing an empty unit is never what a caller means: for a service whose unit
// the distribution packages, it would truncate that file and leave the service
// unable to start.
func unitToWrite(name string, init InitSpec, envFile string) (string, error) {
	if init.Unit == "" {
		return "", fmt.Errorf("service %s has no unit to write", name)
	}

	return substituteEnvFile(init.Unit, envFile), nil
}
