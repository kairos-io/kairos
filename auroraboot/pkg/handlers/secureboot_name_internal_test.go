package handlers

import "testing"

// TestValidateKeySetName checks the strict allowlist that prevents a key-set
// name from escaping keysDir (the name is used verbatim as a directory
// component and inside openssl -keyout paths).
func TestValidateKeySetName(t *testing.T) {
	t.Parallel()

	valid := []string{
		"prod",
		"prod-keys",
		"prod_keys_1",
		"A",
		"abc123",
		"abcdefghij-abcdefghij-abcdefghij-abcdefghij-abcdefghij-abcdefg64",
	}
	for _, n := range valid {
		if err := validateKeySetName(n); err != nil {
			t.Errorf("expected %q to be accepted, got error: %v", n, err)
		}
	}

	invalid := []string{
		"",
		".",
		"..",
		"../x",
		"a/b",
		"/abs",
		"/etc/passwd",
		"..",
		"a b",
		"a;b",
		"a$b",
		"foo.bar",
		"this-name-is-definitely-longer-than-sixty-four-characters-so-it-must-fail",
	}
	for _, n := range invalid {
		if err := validateKeySetName(n); err == nil {
			t.Errorf("expected %q to be rejected, but it was accepted", n)
		}
	}
}
