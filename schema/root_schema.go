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
	_                         struct{}       `title:"Kairos Schema" description:"Defines all valid Kairos configuration attributes."`
	Bundles                   []BundleSchema `json:"bundles,omitempty" description:"Add bundles in runtime"`
	ConfigURL                 string         `json:"config_url,omitempty" description:"URL download configuration from."`
	Env                       []string       `json:"env,omitempty"`
	FailOnBundleErrors        bool           `json:"fail_on_bundles_errors,omitempty"`
	GrubOptionsSchema         `json:"grub_options,omitempty"`
	Install                   InstallSchema            `json:"install,omitempty"`
	Options                   []interface{}            `json:"options,omitempty" description:"Various options."`
	Users                     []UserSchema             `json:"users,omitempty" minItems:"1" required:"true"`
	P2P                       P2PSchema                `json:"p2p,omitempty"`
	Debug                     bool                     `json:"debug,omitempty" mapstructure:"debug"`
	Strict                    bool                     `json:"strict,omitempty" mapstructure:"strict"`
	CloudInitPaths            []string                 `json:"cloud-init-paths,omitempty" mapstructure:"cloud-init-paths"`
	EjectCD                   bool                     `json:"eject-cd,omitempty" mapstructure:"eject-cd"`
	FullCloudConfig           string                   `json:"fullcloudconfig,omitempty" mapstructure:"fullcloudconfig"`
	Cosign                    bool                     `json:"cosign,omitempty" mapstructure:"cosign"`
	Verify                    bool                     `json:"verify,omitempty" mapstructure:"verify"`
	CosignPubKey              string                   `json:"cosign-key,omitempty" mapstructure:"cosign-key"`
	Arch                      string                   `json:"arch,omitempty" mapstructure:"arch"`
	Platform                  PlatformSchema           `json:"platform,omitempty" mapstructure:"platform"`
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

type PlatformSchema struct {
	OS         string
	Arch       string
	GolangArch string
}

// KConfig is used to parse and validate Kairos configuration files.
type KConfig struct {
	Source          string
	parsed          interface{}
	ValidationError error
	schemaType      interface{}
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
