package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	jsonschemago "github.com/swaggest/jsonschema-go"
	"gopkg.in/yaml.v3"
)

type FullConfig struct {
	Users []User   `json:"users,omitempty" minimum:"1" required:"true"`
	_     struct{} `title:"Kairos Config" description:"Defines all valid Kairos configuration attributes."`
}

type User struct {
	Name              string   `json:"name,omitempty" pattern:"([a-z_][a-z0-9_]{0,30})" required:"true" example:"kairos"`
	Passwd            string   `json:"passwd,omitempty" example:"kairos"`
	Groups            string   `json:"groups,omitempty" example:"admin"`
	LockPasswd        bool     `json:"lockPasswd,omitempty" example:"true"`
	SSHAuthorizedKeys []string `json:"sshAuthorizedKeys,omitempty" example:"github:mudler"`
}

type Validator struct {
	Data         string
	Header       string
	yamlError    error
	schemaError  error
	parsedConfig FullConfig
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

	generatedSchema, err := reflector.Reflect(v.parsedConfig)
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

	var u interface{}
	if err := json.Unmarshal([]byte(instance), &u); err != nil {
		v.schemaError = err
		return false
	}

	if err = sch.Validate(u); err != nil {
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

	return true
}

func (cv *Validator) hasHeader() bool {
	return strings.HasPrefix(cv.Data, cv.Header)
}
