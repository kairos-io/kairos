package machine

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/google/shlex"
	"github.com/hashicorp/go-multierror"
	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v2"
)

func DotToYAML(file string) ([]byte, error) {
	if file == "" {
		file = "/proc/cmdline"
	}
	dat, err := ioutil.ReadFile(file)
	if err != nil {
		return []byte{}, err
	}

	v := stringToMap(string(dat))

	return dotToYAML(v)
}

func stringToMap(s string) map[string]interface{} {
	v := map[string]interface{}{}

	splitted, _ := shlex.Split(s)
	for _, item := range splitted {
		parts := strings.SplitN(item, "=", 2)
		value := "true"
		if len(parts) > 1 {
			value = strings.Trim(parts[1], `"`)
		}
		key := strings.Trim(parts[0], `"`)
		v[key] = value
	}

	return v
}
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

func dotToYAML(v map[string]interface{}) ([]byte, error) {
	data := map[string]interface{}{}
	var errs error

	for k, value := range v {
		newData, err := jq(fmt.Sprintf(".%s=\"%s\"", k, value), data)
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
	return out, err
}
