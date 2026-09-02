package cli

import (
	"errors"
	"fmt"
	"net/url"
)

// apiError separates the two failures that look identical to a caller of the
// API client but need completely different things from whoever reads the
// message.
//
// A transport failure means the edgevpn daemon is not listening where we
// looked, which is a configuration or service problem. Anything else means we
// reached the daemon and it could not give us the value, which on a fresh
// cluster usually just means "not published yet, wait".
//
// The client returns *url.Error for the first case, because every request goes
// through net/http, and a decode error for the second.
func apiError(what, address string, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf(
			"cannot reach the edgevpn API at %s: %w "+
				"(check that the edgevpn service is running and that this address "+
				"matches the APILISTEN it was started with)",
			address, err)
	}

	return fmt.Errorf("could not read the %s from the network at %s: %w", what, address, err)
}
