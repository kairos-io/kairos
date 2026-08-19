package payload

type Data struct {
	Passphrase  string `json:"passphrase"`
	Error       string `json:"error"`
	GeneratedBy string `json:"generated_by"`
}

func (d Data) HasError() bool {
	return d.Error != ""
}

func (d Data) HasPassphrase() bool {
	return d.Passphrase != ""
}

func (d Data) HasBeenGenerated() bool {
	return d.GeneratedBy != ""
}
