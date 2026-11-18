package auth

import "testing"

// secureCompare is a thin constant-time wrapper; this locks its equality
// contract (and that it does not panic on empty / unequal-length inputs, which
// bearer-token comparisons routinely feed it).
func TestSecureCompare(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"equal", "correct-horse-battery", "correct-horse-battery", true},
		{"different same length", "aaaaaaaa", "bbbbbbbb", false},
		{"different length prefix", "secret", "secretx", false},
		{"empty vs secret", "", "secret", false},
		{"both empty", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := secureCompare(tc.a, tc.b); got != tc.want {
				t.Fatalf("secureCompare(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
