package schema

// K3sSchema represents the k3s and k3s-agent blocks in the Kairos
// configuration. It mirrors the K3s struct that
// provider/internal/provider/config unmarshals, so that print-schema
// describes the keys the provider actually reads.
type K3sSchema struct {
	_                struct{}          `title:"Kairos Schema: K3s block" description:"Configures the K3s distribution shipped by the Kairos provider."`
	Enabled          bool              `json:"enabled,omitempty" description:"Enables the K3s service. Without this K3s is installed but never started."`
	Env              map[string]string `json:"env,omitempty" description:"Environment variables to pass to the K3s service."`
	ReplaceEnv       bool              `json:"replace_env,omitempty" description:"Replace the default environment instead of appending to it."`
	Args             []string          `json:"args,omitempty" description:"Additional arguments to pass to K3s."`
	ReplaceArgs      bool              `json:"replace_args,omitempty" description:"Replace the default arguments instead of appending to them."`
	EmbeddedRegistry bool              `json:"embedded_registry,omitempty" description:"Enables the K3s embedded registry mirror."`
}

// K0sSchema represents the k0s and k0s-worker blocks in the Kairos
// configuration. It mirrors the K0s struct that
// provider/internal/provider/config unmarshals.
type K0sSchema struct {
	_           struct{}          `title:"Kairos Schema: K0s block" description:"Configures the K0s distribution shipped by the Kairos provider."`
	Enabled     bool              `json:"enabled,omitempty" description:"Enables the K0s service. Without this K0s is installed but never started."`
	Env         map[string]string `json:"env,omitempty" description:"Environment variables to pass to the K0s service."`
	ReplaceEnv  bool              `json:"replace_env,omitempty" description:"Replace the default environment instead of appending to it."`
	Args        []string          `json:"args,omitempty" description:"Additional arguments to pass to K0s."`
	ReplaceArgs bool              `json:"replace_args,omitempty" description:"Replace the default arguments instead of appending to them."`
}
