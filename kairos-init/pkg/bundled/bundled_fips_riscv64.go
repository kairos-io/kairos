//go:build riscv64

package bundled

// FIPS binaries are not available for riscv64.
var (
	EmbeddedKairosFips          []byte
	EmbeddedKairosProviderFips  []byte
	EmbeddedKairosInstallerFips []byte
)
