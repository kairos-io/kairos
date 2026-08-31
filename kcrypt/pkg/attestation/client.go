package attestation

import (
	"encoding/json"

	"github.com/google/go-attestation/attest"
	tpmhelpers "github.com/kairos-io/tpm-helpers"
)

type RemoteAttestationClient struct {
	akm *tpmhelpers.AKManager
}

func NewRemoteAttestationClient(opts ...tpmhelpers.Option) (*RemoteAttestationClient, error) {
	akm, err := tpmhelpers.NewAKManager(opts...)
	if err != nil {
		return nil, err
	}
	return &RemoteAttestationClient{akm: akm}, nil
}

func (c *RemoteAttestationClient) Close() error {
	return c.akm.Close()
}

// CreateInit gathers EK and creates a transient AK, returning AttestationInit
func (c *RemoteAttestationClient) CreateInit() (*AttestationInit, error) {
	// Get attestation params of cached AK
	params, err := c.akm.AKParams()
	if err != nil {
		return nil, err
	}

	// Get EK
	ek, err := c.akm.GetEK()
	if err != nil {
		return nil, err
	}

	// Marshal AK params
	akParamsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	ekSPKI, err := EncodePublicKeyToSPKI(ek.Public)
	if err != nil {
		return nil, err
	}

	return &AttestationInit{
		EKPublic: ekSPKI,
		AKParams: akParamsBytes,
	}, nil
}

// CreateInitDeferredEnrollment creates an AttestationInit with PCR enrollment deferred
// Used in livecd mode where PCR values will differ after installation
func (c *RemoteAttestationClient) CreateInitDeferredEnrollment() (*AttestationInit, error) {
	init, err := c.CreateInit()
	if err != nil {
		return nil, err
	}
	init.DeferPCREnrollment = true
	return init, nil
}

// HandleChallenge takes an AttestationChallenge, activates credential, and returns AttestationProof
// The client selects PCRs.
func (c *RemoteAttestationClient) HandleChallenge(challenge *AttestationChallenge, pcrs []int) (*AttestationProof, error) {
	// Activate credential to get secret
	// Unmarshal EncryptedCredential into the right type
	var enc attest.EncryptedCredential
	if err := json.Unmarshal(challenge.EncryptedCredential, &enc); err != nil {
		return nil, err
	}
	secret, err := c.akm.ActivateCredential(&enc)
	if err != nil {
		return nil, err
	}

	// Generate PCR quote
	pcrQuote, err := c.akm.GeneratePCRQuote(pcrs)
	if err != nil {
		return nil, err
	}

	return &AttestationProof{
		Secret:   secret,
		PCRQuote: pcrQuote,
	}, nil
}
