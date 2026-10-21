package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/imageextractor"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	registrytypes "github.com/moby/moby/api/types/registry"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Pointer fields distinguish omitted credentials from explicitly empty values.
type registryAuth struct {
	File          *string `mapstructure:"file"`
	Username      *string `mapstructure:"username"`
	Password      *string `mapstructure:"password"`
	Auth          *string `mapstructure:"auth"`
	IdentityToken *string `mapstructure:"identity-token"`
	RegistryToken *string `mapstructure:"registry-token"`
}

// decodeRegistryAuth keeps strict type checks at the configuration boundary.
// Decoder errors can include secret values, so return only a fixed diagnostic.
func decodeRegistryAuth(raw interface{}, subkey string) (*registryAuth, error) {
	var fields registryAuth
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:      &fields,
		ErrorUnused: true,
		DecodeNil:   true,
		MatchName:   func(key, field string) bool { return key == field },
		DecodeHook: mapstructure.DecodeHookFuncType(func(from, to reflect.Type, value interface{}) (interface{}, error) {
			if to == reflect.TypeOf((*string)(nil)) && (from == nil || from.Kind() != reflect.String) {
				return nil, fmt.Errorf("credential fields must be strings")
			}
			return value, nil
		}),
	})
	if err != nil {
		return nil, err
	}
	if err := decoder.Decode(raw); err != nil {
		return nil, fmt.Errorf("%s.registry-auth must be an object containing only supported lowercase string fields", subkey)
	}
	return &fields, nil
}

// applyRegistryOptions runs before image sizing and extraction. Update the
// default extractor without discarding options supplied by its caller.
func applyRegistryOptions(cfg *sdkConfig.Config, subkey string, source *sdkImages.ImageSource) error {
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
			// A local source has no registry to authorize. Never leave these
			// credentials unscoped on the shared extractor.
			extractor.Auth = nil
			extractor.AuthRegistry = ""
			if source != nil && source.IsDocker() {
				ref, err := name.ParseReference(source.Value())
				if err != nil {
					return fmt.Errorf("%s registry authentication requires a valid source image", subkey)
				}
				extractor.Auth = auth
				extractor.AuthRegistry = ref.Context().RegistryStr()
			}
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
	fields, err := decodeRegistryAuth(sub.Get("registry-auth"), subkey)
	if err != nil {
		return false, nil, err
	}
	auth, err := readRegistryAuth(cfg, fields, subkey)
	return sub.GetBool("allow-insecure-registries"), auth, err
}

func readRegistryAuth(cfg *sdkConfig.Config, fields *registryAuth, subkey string) (*registrytypes.AuthConfig, error) {
	if fields.File == nil {
		return parseRegistryAuth(fields, subkey)
	}
	if *fields.File == "" || fields.Username != nil || fields.Password != nil || fields.Auth != nil || fields.IdentityToken != nil || fields.RegistryToken != nil {
		return nil, fmt.Errorf("%s.registry-auth.file must be a non-empty path and cannot be combined with other fields", subkey)
	}
	return readRegistryAuthFile(cfg, *fields.File, subkey)
}

func readRegistryAuthFile(cfg *sdkConfig.Config, path, subkey string) (*registrytypes.AuthConfig, error) {
	data, err := cfg.Fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s.registry-auth.file cannot be read", subkey)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var credentials, extra interface{}
	if decoder.Decode(&credentials) != nil || decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("%s.registry-auth.file must contain one valid YAML credential object", subkey)
	}
	// Parse only credential fields, so files cannot reference another file.
	fields, err := decodeRegistryAuth(credentials, subkey)
	if err != nil {
		return nil, err
	}
	auth, err := parseRegistryAuth(fields, subkey)
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
			if strings.Contains(strings.ToLower(key), "auth") && !strings.EqualFold(key, "registry-auth") {
				return fmt.Errorf("%s operation contains an unsupported authentication key; use registry-auth", canonicalOperation)
			}
		}
	}
	return nil
}

func parseRegistryAuth(fields *registryAuth, subkey string) (*registrytypes.AuthConfig, error) {
	if fields.File != nil {
		return nil, fmt.Errorf("%s.registry-auth.file cannot reference another credential file", subkey)
	}
	if (fields.Username != nil && *fields.Username == "") ||
		(fields.IdentityToken != nil && *fields.IdentityToken == "") ||
		(fields.RegistryToken != nil && *fields.RegistryToken == "") {
		return nil, fmt.Errorf("%s.registry-auth username and tokens must not be empty", subkey)
	}
	if (fields.Username != nil) != (fields.Password != nil) {
		return nil, fmt.Errorf("%s.registry-auth requires both username and password", subkey)
	}
	if countTrue(fields.Username != nil, fields.Auth != nil, fields.IdentityToken != nil, fields.RegistryToken != nil) > 1 {
		return nil, fmt.Errorf("%s.registry-auth accepts exactly one credential form: username/password, auth, identity-token, or registry-token", subkey)
	}
	switch {
	case fields.Username != nil:
		return &registrytypes.AuthConfig{Username: *fields.Username, Password: *fields.Password}, nil
	case fields.Auth != nil:
		return decodeBasicAuth(*fields.Auth, subkey)
	case fields.IdentityToken != nil:
		return &registrytypes.AuthConfig{IdentityToken: *fields.IdentityToken}, nil
	case fields.RegistryToken != nil:
		return &registrytypes.AuthConfig{RegistryToken: *fields.RegistryToken}, nil
	default:
		return nil, nil
	}
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
