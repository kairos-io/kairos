package main

func init() {
	// Unlike the other subs, "provider" is not code linked into this binary:
	// it execs the installed provider plugin. Linking provider-kairos's CLI in
	// instead would drag edgevpn, libp2p and kube-vip into every image,
	// including core ones that have no provider at all -- measured at +46 MiB
	// on a 32 MiB binary. See provider.go for the delegation rules.
	register("provider", runProvider)
}
