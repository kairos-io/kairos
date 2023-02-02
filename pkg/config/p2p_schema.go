package config

type P2P struct {
	DisableDHT bool `json:"disable_dht,omitempty" default:"true"`
	NetworkTokenControlFlow
}

type NetworkTokenControlFlow struct{}

type EmptyNetworkToken struct {
	NetworkToken string `json:"network_token" const:""`
}

type PresentNetworkToken struct {
	NetworkToken string `json:"network_token,omitempty" requried:"true" minLength:"1"`
}

type DisabledAuto struct {
	Auto struct {
		Enable bool `json:"enable,omitempty" const:"false"`
	} `json:"auto"`
}

func (NetworkTokenControlFlow) JSONSchemaIf() interface{} {
	return DisabledAuto{}
}

func (NetworkTokenControlFlow) JSONSchemaThen() interface{} {
	return EmptyNetworkToken{}
}

func (NetworkTokenControlFlow) JSONSchemaElse() interface{} {
	return PresentNetworkToken{}
}
