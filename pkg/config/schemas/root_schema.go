package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/itchyny/gojq"
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

const KDefaultHeader = "#cloud-config"

// KConfig is used to parse and validate Kairos configuration files.
type KConfig struct {
	Source          string
	parsed          interface{}
	ValidationError error
	schemaType      interface{}
}

func (kc KConfig) Header() string {
	if !kc.HasHeader() {
		return KDefaultHeader

	}

	header := strings.SplitN(kc.Source, "\n", 2)[0]

	return strings.TrimRightFunc(header, unicode.IsSpace)
}

func (kc KConfig) Bundles() []BundleSchema {
	jsonString, _ := json.Marshal(kc.Data()["bundles"])
	bundles := []BundleSchema{}
	json.Unmarshal(jsonString, &bundles)

	return bundles
}

func (kc KConfig) Options(key string) interface{} {
	options := kc.Data()["options"]

	return options.(map[string]interface{})[key]
}

func (kc KConfig) String() string {
	if len(kc.parsed.(map[string]interface{})) == 0 {
		dat, err := yaml.Marshal(kc)
		if err == nil {
			return fmt.Sprintf("%s\n%s", kc.Header(), string(dat))
		}
	}

	dat, _ := yaml.Marshal(kc.parsed)
	return fmt.Sprintf("%s\n%s", kc.Header(), string(dat))
}

func (kc KConfig) Unmarshal(o interface{}) error {
	return yaml.Unmarshal([]byte(kc.String()), o)
}

func (kc KConfig) Data() map[string]interface{} {
	return kc.parsed.(map[string]interface{})
}

func (kc KConfig) Query(s string) (res string, err error) {
	s = fmt.Sprintf(".%s", s)
	jsondata := map[string]interface{}{}

	// c.String() takes the original data map[string]interface{} and Marshals into YAML, then here we unmarshall it again?
	// we should be able to use c.originalData and copy it to jsondata
	err = yaml.Unmarshal([]byte(kc.Source), &jsondata)
	if err != nil {
		return
	}
	query, err := gojq.Parse(s)
	if err != nil {
		return res, err
	}

	iter := query.Run(jsondata) // or query.RunWithContext
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return res, fmt.Errorf("failed parsing, error: %w", err)
		}

		dat, err := yaml.Marshal(v)
		if err != nil {
			break
		}
		res += string(dat)
	}
	return
}

// GenerateSchema takes the given schema type and builds a JSON Schema out of it
// if a URL is passed it will also add it as the $schema key, which is useful when
// defining a version of a Root Schema which will be available online.
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

// HasHeader returns true if the config has one of the valid headers.
func (kc *KConfig) HasHeader() bool {
	var found bool

	availableHeaders := []string{KDefaultHeader, "#kairos-config", "#node-config"}
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
