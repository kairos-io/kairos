package strings

import (
	"strings"
	"testing"
)

func TestRandStringFromCharsetLengthAndAlphabet(t *testing.T) {
	const charset = "abc123"
	for _, n := range []int{1, 7, 32, 257} {
		got := RandStringFromCharset(n, charset)
		if len(got) != n {
			t.Fatalf("length %d: got %d characters, want %d", n, len(got), n)
		}
		for _, r := range got {
			if !strings.ContainsRune(charset, r) {
				t.Fatalf("length %d: %q is not in the charset", n, r)
			}
		}
	}
}

func TestRandStringFromCharsetDegenerateInput(t *testing.T) {
	if got := RandStringFromCharset(0, "abc"); got != "" {
		t.Errorf("zero length: got %q, want the empty string", got)
	}
	if got := RandStringFromCharset(-1, "abc"); got != "" {
		t.Errorf("negative length: got %q, want the empty string", got)
	}
	if got := RandStringFromCharset(8, ""); got != "" {
		t.Errorf("empty charset: got %q, want the empty string", got)
	}
	if got := RandStringFromCharset(5, "x"); got != "xxxxx" {
		t.Errorf("single character charset: got %q, want %q", got, "xxxxx")
	}
}

// A LUKS passphrase and a one-time recovery password must not repeat. Any
// generator seeded once per process from the clock replays its own sequence
// for every other process that starts in the same nanosecond, so the property
// worth holding on to is that independent calls do not collide.
func TestRandStringRunesDoesNotRepeat(t *testing.T) {
	const samples = 2000
	seen := make(map[string]struct{}, samples)
	for i := 0; i < samples; i++ {
		s := RandStringRunes(32)
		if _, dup := seen[s]; dup {
			t.Fatalf("call %d produced %q a second time", i, s)
		}
		seen[s] = struct{}{}
	}
}

// 256 is not a multiple of 62, so reducing a random byte modulo the charset
// length hands the first eight characters five chances out of 256 and the
// other fifty-four only four: 25 percent more draws. Rejection sampling is
// what removes that, and this is the test that fails without it.
func TestRandStringFromCharsetIsNotModuloBiased(t *testing.T) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const perSymbol = 4000
	total := len(charset) * perSymbol

	counts := map[rune]int{}
	for _, r := range RandStringFromCharset(total, charset) {
		counts[r]++
	}

	// A biased character would average 4844 draws against an expected 4000,
	// six standard deviations outside this band. Correct sampling sits well
	// inside it: the standard deviation at 4000 expected counts is near 63.
	low, high := perSymbol*9/10, perSymbol*11/10
	for _, r := range charset {
		if counts[r] < low || counts[r] > high {
			t.Errorf("%q drawn %d times, want between %d and %d", r, counts[r], low, high)
		}
	}
}
