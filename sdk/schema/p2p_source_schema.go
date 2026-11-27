package schema

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// P2PSourceSchema configures the EXPERIMENTAL p2p image source: how this node
// shares its active, passive and recovery images with its peers and how it
// fetches them from p2p:// sources. Nothing runs unless experimental is true.
//
// Not to be confused with the p2p block, which is the Kairos p2p network.
type P2PSourceSchema struct {
	Enabled         bool     `json:"enabled,omitempty" description:"Run the seeding service that offers this node's images to its peers. Off by default"`
	Experimental    bool     `json:"experimental,omitempty" description:"Acknowledge that the p2p image source is experimental. Required for the seeding service and for p2p:// sources"`
	Bind            string   `json:"bind,omitempty" description:"Local IPv4 address to listen on. Defaults to the first non-loopback address" examples:"[\"10.0.0.2\"]"`
	Port            int      `json:"port,omitempty" minimum:"1" maximum:"65535" description:"Port to listen on. Defaults to 42069"`
	Allow           []string `json:"allow,omitempty" description:"Networks peers must belong to, in CIDR notation. Defaults to the peers named in the source, or the local subnet when they are discovered" examples:"[\"10.0.0.0/24\"]"`
	UploadLimit     int      `json:"upload-limit,omitempty" minimum:"0" description:"Upload limit in KiB/s, 0 means the default of 65536 (64 MiB/s)"`
	MaxPeers        int      `json:"max-peers,omitempty" minimum:"0" description:"Maximum number of requests served at the same time"`
	Insecure        bool     `json:"insecure,omitempty" description:"DANGEROUS. Allow a p2p:// source without a ?pin= or ?sha256= pin. Any host that can reach this node can then make it install any image"`
	MaxImageSize    int      `json:"max-image-size,omitempty" minimum:"0" description:"Largest image accepted, in MiB. Defaults to 16384"`
	Timeout         int      `json:"timeout,omitempty" minimum:"0" description:"Longest a whole p2p download may take, in minutes. Defaults to 360"`
	DisableAnnounce bool     `json:"disable-announce,omitempty" description:"Do not announce this node with mDNS (it is announced by default)"`
}

var _ jsonschemago.Preparer = P2PSourceSchema{}

// PrepareJSONSchema closes the block to keys it does not declare, a typo here
// would otherwise look like a working setting.
func (P2PSourceSchema) PrepareJSONSchema(schema *jsonschemago.Schema) error {
	schema.WithAdditionalProperties(*(&jsonschemago.SchemaOrBool{}).WithTypeBoolean(false))

	return nil
}
