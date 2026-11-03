package unstructured

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v3"
)

func YAMLHasKey(query string, content []byte) (bool, error) {
	data := map[string]interface{}{}

	err := yaml.Unmarshal([]byte(content), &data)
	if err != nil {
		return false, err
	}

	c, err := gojq.Parse(fmt.Sprintf(".%s | ..", query))
	if err != nil {
		return false, err
	}
	code, err := gojq.Compile(c)
	if err != nil {
		return false, err
	}
	iter := code.Run(data)

	v, _ := iter.Next()

	if err, ok := v.(error); ok {
		return false, err
	}
	if v == nil {
		return false, nil
	}
	return true, nil
}

func lookupKey(command string, data map[string]interface{}) (interface{}, error) {
	query, err := gojq.Parse(command)
	if err != nil {
		return nil, err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return nil, err
	}
	iter := code.Run(data)

	v, ok := iter.Next()
	if !ok {
		return nil, errors.New("failed getting result from gojq")
	}

	return v, nil
}

func LookupString(command string, data map[string]interface{}) (string, error) {
	v, err := lookupKey(command, data)
	if err != nil {
		return "", err
	}

	if t, ok := v.(string); ok {
		return t, nil
	}

	return "", fmt.Errorf("value is not a string: %T", v)
}

func ReplaceValue(command string, data map[string]interface{}) (string, error) {
	v, err := lookupKey(command, data)
	if err != nil {
		return "", err
	}

	if _, ok := v.(map[string]interface{}); ok {
		b, err := yaml.Marshal(v)
		return string(b), err
	}

	return "", fmt.Errorf("unexpected type: %T", v)
}

// func ReplaceKeyValue(command string, data interface{}) (string, error) {
// 	LookupString(commd
// }

// MaxListIndex bounds the list index a cmdline key may use, so that one
// token cannot allocate a list of millions of entries.
const MaxListIndex = 1024

var keySegment = regexp.MustCompile(`^([^\[\]]+)((?:\[[0-9]+\])*)$`)

// ToYAML turns a map of dotted keys into nested YAML. A key is a
// dot-separated path, and a segment may end in list indexes, as in
// stages.boot[0].commands[0]. A segment is used as a literal map key and is
// never evaluated: one that is not a name followed by [N] indexes, such as
// install["device"], is a key with that exact name. Values are stored as
// strings, except "true" and "false", which become booleans. A key that
// cannot be placed is skipped: a segment is empty or starts with a digit, an
// index is above MaxListIndex, or the path runs through a value another key
// set. The returned error names every skipped key, and the YAML holds all
// the others.
func ToYAML(v map[string]interface{}) ([]byte, error) {
	data := map[string]interface{}{}
	var errs error

	// Sorted, so that which of two colliding keys wins is the same on
	// every boot.
	for _, k := range slices.Sorted(maps.Keys(v)) {
		var typed interface{} = fmt.Sprintf("%s", v[k])
		if typed == "true" || typed == "false" {
			typed = typed == "true"
		}
		path, err := parseKey(k)
		if err == nil {
			_, err = setPath(data, path, typed)
		}
		if err != nil {
			// The key only: a cmdline value can be a secret.
			errs = multierror.Append(errs, fmt.Errorf("skipping key %q: %w", k, err))
		}
	}

	out, err := yaml.Marshal(&data)
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	return out, errs
}

// parseKey splits a key into map keys (string) and list indexes (int).
func parseKey(k string) ([]interface{}, error) {
	var path []interface{}
	for _, seg := range strings.Split(k, ".") {
		name, indexes := seg, ""
		if m := keySegment.FindStringSubmatch(seg); m != nil {
			name, indexes = m[1], m[2]
		}
		if name == "" {
			return nil, errors.New("empty key segment")
		}
		if name[0] >= '0' && name[0] <= '9' {
			return nil, fmt.Errorf("key segment %q starts with a digit", name)
		}
		path = append(path, name)
		for _, idx := range strings.Split(strings.Trim(indexes, "[]"), "][") {
			if idx == "" {
				continue
			}
			i, err := strconv.Atoi(idx)
			if err != nil || i > MaxListIndex {
				return nil, fmt.Errorf("list index %s is above %d", idx, MaxListIndex)
			}
			path = append(path, i)
		}
	}
	return path, nil
}

// setPath returns node with value stored at path, creating the maps and
// lists on the way. node is changed only when the whole path can be set.
func setPath(node interface{}, path []interface{}, value interface{}) (interface{}, error) {
	if len(path) == 0 {
		return value, nil
	}
	switch p := path[0].(type) {
	case string:
		if node == nil {
			node = map[string]interface{}{}
		}
		m, ok := node.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("cannot set key %q on a %T", p, node)
		}
		child, err := setPath(m[p], path[1:], value)
		if err != nil {
			return nil, err
		}
		m[p] = child
		return m, nil
	default:
		i := p.(int)
		if node == nil {
			node = []interface{}{}
		}
		l, ok := node.([]interface{})
		if !ok {
			return nil, fmt.Errorf("cannot set index %d on a %T", i, node)
		}
		for len(l) <= i {
			l = append(l, nil)
		}
		child, err := setPath(l[i], path[1:], value)
		if err != nil {
			return nil, err
		}
		l[i] = child
		return l, nil
	}
}

// ToYAMLMap turns a map string interface which describes a yaml file in 'dot.yaml' format to a fully deep marshalled yaml.
func ToYAMLMap(v map[string]interface{}) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	tempData, err := ToYAML(v)
	if err != nil {
		return result, err
	}
	err = yaml.Unmarshal(tempData, &result)

	return result, err
}
