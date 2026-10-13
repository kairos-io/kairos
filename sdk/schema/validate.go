package schema

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// InProcessSchemaID is the identifier of the cloud config schema document the
// in-process validator compiles. That document carries no version, so it has
// no $id and cannot be named by SchemaID.
//
// It has to be an absolute URI. jsonschema.CompileString reads its first
// argument as the resource's URL and resolves a relative one against the
// working directory, which put the directory the process happened to run in
// into the text of every validation error. Like SchemaID, this names the
// document and is not a location to fetch.
const InProcessSchemaID = "https://kairos.io/cloud-config.json"

// SchemaID returns the identifier of the cloud config schema document for a
// given Kairos version. It names the document, it is not a location to fetch:
// nothing has been published under https://kairos.io/ since v2.0.1, so the
// address does not resolve for any supported release.
func SchemaID(version string) string {
	return fmt.Sprintf("https://kairos.io/%s/cloud-config.json", version)
}

// JSONSchema builds a JSON Schema based on the Root Schema and the given version
// this is helpful when mapping a validation error.
func JSONSchema(version string) (string, error) {
	schema, err := GenerateSchema(RootSchema{}, SchemaID(version))
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
		defer resp.Body.Close()
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
