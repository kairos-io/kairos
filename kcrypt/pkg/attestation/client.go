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

// CreateInit gathers EK and creates a transient AK, returning Init
func (c *RemoteAttestationClient) CreateInit() (*Init, error) {
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

	return &Init{
		EKPublic: ekSPKI,
		AKParams: akParamsBytes,
	}, nil
}

// CreateInitDeferredEnrollment creates an Init with PCR enrollment deferred
// Used in livecd mode where PCR values will differ after installation
func (c *RemoteAttestationClient) CreateInitDeferredEnrollment() (*Init, error) {
	init, err := c.CreateInit()
	if err != nil {
		return nil, err
	}
	init.DeferPCREnrollment = true
	return init, nil
}

// HandleChallenge takes an Challenge, activates credential, and returns Proof
// The client selects PCRs.
func (c *RemoteAttestationClient) HandleChallenge(challenge *Challenge, pcrs []int) (*Proof, error) {
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

	return &Proof{
		Secret:   secret,
		PCRQuote: pcrQuote,
	}, nil
}
