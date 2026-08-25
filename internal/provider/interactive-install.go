package provider

import (
	"encoding/json"

	"github.com/kairos-io/kairos/v4/sdk/bus"
	"github.com/kairos-io/kairos/v4/sdk/utils"

	"github.com/mudler/edgevpn/pkg/node"
	"github.com/mudler/go-pluggable"
)

func InteractiveInstall(e *pluggable.Event) pluggable.EventResponse { //nolint:revive
	prompts := []bus.YAMLPrompt{
		{
			YAMLSection: "p2p.network_token",
			Prompt:      "Insert a network token, leave empty to autogenerate",
			AskFirst:    true,
			AskPrompt:   "Do you want to setup a full mesh-support?",
			IfEmpty:     node.GenerateNewConnectionData().Base64(),
		},
	}

	// Check which Kubernetes binary is installed
	if utils.K3sBin() != "" {
		prompts = append(prompts, bus.YAMLPrompt{
			YAMLSection: "k3s.enabled",
			Bool:        true,
			Prompt:      "Do you want to enable k3s?",
		})
	} else if utils.K0sBin() != "" {
		prompts = append(prompts, bus.YAMLPrompt{
			YAMLSection: "k0s.enabled",
			Bool:        true,
			Prompt:      "Do you want to enable k0s?",
		})
	}

	payload, err := json.Marshal(prompts)
	if err != nil {
		return ErrorEvent("Failed marshalling JSON input: %s", err.Error())
	}

	return pluggable.EventResponse{
		State: "",
		Data:  string(payload),
		Error: "",
	}
}
