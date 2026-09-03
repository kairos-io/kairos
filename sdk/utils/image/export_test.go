package image

// IsTransientNetworkError exposes isTransientNetworkError to the external test
// package, so the classification can be asserted against error values that are
// awkward to provoke from a test server.
var IsTransientNetworkError = isTransientNetworkError
