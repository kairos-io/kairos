package machine

import (
	"os"
	"strings"

	"github.com/google/shlex"
	"github.com/kairos-io/kairos-sdk/unstructured"
)

func DotToYAML(file string) ([]byte, error) {
	if file == "" {
		file = "/proc/cmdline"
	}
	dat, err := os.ReadFile(file)
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
