package agent

import (
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	config "github.com/kairos-io/kairos/pkg/config"
	schema "github.com/kairos-io/kairos/pkg/config/schemas"
)

func Validate(file string) error {
	var yaml string

	if strings.HasPrefix(file, "http") {
		resp, err := http.Get(file)
		if err != nil {
			return err
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		//Convert the body to type string
		yaml = string(body)
	} else {
		dat, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		yaml = string(dat)
	}

	config, err := schema.NewConfigFromYAML(yaml, config.DefaultHeader, schema.RootSchema{})
	if err != nil {
		return err
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
