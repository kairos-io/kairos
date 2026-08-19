//go:build riscv64

package bundled

// FIPS binaries are not available for riscv64.
var (
	EmbeddedAgentFips            []byte
	EmbeddedImmucoreFips         []byte
	EmbeddedKcryptChallengerFips []byte
	EmbeddedKairosProviderFips   []byte
)
