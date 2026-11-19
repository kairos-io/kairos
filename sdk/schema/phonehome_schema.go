package schema

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// PhonehomeSchema represents the phonehome block in the Kairos configuration.
// It connects the node to an AuroraBoot fleet server.
//
// The keys mirror agent/internal/phonehome.Config, which is where the block is
// read. A key that is not here is a key no validation can report on, so it
// has to stay in step with that struct.
//
// The durations are strings matching what time.ParseDuration accepts:
// gopkg.in/yaml.v3, which decodes the block, reads a time.Duration only from
// such a string and rejects a bare number.
type PhonehomeSchema struct {
	_                             struct{}           `title:"Kairos Schema: Phonehome block" description:"The phonehome block connects the node to an AuroraBoot fleet server."`
	URL                           string             `json:"url,omitempty" description:"URL of the fleet server. Phone-home is off when it is empty" examples:"[\"https://auroraboot.example.com\"]"`
	RegistrationToken             string             `json:"registration_token,omitempty" description:"Token the node registers with"`
	Group                         string             `json:"group,omitempty" description:"Group the node joins on registration"`
	Labels                        map[string]string  `json:"labels,omitempty" description:"Labels the node registers with"`
	HeartbeatInterval             string             `json:"heartbeat_interval,omitempty" pattern:"^[-+]?(0|(([0-9]+(\\.[0-9]*)?|\\.[0-9]+)(ns|us|µs|μs|ms|s|m|h))+)$" description:"Interval between heartbeats, as a Go duration" examples:"[\"30s\"]"`
	ReconnectBackoff              string             `json:"reconnect_backoff,omitempty" pattern:"^[-+]?(0|(([0-9]+(\\.[0-9]*)?|\\.[0-9]+)(ns|us|µs|μs|ms|s|m|h))+)$" description:"Delay before reconnecting to the fleet server, as a Go duration" examples:"[\"10s\"]"`
	ArtifactDownloadRetries       int                `json:"artifact_download_retries,omitempty" minimum:"0" description:"Additional attempts for an artifact download that fails on the network or with an HTTP 5xx response. 0 uses the default"`
	ArtifactDownloadRetryInterval string             `json:"artifact_download_retry_interval,omitempty" pattern:"^[-+]?(0|(([0-9]+(\\.[0-9]*)?|\\.[0-9]+)(ns|us|µs|μs|ms|s|m|h))+)$" description:"Delay between artifact download attempts, as a Go duration. Empty uses the default" examples:"[\"5s\"]"`
	AllowedCommands               []PhonehomeCommand `json:"allowed_commands,omitempty" description:"Remote commands the node runs. Unset allows upgrade, upgrade-recovery, reboot and unregister; an empty list denies every command"`
}

var _ jsonschemago.Preparer = PhonehomeSchema{}

// PrepareJSONSchema closes the block to keys it does not declare. A misspelled
// key is otherwise dropped without a word: allowed_commands spelled wrong
// leaves the default command policy in place, which allows more than a user
// who meant to allow only reboot asked for.
func (PhonehomeSchema) PrepareJSONSchema(schema *jsonschemago.Schema) error {
	schema.WithAdditionalProperties(*(&jsonschemago.SchemaOrBool{}).WithTypeBoolean(false))

	return nil
}

// PhonehomeCommand is the name of a remote command a phone-home node can be
// allowed to run. The names are the ones agent/internal/phonehome dispatches.
type PhonehomeCommand string

var _ jsonschemago.Enum = PhonehomeCommand("")

// Enum lists the command names the agent knows.
func (PhonehomeCommand) Enum() []interface{} {
	return []interface{}{
		"exec",
		"upgrade",
		"upgrade-recovery",
		"reset",
		"apply-cloud-config",
		"reboot",
		"unregister",
		"extension",
	}
}
