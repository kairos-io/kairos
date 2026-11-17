package builder

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/google/go-containerregistry/pkg/name"
)

// ValidateImageRef rejects a value that is not a plain OCI image reference.
// Image references are written into generated Dockerfiles as FROM lines, so
// whitespace or a control character would let the value add instructions of
// its own. Image-resolver URIs (docker://, oci:, dir:, file:, ocifile:) are
// not valid in a FROM line either, and the path schemes name files on the
// server. The reference is checked, not rewritten: callers keep the string
// the user supplied.
func ValidateImageRef(field, ref string) error {
	if strings.IndexFunc(ref, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("invalid %s %q: whitespace and control characters are not allowed in an image reference", field, ref)
	}
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %v", field, ref, err)
	}
	// The parser reads "scheme:/path" as a registry named "scheme:" with an
	// empty port. No real registry looks like that, while an image such as
	// docker:24.0-dind parses with its own registry and is left alone.
	if registry := parsed.Context().RegistryStr(); strings.HasSuffix(registry, ":") {
		return fmt.Errorf("invalid %s %q: %s is a URI scheme, not a registry; give a plain image reference", field, ref, registry)
	}
	return nil
}

// ValidateImageRefs checks every image reference in o that a builder writes
// into a generated Dockerfile or build spec. Unset fields are skipped. The
// error wraps ErrInvalidBuildOptions.
func (o BuildOptions) ValidateImageRefs() error {
	for _, f := range []struct{ field, ref string }{
		{"base image", o.BaseImage},
		{"base image", o.Source.BaseImage},
		{"kairos-init image", o.KairosInitImage},
		{"hadron base", o.HadronBase},
	} {
		if f.ref == "" {
			continue
		}
		if err := ValidateImageRef(f.field, f.ref); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidBuildOptions, err)
		}
	}
	return nil
}
