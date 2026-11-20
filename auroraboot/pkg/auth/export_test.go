package auth

// SessionCount returns how many sessions the store holds, expired or not.
func SessionCount(s *SessionStore) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}
