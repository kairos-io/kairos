package redfish

import (
	"context"
	"errors"
	"strings"
)

// Reachable performs a session-free, unauthenticated reachability check against a
// Redfish endpoint: a single GET {endpoint}/redfish/v1/ (ServiceRoot) through the
// defensive HTTP client (cross-origin redirect reject + response-body cap +
// TLS-verify honouring insecure). It sends NO credentials and creates NO Redfish
// session, so it never consumes one of a BMC's capped concurrent sessions — that
// is the whole point: status can be polled without the session churn a full
// session-based Inspect incurs.
//
// A 2xx response carrying a parseable ServiceRoot returns nil (reachable). A
// refused connection, TLS error, timeout, non-2xx status, or an unparseable body
// returns a non-nil error (unreachable). On hardened BMCs that gate the
// ServiceRoot behind auth, the 401 surfaces here as an error — the caller should
// steer the operator to an authenticated Inspect for a real liveness check.
//
// insecure is the inverse of the target's VerifySSL: pass false to keep TLS
// verification on (the default), true only when the caller explicitly opted out.
// The returned error is scrubbed of any basic-auth credentials defensively, though
// no credentials are ever supplied to this call.
func Reachable(ctx context.Context, endpoint string, insecure bool) error {
	if strings.TrimSpace(endpoint) == "" {
		return errors.New("redfish endpoint is empty")
	}
	if _, err := fetchServiceRoot(ctx, endpoint, insecure); err != nil {
		return scrubError(err, "")
	}
	return nil
}

// scrubError removes a credential substring from an error before it crosses the
// package boundary. It mirrors Deployer.scrub but is free-standing so the
// session-free Reachable helper (which has no Deployer) can reuse it. An empty
// secret leaves the error unchanged.
func scrubError(err error, secret string) error {
	if err == nil {
		return nil
	}
	if secret == "" {
		return err
	}
	msg := strings.ReplaceAll(err.Error(), secret, "[REDACTED]")
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}
