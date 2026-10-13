package services

import (
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/machine/service"
)

// These are the paths the k3s and k0s services read, from before any of this
// was init-agnostic. They are pinned here because getting one wrong means the
// provider writes the node's token and arguments to a file the service never
// reads, and the node comes up with the defaults and no error anywhere.
func TestServiceEnvFilesAreUnchanged(t *testing.T) {
	tests := []struct {
		flavor service.Flavor
		spec   service.Spec
		want   string
	}{
		{service.OpenRC, K3sSpec("k3s"), "/etc/rancher/k3s/k3s.env"},
		{service.OpenRC, K3sSpec("k3s-agent"), "/etc/rancher/k3s/k3s-agent.env"},
		{service.Systemd, K3sSpec("k3s"), "/etc/sysconfig/k3s"},
		{service.Systemd, K3sSpec("k3s-agent"), "/etc/sysconfig/k3s-agent"},
		{service.OpenRC, K0sSpec("k0scontroller"), "/etc/k0s/k0scontroller.env"},
		{service.OpenRC, K0sSpec("k0sworker"), "/etc/k0s/k0sworker.env"},
		{service.Systemd, K0sSpec("k0scontroller"), "/etc/sysconfig/k0scontroller"},
		{service.Systemd, K0sSpec("k0sworker"), "/etc/sysconfig/k0sworker"},
	}

	for _, tt := range tests {
		t.Run(string(tt.flavor)+"/"+tt.spec.Name, func(t *testing.T) {
			svc, err := service.NewFor(tt.flavor, tt.spec)
			if err != nil {
				t.Fatalf("building the service: %v", err)
			}
			if got := svc.EnvFile(); got != tt.want {
				t.Errorf("env file is %q, want %q", got, tt.want)
			}
		})
	}
}

// k3s installs its own openrc script and systemd unit, so Kairos has no unit
// body to write for it. Writing an empty one would truncate the packaged file.
func TestK3sSpecShipsNoUnitBody(t *testing.T) {
	for _, name := range K3sServiceNames {
		spec := K3sSpec(name)
		for _, f := range []service.Flavor{service.OpenRC, service.Systemd} {
			if unit := spec.For(f).Unit; unit != "" {
				t.Errorf("%s on %s carries a unit body, k3s ships its own:\n%s", name, f, unit)
			}
		}
	}
}
