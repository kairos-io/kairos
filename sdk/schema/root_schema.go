package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	jsonschemago "github.com/swaggest/jsonschema-go"
	"gopkg.in/yaml.v3"
)

// RootSchema groups all the different schema of the Kairos configuration together.
type RootSchema struct {
	_                  struct{}         `title:"Kairos Schema" description:"Defines all valid Kairos configuration attributes."`
	Bundles            []BundleSchema   `json:"bundles,omitempty" description:"Add bundles in runtime"`
	ConfigURL          string           `json:"config_url,omitempty" description:"URL download configuration from."`
	Env                []string         `json:"env,omitempty"`
	Extensions         ExtensionsSchema `json:"extensions,omitempty"`
	FailOnBundleErrors bool             `json:"fail_on_bundles_errors,omitempty"`
	GrubOptionsSchema  `json:"grub_options,omitempty"`
	Install            InstallSchema `json:"install,omitempty"`
	Kcrypt             KcryptSchema  `json:"kcrypt,omitempty"`
	Options            []interface{} `json:"options,omitempty" description:"Various options."`
	// Users is not required. An admin user may instead be defined in a yip
	// stage (stages.<stage>[].users) or waived with install.nousers, and the
	// agent enforces that at install time (see CheckConfigForUsers in
	// agent/pkg/config). An explicit empty list is still rejected.
	Users                     []UserSchema             `json:"users,omitempty" minItems:"1"`
	P2P                       P2PSchema                `json:"p2p,omitempty"`
	Debug                     bool                     `json:"debug,omitempty" mapstructure:"debug"`
	Strict                    bool                     `json:"strict,omitempty" mapstructure:"strict"`
	CloudInitPaths            []string                 `json:"cloud-init-paths,omitempty" mapstructure:"cloud-init-paths"`
	EjectCD                   bool                     `json:"eject-cd,omitempty" mapstructure:"eject-cd"`
	Cosign                    bool                     `json:"cosign,omitempty" mapstructure:"cosign"`
	Verify                    bool                     `json:"verify,omitempty" mapstructure:"verify"`
	CosignPubKey              string                   `json:"cosign-key,omitempty" mapstructure:"cosign-key"`
	Arch                      string                   `json:"arch,omitempty" mapstructure:"arch"`
	SquashFsCompressionConfig []string                 `json:"squash-compression,omitempty" mapstructure:"squash-compression"`
	SquashFsNoCompression     bool                     `json:"squash-no-compression,omitempty" mapstructure:"squash-no-compression"`
	UkiMaxEntries             int                      `json:"uki-max-entries,omitempty" mapstructure:"uki-max-entries"`
	Stages                    map[string][]StageSchema `json:"stages,omitempty" description:"Cloud-init stages to execute"`
}

// StageSchema defines the stage fields validated by the Kairos configuration schema.
// Other yip stage fields remain accepted as additional properties.
type StageSchema struct {
	Commands []string `json:"commands,omitempty" description:"Commands to execute"`
}

// KConfig is used to parse and validate Kairos configuration files.
type KConfig struct {
	Source          string
	parsed          interface{}
	ValidationError error
	schemaType      interface{}
}

// MetaSchemaDraft07 is the JSON Schema dialect the generated documents are
// written in. The reflector emits draft-07 keywords, "definitions" rather than
// "$defs", so a consumer has to read the document as draft-07 to resolve the
// references in it.
const MetaSchemaDraft07 = "http://json-schema.org/draft-07/schema#"

// GenerateSchema takes the given schema type and builds a JSON Schema out of it.
// If an id is passed it is written to the $id key, which names the document, and
// $schema is set to the dialect the document is written in. The $schema key
// declares a dialect, not a location, so the id does not belong in it: a
// validator that honours $schema tries to fetch its value as a meta-schema.
//
// Both keys are left out when id is empty. That is the form the in-process
// validator compiles, so its dialect is unchanged by this.
func GenerateSchema(schemaType interface{}, id string) (string, error) {
	reflector := jsonschemago.Reflector{}

	generatedSchema, err := reflector.Reflect(schemaType)
	if err != nil {
		return "", err
	}
	if id != "" {
		generatedSchema.WithID(id)
		generatedSchema.WithSchema(MetaSchemaDraft07)
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

	sch, err := jsonschema.CompileString(InProcessSchemaID, string(generatedSchemaJSON))
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

// ValidateSemantics runs cross-field checks that JSON Schema cannot express
// on its own. Returns a list of warnings that callers should surface to the
// user, and an error for constraints that would leave the system in an
// unusable state (e.g. ssh_hardening: true with no ssh_authorized_keys).
//
// Only meaningful when kc.Source was parsed against RootSchema.
func (kc *KConfig) ValidateSemantics() ([]string, error) {
	// Route through JSON so the existing `json:"..."` tags on RootSchema
	// (and its nested types) do the field mapping. yaml.v3 alone would
	// lowercase the Go field name and miss snake_case keys such as
	// `ssh_hardening` or `ssh_authorized_keys`.
	//
	// Type-level mismatches are IsValid()'s job to surface; if the
	// config cannot even be shaped into a RootSchema we return no
	// findings and let JSON Schema validation report the actual error.
	jsonBytes, err := json.Marshal(kc.parsed)
	if err != nil {
		return nil, nil
	}
	var root RootSchema
	if err := json.Unmarshal(jsonBytes, &root); err != nil {
		return nil, nil
	}

	var warnings []string

	if root.Install.SSHHardening {
		haveKey := false
		var usersWithPasswd []string
		for _, u := range root.Users {
			if len(u.SSHAuthorizedKeys) > 0 {
				haveKey = true
			}
			if u.Passwd != "" {
				usersWithPasswd = append(usersWithPasswd, u.Name)
			}
		}
		if !haveKey {
			return warnings, fmt.Errorf(
				"install.ssh_hardening: true requires at least one user with ssh_authorized_keys; " +
					"otherwise the installed system would be unreachable over SSH")
		}
		for _, name := range usersWithPasswd {
			warnings = append(warnings, fmt.Sprintf(
				"install.ssh_hardening: true disables password authentication; the passwd set on user %q "+
					"will be unused over SSH (remove it if intentional, or drop install.ssh_hardening if not)",
				name))
		}
	}

	return warnings, nil
}
