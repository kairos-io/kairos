package unstructured

import (
	"errors"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v3"
)

func jq(command string, data map[string]interface{}) (map[string]interface{}, error) {
	query, err := gojq.Parse(command)
	if err != nil {
		return nil, err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return nil, err
	}
	iter := code.Run(data)

	v, ok := iter.Next()
	if !ok {
		return nil, errors.New("failed getting rsult from gojq")
	}
	if err, ok := v.(error); ok {
		return nil, err
	}
	if t, ok := v.(map[string]interface{}); ok {
		return t, nil
	}

	return make(map[string]interface{}), nil
}

func ToYAML(v map[string]interface{}) ([]byte, error) {
	data := map[string]interface{}{}
	var errs error

	for k, value := range v {
		tmpl := ".%s=\"%s\""
		// support boolean types
		if value == "true" || value == "false" {
			tmpl = ".%s=%s"
		}
		newData, err := jq(fmt.Sprintf(tmpl, k, value), data)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		data = newData
	}

	out, err := yaml.Marshal(&data)
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	return out, errs
}

// ToYAMLMap turns a map string interface which describes a yaml file in 'dot.yaml' format to a fully deep marshalled yaml.
func ToYAMLMap(v map[string]interface{}) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	tempData, err := ToYAML(v)
	if err != nil {
		return result, err
	}
	err = yaml.Unmarshal(tempData, &result)

	return result, err
}
