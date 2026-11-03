package bundled

// MissingFips returns the names of the embedded FIPS binaries that are
// empty. A kairos-init built with FIPS_STUBS=1 (the slim PR pipeline) embeds
// zero-byte placeholders, and installing those with --fips would leave an
// image that only fails at boot.
func MissingFips() []string {
	var missing []string
	for _, b := range []struct {
		name string
		data []byte
	}{
		{"kairos", EmbeddedKairosFips},
		{"provider-kairos", EmbeddedKairosProviderFips},
		{"kairos-installer", EmbeddedKairosInstallerFips},
	} {
		if len(b.data) == 0 {
			missing = append(missing, b.name)
		}
	}
	return missing
}
