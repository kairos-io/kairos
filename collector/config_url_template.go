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
// Rendering fails if the template is invalid, references an undefined field
// (missingkey=error), or produces the literal "<no value>". An empty
// substitution is also rejected because a URL ending in "?uuid=" is worse
// than a boot-time error.
func RenderConfigURL(u string) (string, error) {
	if !strings.Contains(u, "{{") && !strings.Contains(u, "}}") {
		return u, nil
	}

	ctx, err := buildContext()
	if err != nil {
		return "", fmt.Errorf("rendering config_url: building context: %w", err)
	}

	tmpl, err := template.New("config_url").
		Option("missingkey=error").
		Funcs(sprig.TxtFuncMap()).
		Parse(u)
	if err != nil {
		return "", fmt.Errorf("rendering config_url: parse: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("rendering config_url: execute: %w", err)
	}

	out := buf.String()
	if strings.Contains(out, "<no value>") {
		return "", fmt.Errorf("rendering config_url: template resolved to a missing value in %q", u)
	}

	// Belt and braces: reject empty substitutions like "?uuid=" or trailing
	// "&mac=". The check is coarse but catches the common case where a
	// scalar is defined but blank on this host.
	if err := rejectEmptySubstitutions(u, out); err != nil {
		return "", fmt.Errorf("rendering config_url: %w", err)
	}

	return out, nil
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

// rejectEmptySubstitutions inspects the rendered URL for query-string params
// left empty by a substitution. It compares against the template so params
// that were literally empty in the template ("?foo=") are not flagged.
func rejectEmptySubstitutions(tmpl, rendered string) error {
	// Only look at what follows the first "?" if any; before that, empty
	// segments have no query-string meaning.
	qIdx := strings.Index(rendered, "?")
	if qIdx < 0 {
		return nil
	}
	rq := rendered[qIdx+1:]

	tqIdx := strings.Index(tmpl, "?")
	var tq string
	if tqIdx >= 0 {
		tq = tmpl[tqIdx+1:]
	}

	for _, pair := range strings.Split(rq, "&") {
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			continue
		}
		key := pair[:eq]
		val := pair[eq+1:]
		if val != "" {
			continue
		}
		// Value is empty in the rendered URL. Only fail if the template's
		// same key had a "{{" substitution (i.e. the emptiness is a result
		// of an empty variable, not a literal "?foo=" in the template).
		if templateHadSubstitutionFor(tq, key) {
			return fmt.Errorf("substitution for %q resolved to an empty string", key)
		}
	}
	return nil
}

// templateHadSubstitutionFor reports whether the query-string portion of the
// template contains a "key=...{{...}}..." pair.
func templateHadSubstitutionFor(templateQuery, key string) bool {
	for _, pair := range strings.Split(templateQuery, "&") {
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			continue
		}
		if pair[:eq] != key {
			continue
		}
		if strings.Contains(pair[eq+1:], "{{") {
			return true
		}
	}
	return false
}
