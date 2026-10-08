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

	switch stepID {
	case StepDisk:
		errs = applyDisk(step, &out, values)
	case StepUser:
		errs = applyUser(a, &out, values)
	case StepSSHKeys:
		out.SSHKeys = lines(values[FieldSSHKeys])
	case StepHostname:
		errs = applyHostname(&out, values)
	case StepLocale:
		errs = applyLocale(step, &out, values)
	case StepExtensions:
		errs = applyExtensions(step, &out, values)
	case StepProvider:
		applyProvider(step, &out, values)
	case StepFinish:
		errs = applyFinish(step, &out, values)
	}

	if len(errs) > 0 {
		return a, errs
	}
	return out, nil
}

// get returns the trimmed value of one field.
func get(values map[string]string, id string) string { return strings.TrimSpace(values[id]) }

// fieldErr builds a FieldError for one field.
func fieldErr(field, format string, args ...any) []FieldError {
	return []FieldError{{Field: field, Message: fmt.Sprintf(format, args...)}}
}

func applyDisk(step Step, out *Answers, values map[string]string) []FieldError {
	v := get(values, FieldDisk)
	switch {
	case v == "":
		return fieldErr(FieldDisk, "Pick a disk to install to.")
	case !offered(step.Fields[0], v):
		return fieldErr(FieldDisk, "%s is not one of the disks this machine has.", v)
	}
	out.Disk = v
	return nil
}

// applyUser folds the user step into out. prev is the answers before the
// step, used to keep an existing password when the username is unchanged.
func applyUser(prev Answers, out *Answers, values map[string]string) []FieldError {
	name, pw, confirm := get(values, FieldUsername), values[FieldPassword], values[FieldPassword+ConfirmSuffix]
	switch {
	case name == "":
		out.Username, out.PasswordHash = "", ""
	case !usernameRe.MatchString(name):
		return fieldErr(FieldUsername, "Use lowercase letters, digits, hyphens and underscores, starting with a letter or an underscore, up to 32 characters.")
	case pw == "" && (prev.PasswordHash == "" || prev.Username != name):
		return fieldErr(FieldPassword, "Set a password for %s, or clear the username.", name)
	case pw != "" && pw != confirm:
		return fieldErr(FieldPassword, "The two passwords do not match.")
	case pw != "":
		hash, err := passwordHasher(pw)
		if err != nil {
			return fieldErr(FieldPassword, "The password could not be hashed: %v", err)
		}
		out.Username, out.PasswordHash = name, hash
	default:
		out.Username = name
	}
	return nil
}

func applyHostname(out *Answers, values map[string]string) []FieldError {
	v := get(values, FieldHostname)
	if v != "" && !validHostname(v) {
		return fieldErr(FieldHostname, "Use letters, digits and hyphens, dot-separated, and do not start or end a part with a hyphen.")
	}
	out.Hostname = v
	return nil
}

func applyLocale(step Step, out *Answers, values map[string]string) []FieldError {
	var errs []FieldError
	tz, km := get(values, FieldTimezone), get(values, FieldKeymap)
	// The timezone field is absent on an image with no time zone database,
	// where nothing could honour the answer, so a value submitted for it is
	// refused rather than written into a link that would dangle.
	tzField, tzOffered := fieldByID(step, FieldTimezone)
	switch {
	case tz != "" && !tzOffered:
		errs = append(errs, fieldErr(FieldTimezone, "This image ships no time zone database, so the timezone cannot be set.")...)
	case tz != "" && (!validTimezone(tz) || !offered(tzField, tz)):
		errs = append(errs, fieldErr(FieldTimezone, "%s is not a timezone this image knows.", tz)...)
	}
	kmField, _ := fieldByID(step, FieldKeymap)
	if km != "" && (!validKeymap(km) || !offered(kmField, km)) {
		errs = append(errs, fieldErr(FieldKeymap, "%s is not a keyboard layout this image knows.", km)...)
	}
	out.Timezone, out.Keymap = tz, km
	return errs
}

func applyExtensions(step Step, out *Answers, values map[string]string) []FieldError {
	var errs []FieldError
	picked := map[string]bool{}
	for _, v := range lines(values[FieldExtensions]) {
		if !offered(step.Fields[0], v) {
			errs = append(errs, fieldErr(FieldExtensions, "%s is not an extension this installer offers.", v)...)
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
	return errs
}

// applyProvider writes only what the operator turned on. A section behind a
// no gate writes nothing, IfEmpty included, and a Bool that is off is left
// out rather than written as false: the plugin's defaults then stay what
// they were before the installer asked, as they did when the old terminal
// installer's page was never opened.
func applyProvider(step Step, out *Answers, values map[string]string) {
	prov := map[string]any{}
	for _, f := range step.Fields {
		if strings.HasSuffix(f.ID, AskSuffix) {
			continue
		}
		if gated(step, f.ID) && get(values, f.ID+AskSuffix) != "true" {
			continue
		}
		v := get(values, f.ID)
		if f.Kind == KindBool {
			if v == "true" {
				setPath(prov, f.ID, true)
			}
			continue
		}
		if v == "" {
			v = f.IfEmpty
		}
		if v == "" {
			continue
		}
		setPath(prov, f.ID, v)
	}
	out.Provider = prov
}

// gated reports whether the step asks a yes or no in front of field id.
func gated(step Step, id string) bool {
	for _, f := range step.Fields {
		if f.ID == id+AskSuffix {
			return true
		}
	}
	return false
}

func applyFinish(step Step, out *Answers, values map[string]string) []FieldError {
	v := get(values, FieldFinish)
	if !offered(step.Fields[0], v) {
		return fieldErr(FieldFinish, "%s is not something the installer can do when it finishes.", v)
	}
	out.FinishAction = v
	return nil
}

// fieldByID finds one of a step's fields. A step does not always carry the
// same fields: the locale step drops the timezone on an image that has no
// time zone database.
func fieldByID(s Step, id string) (Field, bool) {
	for _, f := range s.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return Field{}, false
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

// validTimezone and validKeymap are the charset checks Apply and Render
// share: a zoneinfo-shaped name, and a keymap-shaped one.
func validTimezone(v string) bool { return timezoneRe.MatchString(v) && !strings.Contains(v, "..") }
func validKeymap(v string) bool   { return keymapRe.MatchString(v) }

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
