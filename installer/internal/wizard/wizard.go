// Package wizard is the installer's questions, as data.
//
// Every frontend of the installer asks the same things: the terminal UI and
// the web UI render the steps this package returns, with one widget per field
// kind, and both hand the answers back through Apply. The answers become a
// cloud-config through Render, and every cloud-config an install runs with,
// generated, edited by hand or supplied by an MCP caller, goes through
// Finalize last. Nothing here draws a screen or runs the agent.
package wizard

// Kind is how a field is asked. A frontend must have a widget for every Kind,
// and each frontend has a test that fails when one is missing.
type Kind string

const (
	KindText        Kind = "text"
	KindPassword    Kind = "password"
	KindChoice      Kind = "choice"
	KindMultiChoice Kind = "multichoice"
	KindBool        Kind = "bool"
	KindList        Kind = "list"
)

// Kinds lists every Kind, for the frontends' drift tests.
var Kinds = []Kind{KindText, KindPassword, KindChoice, KindMultiChoice, KindBool, KindList}

// Choice is one value a Choice or MultiChoice field offers.
type Choice struct {
	Value  string `json:"value"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// Field is one question.
//
// Values travel as strings in both directions: a Bool is "true" or "false",
// and a List or MultiChoice is its entries joined by newlines. A Password
// field is sent as its ID and its ID with ConfirmSuffix.
type Field struct {
	ID          string   `json:"id"`
	Kind        Kind     `json:"kind"`
	Label       string   `json:"label"`
	Help        string   `json:"help,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Default     string   `json:"default,omitempty"`
	// IfEmpty is the value used when the field is submitted empty. It comes
	// from provider prompts.
	IfEmpty  string   `json:"if_empty,omitempty"`
	Required bool     `json:"required,omitempty"`
	Choices  []Choice `json:"choices,omitempty"`
	// Confirm is a warning the frontend must show, with {value} replaced by
	// the chosen value, and have the operator acknowledge before the install
	// starts. The disk field carries one.
	Confirm string `json:"confirm,omitempty"`
}

// ConfirmSuffix names the second input of a Password field.
const ConfirmSuffix = "_confirm"

// Step is one screen of questions.
type Step struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Help     string  `json:"help,omitempty"`
	// Notice is something the operator should know about this step's
	// choices, such as "no catalog could be read".
	Notice   string  `json:"notice,omitempty"`
	Optional bool    `json:"optional,omitempty"`
	Fields   []Field `json:"fields"`
}

// FieldError is a rejected answer, worded for the operator.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Step IDs, in the order Steps returns them.
const (
	StepDisk       = "disk"
	StepUser       = "user"
	StepSSHKeys    = "ssh_keys"
	StepHostname   = "hostname"
	StepLocale     = "locale"
	StepExtensions = "extensions"
	StepProvider   = "provider"
	StepFinish     = "finish"
)

// Field IDs of the fixed steps.
const (
	FieldDisk       = "disk"
	FieldUsername   = "username"
	FieldPassword   = "password"
	FieldSSHKeys    = "ssh_keys"
	FieldHostname   = "hostname"
	FieldTimezone   = "timezone"
	FieldKeymap     = "keymap"
	FieldExtensions = "extensions"
	FieldFinish     = "finish_action"
)

// Finish actions. The empty string means the machine stays on the live
// system, which is what the agent does with neither flag.
const (
	FinishReboot   = "reboot"
	FinishPoweroff = "poweroff"
)
