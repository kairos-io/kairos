//go:build ui

package ui

import "embed"

// Assets holds the compiled React frontend under dist/, which `make
// ui-build` (or `npm run build` in ui/) writes and git ignores. Builds that
// ship the web UI run that first and pass `-tags ui`.
//
//go:embed all:dist
var Assets embed.FS
