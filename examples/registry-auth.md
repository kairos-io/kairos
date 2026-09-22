# Registry authentication

Install and upgrade image pulls use the host's existing Docker or Podman
credential configuration by default. To provide credentials for one operation,
set `registry-auth` under `install` or `upgrade` in cloud-config.
The inline key is matched case-insensitively. Unknown keys containing `auth`
directly under either operation are rejected; use `registry-auth`.

The existing [Docker and Podman credential-file configuration](https://kairos.io/docs/v4.1.2/advanced/private_registry_auth/)
continues to work when `registry-auth` is omitted. Kairos does not run a login
command or create a credential file when explicit credentials are supplied.

```yaml
#cloud-config
install:
  source: "oci:registry.example.com/kairos/system:version"
  registry-auth:
    username: "REGISTRY_USERNAME"
    password: "REGISTRY_PASSWORD"

upgrade:
  system:
    source: "oci:registry.example.com/kairos/system:next-version"
  registry-auth:
    username: "REGISTRY_USERNAME"
    password: "REGISTRY_PASSWORD"
```

The supported forms are `username` with `password`, base64 encoded `auth`
(`username:password`), `identity-token`, or `registry-token`. Use one form per
operation. An explicit credential is used for that operation and a failed
login does not fall back to the host keychain. Set `registry-auth: {}` or
`registry-auth: null` to use the default keychain behavior.

To read credentials from a file, set `file` under `registry-auth` instead of
providing credentials directly:

```yaml
install:
  registry-auth:
    file: "/run/secrets/registry-auth.yaml"

upgrade:
  registry-auth:
    file: "/run/secrets/registry-auth.yaml"
```

The file contains one YAML object with lowercase field names from the supported
credential forms. Inline credential field names are case-insensitive; file
field names must use their documented lowercase spelling.

```yaml
username: "REGISTRY_USERNAME"
password: "REGISTRY_PASSWORD"
```

Use `file` on its own. Other fields alongside it, file references inside the
credential file, and multiple YAML documents are rejected. Missing, unreadable,
empty, or invalid files fail the operation without falling back to the keychain.

The file must be readable by the agent before install or upgrade starts.
The `before-install` and `before-upgrade` hooks run after image sizing and are
therefore too late to create it. Use restricted permissions such as `0600`.
Only the path is kept in cloud-config; this option does not copy the file or
its contents to the installed system. Supply it again for later upgrades if
it was temporary. File credentials have the same scope as direct credentials.

For a registry with a self-signed certificate, combine authentication with
`allow-insecure-registries` in the same operation block:

```yaml
upgrade:
  allow-insecure-registries: true
  registry-auth:
    username: "REGISTRY_USERNAME"
    password: "REGISTRY_PASSWORD"
```

This also allows plain HTTP and disables certificate verification. Keep the
default secure setting when the registry has a trusted certificate.

Cloud-config can be copied to the installed system by the installer. Treat
these values as plaintext credentials wherever that copy is enabled. Base64
encoding does not encrypt a password. The new option does not add encrypted
credential storage.

Debug configuration output contains configuration data only. It redacts keys
containing `auth`, `passwd`, `password`, `token`, or `secret`.

Each operation block supplies one set of credentials to its image extractor.
It is not a map of credentials by registry. Use the existing keychain when
different image registries need separate credentials. Install and upgrade
blocks can supply different credentials. Credentials remain on the shared image
extractor for the rest of the process, so later system extension pulls reuse
them even when the extension comes from a different registry.
