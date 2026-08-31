package attestation

import (
	"crypto/x509"
	"encoding/json"
)

// AttestationInit is sent by the client at the start of the flow.
// EKPublic is the SPKI-encoded public key (PKIX) of the EK.
// AKParams are the attestation parameters of the transient AK.
// DeferPCREnrollment indicates that PCR values should be set to empty strings
// for later enrollment (used in livecd mode where PCRs will differ after installation).
type AttestationInit struct {
	EKPublic           []byte          `json:"ek_public"`
	AKParams           json.RawMessage `json:"ak_params"` // marshaled attest.AttestationParameters
	DeferPCREnrollment bool            `json:"defer_pcr_enrollment,omitempty"`
}

// AttestationChallenge is returned by the server: a JSON-encoded attest.EncryptedCredential
type AttestationChallenge struct {
	EncryptedCredential json.RawMessage `json:"challenge"`
}

// AttestationProof is returned by the client: secret + PCR quote payload
// PCRQuote is a JSON payload: { quote:{version,quote,signature}, pcrs:{index:value} }
type AttestationProof struct {
	Secret   []byte          `json:"secret"`
	PCRQuote json.RawMessage `json:"pcr_quote"`
}

// AttestationResponse is the final response from the server after verifying the proof.
// It contains either the passphrase (on success) or an error message (on failure).
// If Error is non-empty, the attestation failed and Passphrase should be ignored.
type AttestationResponse struct {
	Passphrase []byte `json:"passphrase,omitempty"` // The decryption passphrase (on success)
	Error      string `json:"error,omitempty"`      // Error message (on failure)
}

// EncodePublicKeyToSPKI returns DER-encoded SubjectPublicKeyInfo for a public key
func EncodePublicKeyToSPKI(pub interface{}) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}
