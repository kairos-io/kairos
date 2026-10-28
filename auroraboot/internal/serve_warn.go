package internal

// WarnUnauthenticatedServe logs a prominent warning that a netboot/artifact HTTP
// server serves its files without authentication. These servers exist so a
// PXE/HTTP-booting machine can fetch its kernel, initrd, squashfs and the
// cloud-config baked into the image over plain HTTP, and a booting machine has no
// way to present a credential — so the server cannot be gated behind a token
// without breaking netboot. Anyone who can reach addr can therefore download
// whatever is served, including a cloud-config that may carry a registration
// token, SSH keys or passwords. Run it only on a trusted, isolated provisioning
// network.
//
// The lack of authentication is the point being flagged; it is independent of
// which interfaces the server binds. A server can bind all interfaces and still
// require auth, or bind one interface and require none — so this warning speaks
// only to the missing authentication, not to the listen address.
func WarnUnauthenticatedServe(addr, name string) {
	Log.Logger.Warn().
		Str("address", addr).
		Str("server", name).
		Msg("Serving over UNAUTHENTICATED HTTP: booting machines cannot present a credential, so anyone who can reach this address can download the served files, which may include a cloud-config with secrets. Run this only on a trusted, isolated provisioning network — do not expose it to untrusted networks or the internet.")
}
