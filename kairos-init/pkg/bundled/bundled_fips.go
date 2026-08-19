//go:build !riscv64

package bundled

import _ "embed"

//go:embed binaries/fips/kairos-agent
var EmbeddedAgentFips []byte

//go:embed binaries/fips/immucore
var EmbeddedImmucoreFips []byte

//go:embed binaries/fips/kcrypt-discovery-challenger
var EmbeddedKcryptChallengerFips []byte

//go:embed binaries/fips/provider-kairos
var EmbeddedKairosProviderFips []byte
