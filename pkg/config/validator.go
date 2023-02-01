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
	Users []User `json:"users" minimum:"1"`
}

type User struct {
	Name   string `json:"name" pattern:"([a-z_][a-z0-9_]{0,30})" required:"true"`
	Passwd string `json:"passwd" pattern:"[abc]"`
}

type Validator struct {
	data        string
	header      string
	yamlError   error
	schemaError error
	fullConfig  FullConfig
}

func Validate(data, header string) error {
	v := Validator{data: data, header: header}

	// First we check that we receive a YAML with valid syntax
	if !v.isValidYaml() {
		return v.yamlError
	}
	// Then we check if the schema/struct/grammar is correct
	if !v.isValidSchema() {
		return v.schemaError
	}

	return nil
}

func (v *Validator) isValidSchema() bool {
	reflector := jsonschemago.Reflector{}

	fmt.Printf("########  %#v", v.fullConfig.Users)

	schema, err := reflector.Reflect(v.fullConfig)
	if err != nil {
		v.schemaError = err
		return false
	}

	j, err := json.MarshalIndent(schema, "", " ")
	if err != nil {
		v.schemaError = err
		return false
	}

	s := string(j)

	instance, err := json.Marshal(v.fullConfig)
	if err != nil {
		v.schemaError = err
		return false
	}

	sch, err := jsonschema.CompileString("schema.json", s)
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
		v.yamlError = fmt.Errorf("missing %s header", v.header)
		return false
	}

	err := yaml.Unmarshal([]byte(v.data), &v.fullConfig)
	if err != nil {
		v.yamlError = err
		return false
	}

	return true
}

func (cv *Validator) hasHeader() bool {
	return strings.HasPrefix(cv.data, cv.header)
}
