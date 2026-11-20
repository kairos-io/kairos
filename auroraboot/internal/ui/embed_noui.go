//go:build !ui

package ui

import "embed"

// Assets is empty in a build without the `ui` tag, such as `go install`,
// which builds from a module zip that never holds the generated dist/.
// The web server reports the missing UI instead of serving it.
var Assets embed.FS
