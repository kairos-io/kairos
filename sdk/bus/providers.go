package bus

import "github.com/mudler/go-pluggable"

// HasProviders reports whether a provider plugin is installed on this system.
//
// A provider is what answers the pairing challenge that `kairos-agent install`
// turns into a QR code, so a frontend deciding whether to offer pairing has to
// know whether anything would answer it. With no provider that flow has no
// token to show and drops to a shell instead.
//
// Discovery goes through the same Autoload call Initialize uses, with the same
// prefix and paths, so the answer cannot drift from what the agent will find.
//
// paths defaults to DefaultProviderPaths when none are given.
func HasProviders(paths ...string) bool {
	if len(paths) == 0 {
		paths = DefaultProviderPaths
	}
	m := pluggable.NewManager(nil).Autoload(DefaultProviderPrefix, paths...)
	return len(m.Plugins) > 0
}
