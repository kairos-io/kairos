package provider

import (
	"encoding/json"

	providerConfig "github.com/c3os-io/c3os/internal/provider/config"
	"github.com/c3os-io/c3os/pkg/bus"
	"github.com/c3os-io/c3os/pkg/config"

	"github.com/mudler/go-nodepair"
	"github.com/mudler/go-pluggable"
)

func Challenge(e *pluggable.Event) pluggable.EventResponse {
	p := &bus.EventPayload{}
	err := json.Unmarshal([]byte(e.Data), p)
	if err != nil {
		return ErrorEvent("Failed reading JSON input: %s input '%s'", err.Error(), e.Data)
	}

	cfg := &providerConfig.Config{}
	err = config.FromString(p.Config, cfg)
	if err != nil {
		return ErrorEvent("Failed reading JSON input: %s input '%s'", err.Error(), p.Config)
	}

	tk := ""
	if cfg.C3OS != nil && cfg.C3OS.NetworkToken != "" {
		tk = cfg.C3OS.NetworkToken
	}
	if tk == "" {
		tk = nodepair.GenerateToken()
	}
	return pluggable.EventResponse{
		Data: tk,
	}
}
