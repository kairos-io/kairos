package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type FullConfig struct {
}

type Validator struct {
	data      string
	header    string
	yamlError error
}

func Validate(data, header string) error {
	v := Validator{data: data, header: header}

	// First we check that we receive a YAML with valid syntax
	if !v.isValidYaml() {
		return v.yamlError
	}

	// Then we check if the schema/struct/grammar is correct
	// cv.isValidSchema()

	return nil
}

func (cv *Validator) isValidYaml() bool {
	if !cv.hasHeader() {
		cv.yamlError = fmt.Errorf("missing %s header", cv.header)
		return false
	}

	fc := FullConfig{}
	err := yaml.Unmarshal([]byte(cv.data), &fc)
	if err != nil {
		cv.yamlError = err
		return false
	}

	return true
}

func (cv *Validator) hasHeader() bool {
	return strings.HasPrefix(cv.data, cv.header)
}
