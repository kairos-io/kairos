package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	jsonschemago "github.com/swaggest/jsonschema-go"
	"gopkg.in/yaml.v3"
)

// RootSchema groups all the different schemas of the Kairos configuration together.
type RootSchema struct {
	_                  struct{}       `title:"Kairos Schema" description:"Defines all valid Kairos configuration attributes."`
	Bundles            []BundleSchema `json:"bundles,omitempty" description:"Add bundles in runtime"`
	ConfigURL          string         `json:"config_url,omitempty" description:"URL download configuration from."`
	Env                []string       `json:"env,omitempty"`
	FailOnBundleErrors bool           `json:"fail_on_bundles_errors,omitempty"`
	GrubOptionsSchema  `json:"grub_options,omitempty"`
	Install            InstallSchema `json:"install,omitempty"`
	Options            []interface{} `json:"options,omitempty" description:"Various options."`
	Users              []UserSchema  `json:"users,omitempty" minItems:"1" required:"true"`
	P2P                P2PSchema     `json:"p2p,omitempty"`
}

// KConfig is used to parse and validate Kairos configuration files
type KConfig struct {
	source          string
	parsed          interface{}
	validationError error
	schemaType      interface{}
	header          string
}

func (kc *KConfig) validate() {
	reflector := jsonschemago.Reflector{}

	generatedSchema, err := reflector.Reflect(kc.schemaType)
	if err != nil {
		kc.validationError = err
	}

	generatedSchemaJSON, err := json.MarshalIndent(generatedSchema, "", " ")
	if err != nil {
		kc.validationError = err
	}

	sch, err := jsonschema.CompileString("schema.json", string(generatedSchemaJSON))
	if err != nil {
		kc.validationError = err
	}

	if err = sch.Validate(kc.parsed); err != nil {
		kc.validationError = err
	}
}

// IsValid returns true if the schema rules of the configuration are valid.
func (kc *KConfig) IsValid() bool {
	kc.validate()

	return kc.validationError == nil
}

// ValidationError returns one of the errors of an invalid schemam rule, when the configuration is valid, then it returns an empty string.
func (kc *KConfig) ValidationError() string {
	kc.validate()

	if kc.validationError != nil {
		return kc.validationError.Error()
	}

	return ""
}

func (kc *KConfig) hasHeader() bool {
	return strings.HasPrefix(kc.source, kc.header)
}

// NewConfigFromYAML is a constructor for KConfig instances. The source of the configuration is passed in YAML and if there are any issues unmarshaling it will return an error.
func NewConfigFromYAML(s, h string, st interface{}) (*KConfig, error) {
	kc := &KConfig{
		source:     s,
		header:     h,
		schemaType: st,
	}

	if !kc.hasHeader() {
		return kc, fmt.Errorf("missing %s header", kc.header)
	}

	err := yaml.Unmarshal([]byte(s), &kc.parsed)
	if err != nil {
		return kc, err
	}
	return kc, nil
}
