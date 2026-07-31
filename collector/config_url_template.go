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
		// Defensive: missingkey=error catches undefined fields before we
		// reach this branch. If a template still produces "<no value>",
		// the rendered output usually makes the empty action obvious.
		return "", fmt.Errorf("rendering config_url: template %q resolved to a missing value; rendered to %q", u, out)
	}

	// Belt and braces: reject empty substitutions that leave "?uuid=" or a
	// blank path segment ("http://x//register"). Catches the common case
	// where a scalar is defined but blank on this host.
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

// rejectEmptySubstitutions inspects the rendered URL for empty spots left by
// substitutions, in both the query string and the path. It compares against
// the template so parts that were literally empty in the template
// ("?foo=" or "/foo//bar") are not flagged; only positions the template
// filled with a "{{ ... }}" action are.
//
// It parses the rendered URL via net/url; a rendered URL that will not parse
// is a fail-loudly case too, since fetching it would blow up later anyway.
func rejectEmptySubstitutions(tmpl, rendered string) error {
	parsed, err := url.Parse(rendered)
	if err != nil {
		return fmt.Errorf("parsing rendered URL: %w", err)
	}

	// Separate the template's path portion from its query portion so we can
	// compare each half against the parsed rendered URL. url.Parse cannot
	// help us here because "{{" is not a valid URL byte, so we string-split.
	tPath := tmpl
	var tQuery string
	if tqIdx := strings.Index(tmpl, "?"); tqIdx >= 0 {
		tPath = tmpl[:tqIdx]
		tQuery = tmpl[tqIdx+1:]
	}

	// Strip the "scheme://host" prefix from the template's path portion so
	// the segments line up with parsed.Path.
	if idx := strings.Index(tPath, "://"); idx >= 0 {
		rest := tPath[idx+3:]
		if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
			tPath = rest[slashIdx:]
		} else {
			tPath = ""
		}
	}

	// Path segments: for each non-leading empty segment in the rendered
	// path, fail if the template's same-position segment contained "{{".
	// The leading segment ("" from strings.Split of a "/"-prefixed path) is
	// skipped because it is never a substitution site.
	if tPath != "" {
		tSegs := strings.Split(tPath, "/")
		rSegs := strings.Split(parsed.Path, "/")
		n := len(tSegs)
		if len(rSegs) < n {
			n = len(rSegs)
		}
		for i := 1; i < n; i++ {
			if rSegs[i] == "" && strings.Contains(tSegs[i], "{{") {
				return fmt.Errorf("substitution in path segment %d resolved to an empty string", i)
			}
		}
	}

	// Query values: for each empty value in the rendered query, fail if
	// the template's same key had a "{{" substitution (i.e. the emptiness
	// is a result of an empty variable, not a literal "?foo=" in the
	// template).
	for _, pair := range strings.Split(parsed.RawQuery, "&") {
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			continue
		}
		key := pair[:eq]
		val := pair[eq+1:]
		if val != "" {
			continue
		}
		if templateHadSubstitutionFor(tQuery, key) {
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
