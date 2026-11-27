package elemental

import (
	"context"

	"github.com/kairos-io/kairos/v4/agent/pkg/p2p"
)

// SetFetchP2PForTest replaces the p2p fetch and returns a restore function.
func SetFetchP2PForTest(f func(context.Context, p2p.FetchConfig) (*p2p.Result, error)) func() {
	old := fetchP2P
	fetchP2P = f
	return func() { fetchP2P = old }
}
