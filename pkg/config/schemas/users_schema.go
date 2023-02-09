package config

// UserSchema represents the users block in the Kairos configuration. It allows the creation of users in the system.
type UserSchema struct {
	_                 struct{} `title:"Kairos Schema: Users block" description:"The users block allows you to create users in the system."`
	Name              string   `json:"name,omitempty" pattern:"([a-z_][a-z0-9_]{0,30})" required:"true" example:"kairos"`
	Passwd            string   `json:"passwd,omitempty" example:"kairos"`
	LockPasswd        bool     `json:"lockPasswd,omitempty" example:"true"`
	Groups            string   `json:"groups,omitempty" example:"admin"`
	SSHAuthorizedKeys []string `json:"ssh_authorized_keys,omitempty" examples:"[\"github:USERNAME\",\"ssh-ed25519 AAAF00BA5\"]"`
}
