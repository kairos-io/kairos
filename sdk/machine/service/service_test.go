package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewForRejectsAnUnnamedService(t *testing.T) {
	if _, err := NewFor(Systemd, Spec{}); err == nil {
		t.Error("expected an error for a service with no name, got none")
	}
}

func TestNewForRejectsAnUnknownInitSystem(t *testing.T) {
	_, err := NewFor(Flavor("upstart"), Spec{Name: "myservice"})
	if err == nil {
		t.Fatal("expected an error for an init system with no backend, got none")
	}
	// The error has to say what is available, or a packager adding an init
	// system has nothing to go on.
	for _, want := range []string{"upstart", "openrc", "systemd"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// An init system we cannot name is driven as systemd, which is what the callers
// this package replaced did by only ever testing for openrc.
func TestDetectFallsBackToSystemd(t *testing.T) {
	if f := Detect(); f != Systemd && f != OpenRC {
		t.Errorf("Detect returned %q, want one of the registered flavors", f)
	}
}

func TestFlavorsListsTheRegisteredBackends(t *testing.T) {
	if got, want := Flavors(), []Flavor{OpenRC, Systemd}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Flavors() = %v, want %v", got, want)
	}
}

// Every backend has to answer every method of the interface. A backend that
// cannot do something returns an error; none of them may report success for
// something that did not happen.
func TestEveryBackendWritesTheUnitItIsGiven(t *testing.T) {
	for _, f := range Flavors() {
		t.Run(string(f), func(t *testing.T) {
			root := t.TempDir()
			svc, err := NewFor(f, Spec{
				Name:     "myservice",
				Root:     root,
				NoReload: true,
				Init: map[Flavor]InitSpec{
					f: {Unit: "unit body for " + string(f), EnvFile: "/etc/myservice.env"},
				},
			})
			if err != nil {
				t.Fatalf("NewFor: %s", err)
			}

			if got := svc.Name(); got != "myservice" {
				t.Errorf("Name() = %q, want %q", got, "myservice")
			}
			if err := svc.WriteUnit(); err != nil {
				t.Fatalf("WriteUnit: %s", err)
			}

			var found bool
			err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return err
				}
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if strings.Contains(string(b), "unit body for "+string(f)) {
					found = true
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walking %s: %s", root, err)
			}
			if !found {
				t.Error("WriteUnit wrote nothing containing the unit body")
			}
		})
	}
}

// A service with an env file takes environment on every init system, so callers
// never have to know where it goes.
func TestEveryBackendSetsEnv(t *testing.T) {
	for _, f := range Flavors() {
		t.Run(string(f), func(t *testing.T) {
			root := t.TempDir()
			svc, err := NewFor(f, Spec{
				Name: "myservice",
				Root: root,
				Init: map[Flavor]InitSpec{f: {EnvFile: "/etc/myservice/myservice.env"}},
			})
			if err != nil {
				t.Fatalf("NewFor: %s", err)
			}

			if err := svc.SetEnv(map[string]string{"TOKEN": "sometoken"}); err != nil {
				t.Fatalf("SetEnv: %s", err)
			}

			got, err := os.ReadFile(filepath.Join(root, svc.EnvFile()))
			if err != nil {
				t.Fatalf("reading the env file: %s", err)
			}
			if want := "TOKEN=\"sometoken\"\n"; string(got) != want {
				t.Errorf("env file content:\ngot  %q\nwant %q", string(got), want)
			}
		})
	}
}

// EnvFile is the path the unit sees at runtime, not the path under Root. A unit
// written into an image that pointed at the build root would be broken on the
// installed system.
func TestEnvFileIsNotPrefixedWithRoot(t *testing.T) {
	for _, f := range Flavors() {
		t.Run(string(f), func(t *testing.T) {
			svc, err := NewFor(f, Spec{
				Name: "myservice",
				Root: "/tmp/somebuildroot",
				Init: map[Flavor]InitSpec{f: {EnvFile: "/etc/myservice.env"}},
			})
			if err != nil {
				t.Fatalf("NewFor: %s", err)
			}
			if got := svc.EnvFile(); got != "/etc/myservice.env" {
				t.Errorf("EnvFile() = %q, want the runtime path /etc/myservice.env", got)
			}
		})
	}
}

func TestNoopSatisfiesService(t *testing.T) {
	var svc Service = Noop{ServiceName: "nothing"}

	if got := svc.Name(); got != "nothing" {
		t.Errorf("Name() = %q, want %q", got, "nothing")
	}
	for name, err := range map[string]error{
		"WriteUnit":     svc.WriteUnit(),
		"OverrideCmd":   svc.OverrideCmd("whatever"),
		"SetEnv":        svc.SetEnv(nil),
		"Start":         svc.Start(),
		"StartBlocking": svc.StartBlocking(),
		"Stop":          svc.Stop(),
		"Restart":       svc.Restart(),
		"Enable":        svc.Enable(),
		"Disable":       svc.Disable(),
	} {
		if err != nil {
			t.Errorf("%s returned %v, want nil", name, err)
		}
	}
}

// Registering a backend is all it takes to support another init system.
func TestRegisterAddsABackend(t *testing.T) {
	const runit = Flavor("runit")
	t.Cleanup(func() { delete(backends, runit) })

	Register(fakeBackend{flavor: runit})

	svc, err := NewFor(runit, Spec{Name: "myservice"})
	if err != nil {
		t.Fatalf("NewFor: %s", err)
	}
	if got := svc.Name(); got != "myservice" {
		t.Errorf("Name() = %q, want %q", got, "myservice")
	}
}

type fakeBackend struct {
	flavor Flavor
}

func (b fakeBackend) Flavor() Flavor { return b.flavor }

func (b fakeBackend) New(spec Spec) (Service, error) {
	return Noop{ServiceName: spec.Name}, nil
}

// k3s packages its own openrc script and systemd unit. A WriteUnit that wrote
// an empty file for a service with no unit body would truncate that packaged
// file and leave the service unable to start.
func TestWriteUnitRefusesAnEmptyUnit(t *testing.T) {
	for _, f := range Flavors() {
		t.Run(string(f), func(t *testing.T) {
			root := t.TempDir()
			svc, err := NewFor(f, Spec{
				Name:     "k3s",
				Root:     root,
				NoReload: true,
				Init:     map[Flavor]InitSpec{f: {EnvFile: "/etc/rancher/k3s/k3s.env"}},
			})
			if err != nil {
				t.Fatalf("NewFor: %s", err)
			}

			if err := svc.WriteUnit(); err == nil {
				t.Error("expected an error for a service with no unit body, got none")
			}

			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("reading %s: %s", root, err)
			}
			if len(entries) != 0 {
				t.Errorf("nothing should have been written, found %d entries", len(entries))
			}
		})
	}
}
