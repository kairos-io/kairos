package collector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/denisbrodbeck/machineid"
	yiputils "github.com/mudler/yip/pkg/utils"
	"github.com/zcalusic/sysinfo"
)

// RenderConfigURL renders url through Go text/template with the same
// sysinfo-derived context yip stages expose under {{ .Values.* }} (product.uuid,
// product.serial, network[0].macaddress, node.hostname, plus Random and
// ProtectedID). Sprig's TxtFuncMap is available.
//
// If url contains no template markers ("{{" or "}}"), it is returned unchanged
// with no context construction (fast path).
//
// Every scalar value the template can read is URL-escaped via url.QueryEscape
// before substitution, so a serial with spaces, "+", "&", "#" or "?" cannot
// corrupt a query string. This is done by walking the marshaled sysinfo map
// once and QueryEscaping every string leaf; numbers and booleans pass through
// unchanged.
//
// Rendering fails on template parse errors and on references to undefined
// fields (missingkey=error). Fields that resolve to an empty string are not
// rejected: the user may have intended empty (e.g. through a pipe or
// conditional). Wrap fields that must not be empty with the Helm-style
// required helper this template registers, e.g.
//
//	config_url: "http://d/?uuid={{ required \"uuid required\" .Values.product.uuid }}"
func RenderConfigURL(u string) (string, error) {
	if !strings.Contains(u, "{{") && !strings.Contains(u, "}}") {
		return u, nil
	}

	ctx, err := buildContext()
	if err != nil {
		return "", fmt.Errorf("rendering config_url: building context: %w", err)
	}

	funcs := sprig.TxtFuncMap()
	funcs["required"] = requiredFunc

	tmpl, err := template.New("config_url").
		Option("missingkey=error").
		Funcs(funcs).
		Parse(u)
	if err != nil {
		return "", fmt.Errorf("rendering config_url: parse: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("rendering config_url: execute: %w", err)
	}

	return buf.String(), nil
}

// requiredFunc is the Helm-style "required" template helper: it errors with
// the provided message when the value is nil or an empty string, otherwise
// passes the value through. Registered under the "required" name.
func requiredFunc(msg string, v interface{}) (interface{}, error) {
	if v == nil {
		return v, fmt.Errorf("%s", msg)
	}
	if s, ok := v.(string); ok && s == "" {
		return v, fmt.Errorf("%s", msg)
	}
	return v, nil
}

// buildContext is the seam tests replace to inject a deterministic context.
var buildContext = defaultBuildContext

// defaultBuildContext mirrors yip's templateSysData: it JSON-round-trips
// sysinfo, then adds Random and ProtectedID, wraps under {"Values": ...} and
// walks the map to URL-escape every string leaf.
func defaultBuildContext() (map[string]interface{}, error) {
	var system sysinfo.SysInfo
	system.GetSysInfo()

	data, err := json.Marshal(&system)
	if err != nil {
		return nil, fmt.Errorf("marshaling sysinfo: %w", err)
	}

	values := map[string]interface{}{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("unmarshaling sysinfo: %w", err)
	}

	values["Random"] = yiputils.RandomString(32)
	protectedID, _ := machineid.ProtectedID("kairos-config-url")
	values["ProtectedID"] = protectedID

	return walkAndEscape(map[string]interface{}{"Values": values}), nil
}

// walkAndEscape returns a copy of m with every string leaf URL-escaped via
// url.QueryEscape. Numbers, booleans, and nil are passed through unchanged.
// Nested maps and slices are walked recursively.
func walkAndEscape(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = escapeValue(v)
	}
	return out
}

// escapeValue returns v with every string leaf URL-escaped via url.QueryEscape,
// recursing into nested maps and slices. Other types pass through unchanged.
func escapeValue(v interface{}) interface{} {
	switch t := v.(type) {
	case string:
		return url.QueryEscape(t)
	case map[string]interface{}:
		return walkAndEscape(t)
	case []interface{}:
		cp := make([]interface{}, len(t))
		for i, item := range t {
			cp[i] = escapeValue(item)
		}
		return cp
	default:
		return v
	}
}
