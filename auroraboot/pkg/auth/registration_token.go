package auth

import "sync"

// RegistrationToken holds the registration token shared by the registration
// middleware and the settings handler that rotates it. Reads and writes go
// through a lock, so a rotation never races an in-flight registration check.
type RegistrationToken struct {
	mu    sync.RWMutex
	value string
}

// NewRegistrationToken returns a holder seeded with value.
func NewRegistrationToken(value string) *RegistrationToken {
	return &RegistrationToken{value: value}
}

// Get returns the current token.
func (t *RegistrationToken) Get() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.value
}

// Set replaces the current token.
func (t *RegistrationToken) Set(value string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.value = value
}

// Matches reports whether candidate equals the current token, compared in
// constant time.
func (t *RegistrationToken) Matches(candidate string) bool {
	return secureCompare(candidate, t.Get())
}
