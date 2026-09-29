package wizard

import (
	"fmt"
	"regexp"
	"strings"

	sdkExtensions "github.com/kairos-io/kairos/v4/sdk/types/extensions"
)

var (
	hostnameLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	// usernameRe is the portable shadow-utils rule.
	usernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	// Timezone and keymap are written into a command and a file, so only a
	// zoneinfo-shaped name and a keymap-shaped name get through, whether or
	// not the image offered a list to check them against.
	timezoneRe = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)*$`)
	keymapRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// Apply validates one step's values and returns the answers with that step
// folded in. On any error the answers come back unchanged, so a frontend can
// show the errors and keep what the operator had.
func Apply(steps []Step, a Answers, stepID string, values map[string]string) (Answers, []FieldError) {
	step, ok := StepByID(steps, stepID)
	if !ok {
		return a, []FieldError{{Message: fmt.Sprintf("unknown step %q", stepID)}}
	}
	out := a
	var errs []FieldError
	fail := func(field, format string, args ...any) {
		errs = append(errs, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	}
	get := func(id string) string { return strings.TrimSpace(values[id]) }

	switch stepID {
	case StepDisk:
		v := get(FieldDisk)
		switch {
		case v == "":
			fail(FieldDisk, "Pick a disk to install to.")
		case !offered(step.Fields[0], v):
			fail(FieldDisk, "%s is not one of the disks this machine has.", v)
		default:
			out.Disk = v
		}
	case StepUser:
		name, pw, confirm := get(FieldUsername), values[FieldPassword], values[FieldPassword+ConfirmSuffix]
		switch {
		case name == "":
			out.Username, out.PasswordHash = "", ""
		case !usernameRe.MatchString(name):
			fail(FieldUsername, "Use lowercase letters, digits, hyphens and underscores, starting with a letter, up to 32 characters.")
		case pw == "" && (a.PasswordHash == "" || a.Username != name):
			fail(FieldPassword, "Set a password for %s, or clear the username.", name)
		case pw != "" && pw != confirm:
			fail(FieldPassword, "The two passwords do not match.")
		case pw != "":
			hash, err := passwordHasher(pw)
			if err != nil {
				fail(FieldPassword, "The password could not be hashed: %v", err)
				break
			}
			out.Username, out.PasswordHash = name, hash
		default:
			out.Username = name
		}
	case StepSSHKeys:
		out.SSHKeys = lines(values[FieldSSHKeys])
	case StepHostname:
		v := get(FieldHostname)
		if v != "" && !validHostname(v) {
			fail(FieldHostname, "Use letters, digits and hyphens, dot-separated, and do not start or end a part with a hyphen.")
			break
		}
		out.Hostname = v
	case StepLocale:
		tz, km := get(FieldTimezone), get(FieldKeymap)
		if tz != "" && (!timezoneRe.MatchString(tz) || strings.Contains(tz, "..") || !offered(step.Fields[0], tz)) {
			fail(FieldTimezone, "%s is not a timezone this image knows.", tz)
		}
		if km != "" && (!keymapRe.MatchString(km) || !offered(step.Fields[1], km)) {
			fail(FieldKeymap, "%s is not a keyboard layout this image knows.", km)
		}
		out.Timezone, out.Keymap = tz, km
	case StepExtensions:
		picked := map[string]bool{}
		for _, v := range lines(values[FieldExtensions]) {
			if !offered(step.Fields[0], v) {
				fail(FieldExtensions, "%s is not an extension this installer offers.", v)
			}
			picked[v] = true
		}
		out.Extensions = nil
		// Offer order, so the rendered YAML does not depend on click order.
		for _, c := range step.Fields[0].Choices {
			if picked[c.Value] {
				out.Extensions = append(out.Extensions, sdkExtensions.Extension{Name: c.Value})
			}
		}
	case StepProvider:
		prov := map[string]any{}
		for _, f := range step.Fields {
			v := get(f.ID)
			if v == "" {
				v = f.IfEmpty
			}
			if v == "" {
				continue
			}
			var val any = v
			if f.Kind == KindBool {
				val = v == "true"
			}
			setPath(prov, f.ID, val)
		}
		out.Provider = prov
	case StepFinish:
		v := get(FieldFinish)
		if !offered(step.Fields[0], v) {
			fail(FieldFinish, "%s is not something the installer can do when it finishes.", v)
			break
		}
		out.FinishAction = v
	}

	if len(errs) > 0 {
		return a, errs
	}
	return out, nil
}

// offered reports whether v is one of f's choices. Only Choice and
// MultiChoice fields are checked: a Text field is what Steps falls back to
// when the image has no list to offer, and the charset checks above are its
// guard. A Choice with no choices, such as a disk step on a machine with no
// disks, accepts nothing.
func offered(f Field, v string) bool {
	if f.Kind != KindChoice && f.Kind != KindMultiChoice {
		return true
	}
	for _, c := range f.Choices {
		if c.Value == v {
			return true
		}
	}
	return false
}

func validHostname(v string) bool {
	if len(v) > 253 {
		return false
	}
	for _, label := range strings.Split(v, ".") {
		if !hostnameLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func lines(v string) []string {
	var out []string
	for _, l := range strings.Split(v, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// setPath sets a dot-separated section in m, creating the maps on the way.
func setPath(m map[string]any, path string, v any) {
	keys := strings.Split(path, ".")
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[keys[len(keys)-1]] = v
}
