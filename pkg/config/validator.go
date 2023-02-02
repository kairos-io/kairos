package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	jsonschemago "github.com/swaggest/jsonschema-go"
	"gopkg.in/yaml.v3"
)

type Schema struct {
	Users []User   `json:"users,omitempty" minItems:"1" required:"true"`
	P2P   P2P      `json:"p2p,omitempty"`
	_     struct{} `title:"Kairos Schema" description:"Defines all valid Kairos configuration attributes."`
}

type P2P struct {
	DisableDHT bool `json:"disable_dht,omitempty" default:"true"`
	NetworkTokenControlFlow
}

type NetworkTokenControlFlow struct{}

type EmptyNetworkToken struct {
	NetworkToken string `json:"network_token" const:""`
}

type PresentNetworkToken struct {
	NetworkToken string `json:"network_token,omitempty" requried:"true" minLength:"1"`
}

type DisabledAuto struct {
	Auto struct {
		Enable bool `json:"enable,omitempty" const:"false"`
	} `json:"auto"`
}

func (NetworkTokenControlFlow) JSONSchemaIf() interface{} {
	return DisabledAuto{}
}

func (NetworkTokenControlFlow) JSONSchemaThen() interface{} {
	return EmptyNetworkToken{}
}

func (NetworkTokenControlFlow) JSONSchemaElse() interface{} {
	return PresentNetworkToken{}
}

type User struct {
	Name              string   `json:"name,omitempty" pattern:"([a-z_][a-z0-9_]{0,30})" required:"true" example:"kairos"`
	Groups            string   `json:"groups,omitempty" example:"admin"`
	LockPasswd        bool     `json:"lockPasswd,omitempty" example:"true"`
	Passwd            string   `json:"passwd,omitempty" example:"kairos"`
	SSHAuthorizedKeys []string `json:"ssh_authorized_keys,omitempty" examples:"[\"github:USERNAME\",\"ssh-ed25519 AAAF00BA5\"]"`
}

type Validator struct {
	Data         string
	Header       string
	yamlError    error
	schemaError  error
	parsedConfig interface{}
}

func Validate(data, header string) error {
	v := Validator{Data: data, Header: header}

	if !v.isValidYaml() {
		return v.yamlError
	}

	if !v.isValidSchema() {
		return v.schemaError
	}

	return nil
}

func (v *Validator) Error() error {
	if v.yamlError != nil {
		return v.yamlError
	}

	if v.schemaError != nil {
		return v.schemaError
	}

	return nil
}

func (v *Validator) isValidSchema() bool {
	reflector := jsonschemago.Reflector{}

	generatedSchema, err := reflector.Reflect(Schema{})
	if err != nil {
		v.schemaError = err
		return false
	}

	generatedSchemaJson, err := json.MarshalIndent(generatedSchema, "", " ")
	if err != nil {
		v.schemaError = err
		return false
	}

	// TODO: remove
	fmt.Println("############ schema: ")
	fmt.Println(string(generatedSchemaJson))

	instance, err := json.Marshal(v.parsedConfig)
	if err != nil {
		v.schemaError = err
		return false
	}

	// TODO: remove
	fmt.Println("############ instance")
	fmt.Println(string(instance))

	sch, err := jsonschema.CompileString("schema.json", string(generatedSchemaJson))
	if err != nil {
		v.schemaError = err
		return false
	}

	if err = sch.Validate(v.parsedConfig); err != nil {
		v.schemaError = err
		return false
	}

	return true
}

func (v *Validator) isValidYaml() bool {
	if !v.hasHeader() {
		v.yamlError = fmt.Errorf("missing %s header", v.Header)
		return false
	}

	err := yaml.Unmarshal([]byte(v.Data), &v.parsedConfig)
	if err != nil {
		v.yamlError = err
		return false
	}
	fmt.Println("#### ParsedConfig:")
	fmt.Println(v.parsedConfig)

	return true
}

func (cv *Validator) hasHeader() bool {
	return strings.HasPrefix(cv.Data, cv.Header)
}
