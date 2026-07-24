package cli

import (
	"testing"

	"github.com/kairos-io/provider-kairos/v2/internal/provider"
	urfavecli "github.com/urfave/cli/v2"
)

func TestAPIDefaultsToEdgeVPNUnixSocket(t *testing.T) {
	apiFlag, ok := networkAPI[0].(*urfavecli.StringFlag)
	if !ok {
		t.Fatalf("api flag has unexpected type %T", networkAPI[0])
	}
	if apiFlag.Value != provider.DefaultEdgeVPNAPIAddress {
		t.Fatalf("api default = %q, want %q", apiFlag.Value, provider.DefaultEdgeVPNAPIAddress)
	}
	if len(apiFlag.EnvVars) != 1 || apiFlag.EnvVars[0] != "EDGEVPN_API" {
		t.Fatalf("api environment overrides = %v, want [EDGEVPN_API]", apiFlag.EnvVars)
	}
}
