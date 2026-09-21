# Registry authentication

Install and upgrade image pulls use the host's existing Docker or Podman
credential configuration by default. To provide credentials for one operation,
set `registry-auth` under `install` or `upgrade` in cloud-config.

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

Each operation block supplies one set of credentials to its image extractor.
It is not a map of credentials by registry. Use the existing keychain when
different image registries need separate credentials. Install and upgrade
blocks can supply different credentials.
