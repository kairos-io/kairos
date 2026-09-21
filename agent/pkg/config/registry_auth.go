package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/imageextractor"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	registrytypes "github.com/moby/moby/api/types/registry"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
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
	auth, err := readRegistryAuth(cfg, sub.Get("registry-auth"), subkey)
	return sub.GetBool("allow-insecure-registries"), auth, err
}

func readRegistryAuth(cfg *sdkConfig.Config, raw interface{}, subkey string) (*registrytypes.AuthConfig, error) {
	values, _ := raw.(map[string]interface{})
	file, hasFile := values["file"]
	if !hasFile {
		return parseRegistryAuth(raw, subkey)
	}
	path, ok := file.(string)
	if !ok || path == "" || len(values) != 1 {
		return nil, fmt.Errorf("%s.registry-auth.file must be a non-empty path and cannot be combined with other fields", subkey)
	}
	return readRegistryAuthFile(cfg, path, subkey)
}

func readRegistryAuthFile(cfg *sdkConfig.Config, path, subkey string) (*registrytypes.AuthConfig, error) {
	data, err := cfg.Fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s.registry-auth.file cannot be read: %w", subkey, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var credentials, extra interface{}
	if decoder.Decode(&credentials) != nil || decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("%s.registry-auth.file must contain one valid YAML credential object", subkey)
	}
	// Parse only credential fields, so files cannot reference another file.
	auth, err := parseRegistryAuth(credentials, subkey)
	if err != nil {
		return nil, err
	}
	if auth == nil {
		return nil, fmt.Errorf("%s.registry-auth.file must contain credentials", subkey)
	}
	return auth, nil
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
	fields, err := registryAuthFields(values, subkey)
	if err != nil {
		return nil, err
	}
	username, hasUsername := fields["username"]
	password, hasPassword := fields["password"]
	auth, hasAuth := fields["auth"]
	identity, hasIdentity := fields["identity-token"]
	token, hasToken := fields["registry-token"]
	if hasUsername != hasPassword {
		return nil, fmt.Errorf("%s.registry-auth requires both username and password", subkey)
	}
	if countTrue(hasUsername, hasAuth, hasIdentity, hasToken) > 1 {
		return nil, fmt.Errorf("%s.registry-auth accepts exactly one credential form: username/password, auth, identity-token, or registry-token", subkey)
	}
	switch {
	case hasUsername:
		return &registrytypes.AuthConfig{Username: username, Password: password}, nil
	case hasAuth:
		return decodeBasicAuth(auth, subkey)
	case hasIdentity:
		return &registrytypes.AuthConfig{IdentityToken: identity}, nil
	case hasToken:
		return &registrytypes.AuthConfig{RegistryToken: token}, nil
	default:
		return nil, fmt.Errorf("%s.registry-auth must contain a supported credential form", subkey)
	}
}

// registryAuthFields validates field names and values without echoing secrets.
func registryAuthFields(values map[string]interface{}, subkey string) (map[string]string, error) {
	fields := make(map[string]string, len(values))
	for key, value := range values {
		switch key {
		case "username", "password", "auth", "identity-token", "registry-token":
		default:
			return nil, fmt.Errorf("%s.registry-auth contains an unsupported field", subkey)
		}
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s.registry-auth.%s must be a string", subkey, key)
		}
		if text == "" && key != "password" && key != "auth" {
			return nil, fmt.Errorf("%s.registry-auth.%s must not be empty", subkey, key)
		}
		fields[key] = text
	}
	return fields, nil
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
