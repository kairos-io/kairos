package strings

import (
	"crypto/rand"
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// LetterCharset is the alphabet RandStringRunes draws from.
const LetterCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// RandStringFromCharset returns n characters drawn uniformly at random from
// charset, read from the operating system's cryptographic generator. Use it
// for anything an attacker must not be able to guess: passphrases, one-time
// passwords, service identifiers.
//
// Selection rejects the byte values that would wrap when reduced modulo the
// charset length, so a charset whose length does not divide 256 does not make
// its first characters more likely than the rest. With 62 characters a plain
// modulo would hand the first eight of them 25 percent more draws than the
// other 54.
//
// It panics if the operating system cannot supply randomness, which is also
// what crypto/rand does on its own: returning a guessable string instead
// would hide the failure in the one place that cannot tolerate it.
func RandStringFromCharset(n int, charset string) string {
	if n <= 0 || len(charset) == 0 {
		return ""
	}

	// The largest multiple of len(charset) that fits in a byte. Values at or
	// above it are drawn again.
	limit := 256 - (256 % len(charset))

	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			panic("kairos: no randomness available: " + err.Error())
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, charset[int(b)%len(charset)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out)
}

// RandStringRunes returns n random letters, upper and lower case.
func RandStringRunes(n int) string {
	return RandStringFromCharset(n, LetterCharset)
}

func ListOutput(rels []string, output string) []string {
	switch strings.ToLower(output) {
	case "yaml":
		d, _ := yaml.Marshal(rels)
		return []string{string(d)}
	case "json":
		d, _ := json.Marshal(rels)
		return []string{string(d)}
	default:
		return rels
	}
}
