package builder

import "strings"

// RedactLine replaces every occurrence of each value in secrets with
// "<redacted>" and returns the result. Values shorter than eight characters
// are skipped to avoid replacing unrelated text that happens to contain
// them - a three-character password matches too much log noise to be
// worth hiding, and a secret that short is trivially discoverable
// anyway. Redaction is verbatim only: a base64-, JSON-escaped, or partial
// appearance of the same value still passes through, which the field doc
// on BuildOptions.LogRedactValues calls out as best-effort.
func RedactLine(line string, secrets []string) string {
	if len(secrets) == 0 {
		return line
	}
	for _, s := range secrets {
		if len(s) < 8 {
			continue
		}
		line = strings.ReplaceAll(line, s, "<redacted>")
	}
	return line
}
