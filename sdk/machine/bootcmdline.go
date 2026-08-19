package machine

import (
	"os"
	"strconv"
	"strings"

	"github.com/google/shlex"
	"github.com/kairos-io/kairos-sdk/unstructured"
	"gopkg.in/yaml.v3"
)

// Cmdline stanzas owned exclusively by this package. ParseCmdLine and any
// other generic dot-parser must skip these so their payload is not double
// processed and does not leak spurious kairos/cos top-level keys.
const (
	kairosConfigPrefix    = "kairos.config="
	kairosConfigURLPrefix = "kairos.config_url="
	cosSetupPrefix        = "cos.setup="
)

// KairosOwnedPrefixes lists the cmdline token prefixes this package owns.
// Callers implementing their own cmdline parsing (e.g. the generic
// dot-nested parser in collector.ParseCmdLine) must skip any token that
// starts with one of these so the payload is not double-parsed.
var KairosOwnedPrefixes = []string{kairosConfigPrefix, kairosConfigURLPrefix, cosSetupPrefix}

// KairosCmdlineYAML builds a YAML document from every Kairos-owned cmdline
// stanza on the given file (defaults to /proc/cmdline when empty).
//
// Three token forms are recognised, all handled by this single entrypoint:
//
//   - kairos.config=KEY=VALUE — sets KEY to VALUE. KEY supports dot notation
//     for nested maps (e.g. install.auto=true) and numeric segments for list
//     indices (e.g. users.0.name=kairos). Repeatable; later occurrences of
//     the same key win. Values may contain spaces if the whole token is
//     quoted (e.g. kairos.config="hostname=my box"). All values are stored
//     as strings; downstream schemas coerce as needed.
//   - kairos.config_url=URL — convenience form that sets the top-level
//     config_url key. URL is stored verbatim (no KEY=VALUE parsing) so it
//     can contain '=' safely.
//   - cos.setup=X — legacy alias, kept so existing installs keep booting.
//     If X contains '=' it is treated exactly like kairos.config=X;
//     otherwise the bare value is routed into config_url (matching the
//     historical "cos.setup points at a config" behavior). Prefer
//     kairos.config / kairos.config_url in new deployments.
//
// Returns (nil, nil) when no Kairos-owned stanzas are present so callers
// can skip cheaply.
func KairosCmdlineYAML(file string) ([]byte, error) {
	if file == "" {
		file = "/proc/cmdline"
	}
	dat, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return KairosCmdlineYAMLFromString(string(dat))
}

// KairosCmdlineYAMLFromString is the string-based counterpart of
// KairosCmdlineYAML. Useful for tests and for callers that have already
// read the cmdline from a mock filesystem.
func KairosCmdlineYAMLFromString(s string) ([]byte, error) {
	tokens, _ := shlex.Split(s)
	root := map[string]interface{}{}
	present := false
	for _, t := range tokens {
		switch {
		case strings.HasPrefix(t, kairosConfigURLPrefix):
			v := strings.Trim(strings.TrimPrefix(t, kairosConfigURLPrefix), `"`)
			if v == "" {
				continue
			}
			root["config_url"] = v
			present = true
		case strings.HasPrefix(t, kairosConfigPrefix):
			payload := strings.TrimPrefix(t, kairosConfigPrefix)
			if payload == "" {
				continue
			}
			applyKairosConfigStanza(root, payload)
			present = true
		case strings.HasPrefix(t, cosSetupPrefix):
			payload := strings.TrimPrefix(t, cosSetupPrefix)
			if payload == "" {
				continue
			}
			if strings.Contains(payload, "=") {
				applyKairosConfigStanza(root, payload)
			} else {
				root["config_url"] = strings.Trim(payload, `"`)
			}
			present = true
		}
	}
	if !present {
		return nil, nil
	}
	return yaml.Marshal(root)
}

// DotToYAML is the generic dot-nested cmdline parser: every KEY=VALUE
// token on the given file (defaults to /proc/cmdline when empty)
// becomes a YAML entry, with dots in KEY producing nested maps.
// Kairos-owned prefixes (see KairosOwnedPrefixes) are intentionally
// skipped so those tokens flow exclusively through KairosCmdlineYAML.
// Callers wanting both families merged should use
// collector.ParseCmdLine, which routes each token to the right parser.
func DotToYAML(file string) ([]byte, error) {
	if file == "" {
		file = "/proc/cmdline"
	}
	dat, err := os.ReadFile(file)
	if err != nil {
		return []byte{}, err
	}
	return unstructured.ToYAML(stringToMap(string(dat)))
}

// DotStringToYAML is the string-based counterpart of DotToYAML.
func DotStringToYAML(s string) ([]byte, error) {
	return unstructured.ToYAML(stringToMap(s))
}

// applyKairosConfigStanza parses a "KEY=VALUE" payload (KEY may use
// dot notation with numeric segments for list indices) and writes it into
// root. Payloads without '=' set the key to the literal string "true", so
// bare-flag stanzas like kairos.config=install.auto behave sensibly.
func applyKairosConfigStanza(root map[string]interface{}, payload string) {
	parts := strings.SplitN(payload, "=", 2)
	value := "true"
	if len(parts) > 1 {
		value = strings.Trim(parts[1], `"`)
	}
	key := strings.Trim(parts[0], `"`)
	if key == "" {
		return
	}
	segs := strings.Split(key, ".")
	// The root is always a map keyed by the first segment (numeric first
	// segments are treated as string keys, since the top level is a map).
	first := segs[0]
	root[first] = setDotIndexPath(root[first], segs[1:], value)
}

// setDotIndexPath writes value at the given path into node, creating
// nested maps for non-numeric segments and slices for numeric segments.
// The node argument may be nil, a map[string]interface{}, or a
// []interface{}. Returns the possibly-replaced node so callers can
// re-assign into their parent container.
func setDotIndexPath(node interface{}, segs []string, value interface{}) interface{} {
	if len(segs) == 0 {
		return value
	}
	seg := segs[0]
	rest := segs[1:]
	if idx, err := strconv.Atoi(seg); err == nil && idx >= 0 {
		var list []interface{}
		if l, ok := node.([]interface{}); ok {
			list = l
		}
		for len(list) <= idx {
			list = append(list, nil)
		}
		list[idx] = setDotIndexPath(list[idx], rest, value)
		return list
	}
	m, ok := node.(map[string]interface{})
	if !ok {
		m = map[string]interface{}{}
	}
	m[seg] = setDotIndexPath(m[seg], rest, value)
	return m
}

// stringToMap is the generic dot-nested KEY=VALUE parser used by
// DotToYAML / DotStringToYAML. It skips every Kairos-owned prefix so
// those tokens are handled exclusively by KairosCmdlineYAML.
func stringToMap(s string) map[string]interface{} {
	v := map[string]interface{}{}

	splitted, _ := shlex.Split(s)
	for _, item := range splitted {
		if isKairosOwned(item) {
			continue
		}
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

func isKairosOwned(token string) bool {
	for _, p := range KairosOwnedPrefixes {
		if strings.HasPrefix(token, p) {
			return true
		}
	}
	return false
}
