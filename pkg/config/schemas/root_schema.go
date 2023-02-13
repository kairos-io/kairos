package config

import (
	"encoding/json"
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

// KConfig is used to parse and validate Kairos configuration files.
type KConfig struct {
	Source          string
	parsed          interface{}
	ValidationError error
	schemaType      interface{}
}

func GenerateSchema(schemaType interface{}, url string) (string, error) {
	reflector := jsonschemago.Reflector{}

	generatedSchema, err := reflector.Reflect(schemaType)
	if err != nil {
		return "", err
	}
	if url != "" {
		generatedSchema.WithSchema(url)
	}

	generatedSchemaJSON, err := json.MarshalIndent(generatedSchema, "", " ")
	if err != nil {
		return "", err
	}

	return string(generatedSchemaJSON), nil
}

func (kc *KConfig) validate() {
	generatedSchemaJSON, err := GenerateSchema(kc.schemaType, "")
	if err != nil {
		kc.ValidationError = err
		return
	}

	sch, err := jsonschema.CompileString("schema.json", string(generatedSchemaJSON))
	if err != nil {
		kc.ValidationError = err
		return
	}

	if err = sch.Validate(kc.parsed); err != nil {
		kc.ValidationError = err
	}
}

// IsValid returns true if the schema rules of the configuration are valid.
func (kc *KConfig) IsValid() bool {
	kc.validate()

	return kc.ValidationError == nil
}

func (kc *KConfig) HasHeader() bool {
	var found bool

	availableHeaders := []string{"#cloud-config", "#kairos-config", "#node-config"}
	for _, header := range availableHeaders {
		if strings.HasPrefix(kc.Source, header) {
			found = true
		}
	}
	return found
}

// NewConfigFromYAML is a constructor for KConfig instances. The source of the configuration is passed in YAML and if there are any issues unmarshaling it will return an error.
func NewConfigFromYAML(s string, st interface{}) (*KConfig, error) {
	kc := &KConfig{
		Source:     s,
		schemaType: st,
	}

	err := yaml.Unmarshal([]byte(s), &kc.parsed)
	if err != nil {
		return kc, err
	}
	return kc, nil
}
