// Package sshdfloor checks an effective sshd configuration against the
// minimum Kairos requires, whichever drop-in supplied the values.
//
// The ssh-hardening end-to-end test runs the DevSec ssh-baseline profile,
// which compares the effective config against one exact list per directive.
// That works only while a single drop-in owns the directive. Since
// hadron v0.5.3 the base image ships its own sshd policy in
// /etc/ssh/sshd_config.d/01-hadron-stig.conf and 02-hadron-crypto.conf,
// numbered below kairos-init's 05-kairos-hardening.conf and therefore
// winning every directive both files set (sshd_config is first value wins,
// in lexical order of the drop-in name). Six of the profile's controls then
// report the base image's values rather than the ones kairos-init ships.
//
// Which of the two policies should own those directives is open in
// kairos-io/kairos#5041. Until it is settled those six controls are waived
// in assets/ssh-baseline-waivers.yaml, and this package is what keeps the
// waiver from becoming a blind spot: it asserts the properties that must
// hold under either policy, rather than an exact list. A base image that
// re-enables CBC, an HMAC over MD5 or SHA-1, a SHA-1 key exchange or a DSA
// host key still turns the leg red.
package sshdfloor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// maxIdleSeconds bounds ClientAliveInterval * ClientAliveCountMax, the
// time sshd leaves an unresponsive session on the wire. Both policies in
// play are inside it: kairos-init sets 300 * 3, hadron v0.5.3 sets 600 * 1.
const maxIdleSeconds = 900

// maxAuthTries bounds MaxAuthTries. kairos-init sets 2, hadron v0.5.3
// sets 4. OpenSSH's own default is 6, which is above the bound, so an
// image that ships no policy at all is still caught.
const maxAuthTries = 4

// bannedAlgorithms are substrings that must not appear in any entry of
// the directive they are listed under. They are matched as substrings so
// that a newly named variant of a broken primitive is caught too.
var bannedAlgorithms = map[string][]string{
	"ciphers": {
		"-cbc",     // CBC modes, vulnerable to the 2008 plaintext recovery attack
		"3des",     // 64-bit block, Sweet32
		"arcfour",  // RC4
		"blowfish", // 64-bit block, Sweet32
		"cast128",  // 64-bit block, Sweet32
		"none",     // no encryption
	},
	"macs": {
		"md5",     // collision resistance long gone
		"sha1",    // SHA-1 collisions, 2017
		"umac-64", // 64-bit tag
		"ripemd",  // not reviewed for this use
		"none",    // no integrity protection
	},
	"kexalgorithms": {
		"sha1",                               // covers diffie-hellman-group14-sha1 and friends
		"diffie-hellman-group1-",             // 1024-bit group, Logjam
		"diffie-hellman-group-exchange-sha1", // redundant with sha1, listed for the error message
		"rsa1024",                            // 1024-bit RSA key transport
	},
	"hostkeyalgorithms": {
		"ssh-dss", // DSA, 1024-bit and fixed
		"ssh-rsa", // the SHA-1 signature algorithm, distinct from rsa-sha2-*
	},
	"pubkeyacceptedalgorithms": {
		"ssh-dss",
		"ssh-rsa",
	},
}

// Parse turns the output of `sshd -T` into a directive map. sshd -T emits
// one directive per line, lowercased, name and value separated by a single
// space, and repeats the name for directives that may appear more than once
// (hostkey is the one this package cares about). Repeats are collected in
// order.
func Parse(sshdT string) map[string][]string {
	out := map[string][]string{}
	for _, line := range strings.Split(sshdT, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, " ")
		if !found {
			// A directive with no value, for example "permitrootlogin"
			// on a build that does not support it. Record the presence.
			out[strings.ToLower(name)] = append(out[strings.ToLower(name)], "")
			continue
		}
		name = strings.ToLower(name)
		out[name] = append(out[name], strings.TrimSpace(value))
	}
	return out
}

// Check returns one message per violation of the floor, and an empty slice
// when the configuration is acceptable. A directive that is absent from the
// map is reported rather than skipped: `sshd -T` prints every directive the
// build supports, so absence means the build does not have it and the floor
// cannot be shown to hold.
func Check(cfg map[string][]string) []string {
	var problems []string

	for directive, banned := range bannedAlgorithms {
		values, ok := cfg[directive]
		if !ok {
			// pubkeyacceptedalgorithms is absent on OpenSSH < 8.2, where
			// it is spelled pubkeyacceptedkeytypes. Every other directive
			// here has been present since well before the 8.5 floor the
			// hardening stage already requires.
			if directive == "pubkeyacceptedalgorithms" {
				continue
			}
			problems = append(problems, fmt.Sprintf("%s is not reported by sshd -T, cannot verify it", directive))
			continue
		}
		for _, entry := range splitList(values) {
			for _, b := range banned {
				if strings.Contains(entry, b) {
					problems = append(problems,
						fmt.Sprintf("%s offers %q, which matches the banned primitive %q", directive, entry, b))
				}
			}
		}
	}

	problems = append(problems, checkHostKeys(cfg)...)
	problems = append(problems, checkLoginPolicy(cfg)...)

	// bannedAlgorithms is a map, so the loop above visits it in a random
	// order. Sort so a failing run prints the same list every time and two
	// runs can be diffed.
	sort.Strings(problems)

	return problems
}

// checkHostKeys replaces what the waived sshd-14 control asserted about
// which host key files sshd loads. The exact set is the base image's
// business, but an ed25519 key has to be there (it is the only algorithm
// every supported client and both policies agree on) and a DSA key must
// not be.
func checkHostKeys(cfg map[string][]string) []string {
	keys, ok := cfg["hostkey"]
	if !ok || len(keys) == 0 {
		return []string{"sshd -T reports no hostkey, cannot verify the host keys in use"}
	}
	var problems []string
	ed25519 := false
	for _, k := range keys {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "ed25519") {
			ed25519 = true
		}
		if strings.Contains(lower, "_dsa") {
			problems = append(problems, fmt.Sprintf("host key %q is DSA", k))
		}
	}
	if !ed25519 {
		problems = append(problems, fmt.Sprintf("no ed25519 host key among %v", keys))
	}
	return problems
}

// checkLoginPolicy replaces what the waived sshd-19 and sshd-36 controls
// asserted. Both policies pick different numbers; what matters is that
// neither leaves the OpenSSH default in place.
func checkLoginPolicy(cfg map[string][]string) []string {
	var problems []string

	tries, err := intValue(cfg, "maxauthtries")
	switch {
	case err != nil:
		problems = append(problems, err.Error())
	case tries < 1 || tries > maxAuthTries:
		problems = append(problems,
			fmt.Sprintf("maxauthtries is %d, want between 1 and %d", tries, maxAuthTries))
	}

	interval, ierr := intValue(cfg, "clientaliveinterval")
	countMax, cerr := intValue(cfg, "clientalivecountmax")
	switch {
	case ierr != nil:
		problems = append(problems, ierr.Error())
	case cerr != nil:
		problems = append(problems, cerr.Error())
	case interval <= 0:
		problems = append(problems,
			fmt.Sprintf("clientaliveinterval is %d, so an unresponsive session is never disconnected", interval))
	case countMax < 0:
		problems = append(problems, fmt.Sprintf("clientalivecountmax is %d", countMax))
	case interval*countMax > maxIdleSeconds:
		problems = append(problems,
			fmt.Sprintf("clientaliveinterval %d times clientalivecountmax %d is %ds, want at most %ds",
				interval, countMax, interval*countMax, maxIdleSeconds))
	}

	return problems
}

// splitList flattens the comma-separated algorithm lists sshd -T prints,
// lowercasing and dropping empty entries. A directive that legitimately
// repeats is handled by ranging over the outer slice too.
func splitList(values []string) []string {
	var out []string
	for _, v := range values {
		for _, entry := range strings.Split(v, ",") {
			entry = strings.ToLower(strings.TrimSpace(entry))
			if entry != "" {
				out = append(out, entry)
			}
		}
	}
	return out
}

func intValue(cfg map[string][]string, name string) (int, error) {
	values, ok := cfg[name]
	if !ok || len(values) == 0 {
		return 0, fmt.Errorf("%s is not reported by sshd -T, cannot verify it", name)
	}
	n, err := strconv.Atoi(strings.TrimSpace(values[0]))
	if err != nil {
		return 0, fmt.Errorf("%s is %q, which is not a number", name, values[0])
	}
	return n, nil
}
