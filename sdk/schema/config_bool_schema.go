package schema

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// configBoolPattern is the set of strings the agent reads as a boolean. It is
// strconv.ParseBool's input set, which is the function both of the readers
// below hand a string to.
const configBoolPattern = `^(1|0|t|T|true|True|TRUE|f|F|false|False|FALSE)$`

// ConfigBool is a yes/no key that the agent reads through a weakly typed
// decoder, so it accepts more than YAML's true and false literals. Two
// readers are involved and they agree on the accepted set:
//
//   - viper's Unmarshal, which unmarshallFullSpec hands a whole block to, sets
//     mapstructure's WeaklyTypedInput. A string goes through
//     strconv.ParseBool, and a number is true when it is not zero.
//   - configBool in agent/pkg/config, which reads upgrade.recovery off the
//     merged cloud config, does the same by hand and on purpose: templating
//     engines quote their output, so a generated config writes "true".
//
// Declaring such a key as a plain boolean makes a config the node goes on to
// apply fail validation, which is a warning normally and a hard error under
// --strict-validation, so the declaration has to match what the reader
// accepts. A string neither reader can parse, YAML's unquoted yes among them,
// still fails in both places, and failing it here names the key.
//
// agent/pkg/config/spec_bool_test.go holds the two to the same set.
type ConfigBool struct{}

var _ jsonschemago.Preparer = ConfigBool{}

// PrepareJSONSchema replaces the object type the reflector infers from the
// empty struct with the three forms the readers accept. The pattern is a
// sibling of the type list rather than a branch of a oneOf because pattern
// applies to strings only: a boolean or a number instance ignores it.
func (ConfigBool) PrepareJSONSchema(schema *jsonschemago.Schema) error {
	schema.Type = (&jsonschemago.Type{}).WithSliceOfSimpleTypeValues(
		jsonschemago.Boolean, jsonschemago.Integer, jsonschemago.String,
	)
	schema.WithPattern(configBoolPattern)

	return nil
}
