package schema

import (
	"encoding/json"
	"fmt"
	"sort"
)

// The schema the validator compiles does not set additionalProperties, so a
// top-level key it does not know is accepted and then dropped by the readers
// that only look at the keys they declare. A misspelled `uses` validates clean
// and silently takes no effect.
//
// Rejecting every unknown key would fix that, but it would also start failing
// configurations that are correct today: the Kubernetes blocks (`k3s`,
// `k0s-worker`), anything a provider or a bundle reads for itself, and the
// cloud-init keys that yip handles are all outside RootSchema. Whether those
// should warn or fail is a separate decision.
//
// A suggestion needs neither. An unknown key that is one or two edits away
// from a key the schema does declare is a misspelling of it with near
// certainty, and an unknown key that is not close to any of them says nothing
// and is left alone. So `uses` is reported against `users`, while `k3s` and
// `hostname` stay silent.

// suggestionDistance is the largest edit distance at which an unknown
// top-level key is reported as a probable misspelling of a schema key.
//
// It scales with the length of the key being checked. Two edits on a short
// key rewrite most of it: `ntp` is a cloud-init key and two edits from `p2p`,
// which is a coincidence rather than a typo. On a longer key two edits are a
// plausible slip, which is how `kube-vip` reaches `kubevip`.
func suggestionDistance(key string) int {
	if len([]rune(key)) < 5 {
		return 1
	}

	return 2
}

// SuggestUnknownKeys returns a warning for each top-level key of the parsed
// configuration that the schema does not declare but that is close enough to a
// key it does declare to be a misspelling of it. Keys that are not close to
// anything produce nothing, so a configuration that uses keys outside the
// schema on purpose stays quiet.
//
// This never reports an error. A suggestion is a hint about a key that is
// being ignored, not a judgement that the configuration is wrong.
//
// Only meaningful when kc.Source was parsed against RootSchema.
func (kc *KConfig) SuggestUnknownKeys() []string {
	present := topLevelKeys(kc.parsed)
	if len(present) == 0 {
		return nil
	}

	known, err := schemaTopLevelKeys(kc.schemaType)
	if err != nil || len(known) == 0 {
		// Without the schema's own key list there is nothing to compare
		// against. Validation reports a schema that cannot be built.
		return nil
	}

	var warnings []string
	for _, key := range present {
		nearest, ok := nearestKey(key, known)
		if !ok {
			continue
		}

		warnings = append(warnings, fmt.Sprintf(
			"unknown configuration key %q, which is ignored; did you mean %q?", key, nearest))
	}

	return warnings
}

// nearestKey returns the candidate closest to key, and whether it is close
// enough to suggest. A key that is itself one of the candidates has nothing to
// suggest, which is what makes this the only check a declared key needs.
//
// Candidates must be sorted. Two of them can sit at the same distance from the
// key, so the order decides which one is named, and it has to be the same one
// on every run.
func nearestKey(key string, candidates []string) (string, bool) {
	best := ""
	bestDistance := 0

	for _, candidate := range candidates {
		distance := editDistance(key, candidate)
		if distance == 0 {
			return "", false
		}
		if best == "" || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}

	if best == "" || bestDistance > suggestionDistance(key) {
		return "", false
	}

	return best, true
}

// topLevelKeys returns the sorted top-level keys of a parsed YAML document, or
// nothing when the document is not a mapping.
func topLevelKeys(parsed interface{}) []string {
	var keys []string

	switch doc := parsed.(type) {
	case map[string]interface{}:
		for k := range doc {
			keys = append(keys, k)
		}
	case map[interface{}]interface{}:
		for k := range doc {
			if s, ok := k.(string); ok {
				keys = append(keys, s)
			}
		}
	default:
		return nil
	}

	sort.Strings(keys)

	return keys
}

// schemaTopLevelKeys returns the sorted property names of the document
// GenerateSchema builds for the given type. Reading them back off the
// generated document rather than listing them here keeps them in step with the
// struct: a field added to RootSchema is a key the suggestion knows about, and
// one removed stops being suggested.
func schemaTopLevelKeys(schemaType interface{}) ([]string, error) {
	generated, err := GenerateSchema(schemaType, "")
	if err != nil {
		return nil, err
	}

	var document struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(generated), &document); err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(document.Properties))
	for k := range document.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys, nil
}

// editDistance returns the Levenshtein distance between a and b: the number of
// single-character insertions, deletions or substitutions that turn one into
// the other. It compares runes, so a multi-byte character counts as one edit
// rather than as its bytes.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}

	// Only the previous row of the matrix is ever read, so one row of
	// len(br)+1 is kept instead of the whole matrix.
	previous := make([]int, len(br)+1)
	current := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(ar); i++ {
		current[0] = i
		for j := 1; j <= len(br); j++ {
			substitution := previous[j-1]
			if ar[i-1] != br[j-1] {
				substitution++
			}
			current[j] = min(min(current[j-1]+1, previous[j]+1), substitution)
		}
		previous, current = current, previous
	}

	return previous[len(br)]
}
