//go:build !riscv64

package bundled

import _ "embed"

//go:embed binaries/fips/kairos
var EmbeddedKairosFips []byte

//go:embed binaries/fips/provider-kairos
var EmbeddedKairosProviderFips []byte

//go:embed binaries/fips/kairos-installer
var EmbeddedKairosInstallerFips []byte
