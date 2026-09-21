package config

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/imageextractor"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	registrytypes "github.com/moby/moby/api/types/registry"
	"github.com/spf13/viper"
)

// applyRegistryOptions runs before image sizing and extraction. Update the
// default extractor without discarding options supplied by its caller.
func applyRegistryOptions(cfg *sdkConfig.Config, subkey string) error {
	allow, auth, err := readRegistryOptions(cfg, subkey)
	if err != nil {
		return err
	}
	if extractor, ok := cfg.ImageExtractor.(imageextractor.OCIImageExtractor); ok {
		if allow {
			cfg.Logger.Infof("Allowing insecure registry pulls via %s.allow-insecure-registries", subkey)
			extractor.Insecure = true
		}
		if auth != nil {
			extractor.Auth = auth
		}
		cfg.ImageExtractor = extractor
	}
	return nil
}

func readRegistryOptions(cfg *sdkConfig.Config, subkey string) (bool, *registrytypes.AuthConfig, error) {
	if err := validateRegistryAuthKeys(cfg.Collector.Values); err != nil {
		return false, nil, err
	}
	ccString, err := cfg.Collector.String()
	if err != nil {
		return false, nil, err
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(ccString)); err != nil {
		return false, nil, err
	}
	sub := v.Sub(subkey)
	if sub == nil {
		return false, nil, nil
	}
	auth, err := parseRegistryAuth(sub.Get("registry-auth"), subkey)
	return sub.GetBool("allow-insecure-registries"), auth, err
}

// validateRegistryAuthKeys rejects authentication-looking keys at the direct
// operation level unless they use the canonical registry-auth spelling.
func validateRegistryAuthKeys(values collector.ConfigValues) error {
	for operation, raw := range values {
		canonicalOperation := ""
		switch {
		case strings.EqualFold(operation, "install"):
			canonicalOperation = "install"
		case strings.EqualFold(operation, "upgrade"):
			canonicalOperation = "upgrade"
		default:
			continue
		}

		var operationValues map[string]interface{}
		switch typed := raw.(type) {
		case collector.ConfigValues:
			operationValues = map[string]interface{}(typed)
		case map[string]interface{}:
			operationValues = typed
		default:
			continue
		}
		for key := range operationValues {
			if strings.Contains(strings.ToLower(key), "auth") && key != "registry-auth" {
				return fmt.Errorf("%s operation contains an unsupported authentication key; use registry-auth", canonicalOperation)
			}
		}
	}
	return nil
}

func parseRegistryAuth(raw interface{}, subkey string) (*registrytypes.AuthConfig, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s.registry-auth must be an object", subkey)
	}
	if len(values) == 0 {
		return nil, nil
	}
	for key := range values {
		switch key {
		case "username", "password", "auth", "identity-token", "registry-token":
		default:
			return nil, fmt.Errorf("%s.registry-auth contains an unsupported field", subkey)
		}
	}
	username, hasUsername, err := authString(values, "username", subkey)
	if err != nil {
		return nil, err
	}
	password, hasPassword, err := authString(values, "password", subkey)
	if err != nil {
		return nil, err
	}
	auth, hasAuth, err := authString(values, "auth", subkey)
	if err != nil {
		return nil, err
	}
	identity, hasIdentity, err := authString(values, "identity-token", subkey)
	if err != nil {
		return nil, err
	}
	token, hasToken, err := authString(values, "registry-token", subkey)
	if err != nil {
		return nil, err
	}
	if hasUsername != hasPassword {
		return nil, fmt.Errorf("%s.registry-auth requires both username and password", subkey)
	}
	if countTrue(hasUsername, hasAuth, hasIdentity, hasToken) > 1 {
		return nil, fmt.Errorf("%s.registry-auth accepts exactly one credential form: username/password, auth, identity-token, or registry-token", subkey)
	}
	if hasUsername {
		if username == "" {
			return nil, fmt.Errorf("%s.registry-auth.username must not be empty", subkey)
		}
		return &registrytypes.AuthConfig{Username: username, Password: password}, nil
	}
	if hasAuth {
		return decodeBasicAuth(auth, subkey)
	}
	if hasIdentity {
		if identity == "" {
			return nil, fmt.Errorf("%s.registry-auth.identity-token must not be empty", subkey)
		}
		return &registrytypes.AuthConfig{IdentityToken: identity}, nil
	}
	if hasToken {
		if token == "" {
			return nil, fmt.Errorf("%s.registry-auth.registry-token must not be empty", subkey)
		}
		return &registrytypes.AuthConfig{RegistryToken: token}, nil
	}
	return nil, fmt.Errorf("%s.registry-auth must contain a supported credential form", subkey)
}

func authString(values map[string]interface{}, name, subkey string) (string, bool, error) {
	value, exists := values[name]
	if !exists {
		return "", false, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", true, fmt.Errorf("%s.registry-auth.%s must be a string", subkey, name)
	}
	return text, true, nil
}

func countTrue(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func decodeBasicAuth(value, subkey string) (*registrytypes.AuthConfig, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%s.registry-auth.auth must be base64 username:password", subkey)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return nil, fmt.Errorf("%s.registry-auth.auth must decode to username:password", subkey)
	}
	return &registrytypes.AuthConfig{Username: parts[0], Password: parts[1]}, nil
}
