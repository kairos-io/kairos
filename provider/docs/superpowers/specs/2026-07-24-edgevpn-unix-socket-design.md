# EdgeVPN Unix Socket Design

## Scope

Move provider-kairos' default EdgeVPN API endpoint from loopback TCP to
`unix:///run/edgevpn-kairos.sock`. Keep this change limited to transport.
Dedicated users, groups, and systemd socket activation are deferred.

## Design

Define the default endpoint and Unix socket mode once in the provider package.
The CLI, bootstrap fallback, and generated EdgeVPN environment must resolve to
the same endpoint. An explicit `--api`, `EDGEVPN_API`, bootstrap payload, or
`p2p.vpn.env.APILISTEN` value remains supported, including TCP endpoints.

When the resolved listener is Unix-based, generated daemon configuration also
sets `APILISTENUNIXMODE=0600`. A user-supplied environment value has final
precedence.

## Testing

Unit tests cover the CLI default, bootstrap fallback, generated Unix listener
configuration, explicit TCP compatibility, and user environment precedence.
The repository's full Go test suite is the completion gate.
