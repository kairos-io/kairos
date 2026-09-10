package schema

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// ExtensionsSchema represents the extensions block in the Kairos
// configuration. It is where a node says which indexes an extension name is
// resolved against, and whether an extension has to be signed to be merged.
type ExtensionsSchema struct {
	_                struct{} `title:"Kairos Schema: Extensions block" description:"Node-wide settings for system and configuration extensions."`
	Catalogs         []string `json:"catalogs,omitempty" description:"Extension indexes a bare extension name is resolved against, searched in order: the first catalog publishing the name wins. Replaces the built-in default when set." examples:"[\"https://kairos-io.github.io/hadron-layers/releases.json\"]"`
	IgnoreSignatures bool     `json:"ignore_signatures,omitempty" description:"Merge extensions that carry no signature systemd can verify. Off by default: under Trusted Boot an unsigned extension is refused, which is the point of Trusted Boot."`
}

// ExtensionSchema represents one requested extension. It is written either as
// a plain string, optionally `name@version`, or as a mapping so a version
// constraint containing a space can be spelled out.
type ExtensionSchema struct{}

var (
	_ jsonschemago.OneOfExposer = ExtensionSchema{}
	_ jsonschemago.Preparer     = ExtensionSchema{}
)

// PrepareJSONSchema drops the `object` type the reflector infers from the Go
// struct. The struct is only a carrier for the oneOf branches: an extension is
// written as a string as often as it is written as a mapping, and leaving
// `"type": "object"` in place rejects the string form.
func (ExtensionSchema) PrepareJSONSchema(schema *jsonschemago.Schema) error {
	schema.Type = nil
	return nil
}

// ExtensionReferenceSchema is the shorthand string form: a catalog name, a
// `name@version`, a URI, or an absolute path.
type ExtensionReferenceSchema string

// ExtensionVersionedSchema is the mapping form, which is the only way to write
// a version constraint that contains a space, such as ">= 2.1, < 3".
type ExtensionVersionedSchema struct {
	Name    string `json:"name" required:"true" minLength:"1" description:"Catalog name of the extension, or a URI or absolute path to an extension image."`
	Version string `json:"version,omitempty" description:"An exact catalog version, or a semver constraint such as \">= 2.1, < 3\". Only meaningful for a catalog name." examples:"[\"2.1.7\",\">= 2.1, < 3\"]"`
}

// JSONSchemaOneOf states that an extension is either the shorthand string or
// the name/version mapping.
func (ExtensionSchema) JSONSchemaOneOf() []interface{} {
	return []interface{}{
		ExtensionReferenceSchema(""), ExtensionVersionedSchema{},
	}
}
