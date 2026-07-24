# EdgeVPN Unix Socket Implementation Plan

**Goal:** Make provider-kairos use a root-only Unix socket for the EdgeVPN API by default while preserving explicit TCP compatibility.
**Architecture:** A shared provider default feeds the CLI and bootstrap fallback. SetupAPI and SetupVPN generate matching EdgeVPN listener variables, while user-supplied environment values retain final precedence.

## Global Constraints

The default endpoint is `unix:///run/edgevpn-kairos.sock`.
The default Unix socket mode is `0600`.
Do not add users, groups, or systemd socket activation.
Preserve explicit TCP and custom Unix endpoint overrides.

### Task 1: Switch the default EdgeVPN transport to a Unix socket  {id: 1, deps: []}

**Files:**
- Modify: `internal/provider/p2p.go`
- Modify: `internal/provider/bootstrap.go`
- Modify: `internal/cli/start.go`
- Test: `internal/provider/*_test.go`
- Test: `internal/cli/*_test.go`

**Interfaces:**
- Produces: exported constants `provider.DefaultEdgeVPNAPIAddress string` and `provider.DefaultEdgeVPNAPISocketMode string`.
- Consumes: `SetupAPI(apiAddress, rootDir string, start bool, c *providerConfig.Config) error`.
- Consumes: `SetupVPN(instance, apiAddress, rootDir string, start bool, c *providerConfig.Config) error`.
- Consumes: CLI flag `--api` with `EDGEVPN_API` environment override.

Use `unix:///run/edgevpn-kairos.sock` when no endpoint is supplied by the CLI,
environment, or bootstrap payload. Generate `APILISTENUNIXMODE=0600` for the
default or any Unix listener, but allow `p2p.vpn.env` to override it. Preserve
explicit TCP endpoints and custom Unix paths. Add focused regression tests.

**Verify:** `go test ./...`
