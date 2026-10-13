package schema

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// configBoolPattern is the set of strings strconv.ParseBool accepts, which is
// what configBool hands a string to.
const configBoolPattern = `^(1|0|t|T|true|True|TRUE|f|F|false|False|FALSE)$`

// specBoolPattern is configBoolPattern plus the empty string. mapstructure's
// WeaklyTypedInput reads "" as false rather than calling strconv.ParseBool, so
// a key decoded through viper accepts one more value than configBool does. A
// template whose variable is unset writes exactly that.
const specBoolPattern = `^(|1|0|t|T|true|True|TRUE|f|F|false|False|FALSE)$`

// ConfigBool is a yes/no key read by configBool in agent/pkg/config, which
// hands the value to strconv.ParseBool by hand and on purpose: templating
// engines quote their output, so a generated config writes "true".
//
// Declaring such a key as a plain boolean makes a config the node goes on to
// apply fail validation, which is a warning normally and a hard error under
// --strict-validation, so the declaration has to match what the reader
// accepts. A string the reader cannot parse, YAML's unquoted yes among them,
// still fails, and failing it here names the key.
//
// SpecBool is the same idea for the keys read through viper, which accept one
// value more. agent/pkg/config/spec_bool_test.go holds each type to its own
// reader's set.
type ConfigBool struct{}

// SpecBool is a yes/no key that unmarshallFullSpec decodes into a spec through
// viper's Unmarshal. viper sets mapstructure's WeaklyTypedInput, and neither
// setDecoder nor decodeHook turns it off, so a string goes through
// strconv.ParseBool, the empty string reads as false, and a number is true
// when it is not zero.
type SpecBool struct{}

var (
	_ jsonschemago.Preparer = ConfigBool{}
	_ jsonschemago.Preparer = SpecBool{}
)

// PrepareJSONSchema replaces the object type the reflector infers from the
// empty struct with the three forms the reader accepts. The pattern is a
// sibling of the type list rather than a branch of a oneOf because pattern
// applies to strings only: a boolean or a number instance ignores it.
func (ConfigBool) PrepareJSONSchema(schema *jsonschemago.Schema) error {
	return prepareBoolSchema(schema, configBoolPattern)
}

// PrepareJSONSchema is ConfigBool's, with the empty string allowed.
func (SpecBool) PrepareJSONSchema(schema *jsonschemago.Schema) error {
	return prepareBoolSchema(schema, specBoolPattern)
}

func prepareBoolSchema(schema *jsonschemago.Schema, pattern string) error {
	schema.Type = (&jsonschemago.Type{}).WithSliceOfSimpleTypeValues(
		jsonschemago.Boolean, jsonschemago.Integer, jsonschemago.String,
	)
	schema.WithPattern(pattern)

	return nil
}

// UnmarshalJSON accepts whatever the schema above accepts and keeps nothing:
// the type carries no value, it only declares one.
//
// It has to exist. ValidateSemantics decodes the parsed config into a
// RootSchema and, when that fails, reports no findings and leaves the type
// error to IsValid. Without this method, decoding a boolean into the empty
// struct fails, so a config carrying any key of this type would skip the
// cross-field checks, among them the fatal one that refuses
// install.ssh_hardening with no authorized key.
func (*ConfigBool) UnmarshalJSON([]byte) error { return nil }

// UnmarshalJSON is ConfigBool's, for the same reason.
func (*SpecBool) UnmarshalJSON([]byte) error { return nil }
