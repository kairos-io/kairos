package action

import (
	"bytes"
	"os"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/kairos-io/kairos-sdk/state"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"gopkg.in/yaml.v3"
)

func RenderTemplate(path string, config *sdkConfig.Config, runtime state.Runtime) ([]byte, error) {
	// Marshal runtime to YAML then to Map so that it is consistent with the output of 'kairos-agent state'
	var runtimeMap map[string]interface{}
	err := yaml.Unmarshal([]byte(runtime.String()), &runtimeMap)
	if err != nil {
		return nil, err
	}

	tpl, err := loadTemplateFile(path)
	if err != nil {
		return nil, err
	}

	result := new(bytes.Buffer)
	err = tpl.Execute(result, map[string]interface{}{
		"Config": config.Collector.Values,
		"State":  runtimeMap,
	})
	if err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

func loadTemplateFile(path string) (*template.Template, error) {
	tpl := template.New(path).Funcs(sprig.FuncMap())
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return tpl.Parse(string(content))
}
