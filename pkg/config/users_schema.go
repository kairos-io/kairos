package config

type UserSchema struct {
	Name              string   `json:"name,omitempty" pattern:"([a-z_][a-z0-9_]{0,30})" required:"true" example:"kairos"`
	Groups            string   `json:"groups,omitempty" example:"admin"`
	LockPasswd        bool     `json:"lockPasswd,omitempty" example:"true"`
	Passwd            string   `json:"passwd,omitempty" example:"kairos"`
	SSHAuthorizedKeys []string `json:"ssh_authorized_keys,omitempty" examples:"[\"github:USERNAME\",\"ssh-ed25519 AAAF00BA5\"]"`
}
