package schema

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// JSONSchema builds a JSON Schema based on the Root Schema and the given version
// this is helpful when mapping a validation error.
func JSONSchema(version string) (string, error) {
	url := fmt.Sprintf("https://kairos.io/%s/cloud-config.json", version)
	schema, err := GenerateSchema(RootSchema{}, url)
	if err != nil {
		return "", err
	}

	return schema, nil
}

// Validate ensures that a given schema is Valid according to the Root Schema from the agent.
func Validate(source string) error {
	var yaml string

	if strings.HasPrefix(source, "http") {
		resp, err := http.Get(source)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		//Convert the body to type string
		yaml = string(body)
	} else {
		// Maybe we should just try to read the string for the normal headers? That would identify a full yaml vs a file
		dat, err := os.ReadFile(source)
		if err != nil {
			if strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "file name too long") {
				yaml = source
			} else {
				return err
			}
		} else {
			yaml = string(dat)
		}
	}

	config, err := NewConfigFromYAML(yaml, RootSchema{})
	if err != nil {
		return err
	}

	if !config.HasHeader() {
		return fmt.Errorf("missing #cloud-config header")
	}

	if config.IsValid() {
		return nil
	}

	err = config.ValidationError
	if err != nil {
		return err
	}

	return nil
}
