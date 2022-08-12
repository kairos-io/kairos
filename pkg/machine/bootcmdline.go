package machine

import (
	"io/ioutil"
	"strings"

	"github.com/c3os-io/c3os/sdk/unstructured"
	"github.com/google/shlex"
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

	return unstructured.ToYAML(v)
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
