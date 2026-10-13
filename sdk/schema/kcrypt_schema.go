package schema

// KcryptSchema represents the kcrypt block in the Kairos configuration. It
// configures how partitions listed in install.encrypted_partitions are
// encrypted and unlocked: locally against the TPM, or remotely through a
// kcrypt-challenger key management server.
type KcryptSchema struct {
	_             struct{}               `title:"Kairos Schema: Kcrypt block" description:"The kcrypt block configures partition encryption: local TPM settings, the optional challenger key management server, and the boot time encryption opt-in."`
	Challenger    KcryptChallengerSchema `json:"challenger,omitempty"`
	NVIndex       string                 `json:"nv_index,omitempty" description:"TPM NV index where the encoded key blob is stored for local TPM encryption."`
	CIndex        string                 `json:"c_index,omitempty" description:"TPM NV index used as the RSA certificate index for local TPM encryption."`
	TPMDevice     string                 `json:"tpm_device,omitempty" description:"Path of the TPM device to use, when the default does not fit."`
	EncryptOnBoot bool                   `json:"encrypt_on_boot,omitempty" description:"Encrypt the partitions listed in install.encrypted_partitions that are still plaintext on boot, before they are mounted. Encrypting destroys the current contents of those partitions. With this set, a partition that cannot be encrypted halts the boot instead of continuing on plaintext."`
}

// KcryptChallengerSchema represents the kcrypt.challenger block: the remote
// key management server that holds the encryption passphrases, used instead
// of the local TPM.
type KcryptChallengerSchema struct {
	_           struct{} `title:"Kairos Schema: Kcrypt challenger block" description:"The challenger block points kcrypt at a remote key management server instead of the local TPM."`
	Server      string   `json:"challenger_server,omitempty" description:"URL of the kcrypt-challenger key management server."`
	MDNS        bool     `json:"mdns,omitempty" description:"Discover the challenger server on the local network over mDNS."`
	Certificate string   `json:"certificate,omitempty" description:"Certificate used to verify the challenger server."`
}
