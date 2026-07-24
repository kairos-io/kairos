package attestation

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"

	"github.com/google/go-attestation/attest"
	"github.com/google/go-tpm/tpm2"
	tpm "github.com/kairos-io/tpm-helpers"
)

// DefaultRSAPublicExponent is the standard RSA public exponent (also known as F4
// or 0x10001) used when the TPM doesn't provide an exponent value (i.e., when
// exponent is 0). This is the most commonly used RSA public exponent in practice.
const DefaultRSAPublicExponent = 65537

type AttestationRequest struct {
	TPMHash            string
	PCRs               map[int][]byte
	EKPEM              []byte
	DeferPCREnrollment bool
}

type Attestator interface {
	IssuePassphrase(ctx context.Context, req AttestationRequest) ([]byte, error)
}

type VerificationResult struct {
	AKPublic crypto.PublicKey // for internal checks/logging if needed by caller
	PCRs     map[int][]byte
}

type RemoteAttestationServer struct {
	attestator Attestator
}

func NewRemoteAttestationServer(attestator Attestator) *RemoteAttestationServer {
	return &RemoteAttestationServer{attestator: attestator}
}

func (s *RemoteAttestationServer) ParseInit(init *AttestationInit) (*attest.EK, *attest.AttestationParameters, error) {
	// Decode EK public from SPKI DER
	ekPub, err := x509.ParsePKIXPublicKey(init.EKPublic)
	if err != nil {
		return nil, nil, fmt.Errorf("parse EK SPKI: %w", err)
	}
	ek := &attest.EK{Public: ekPub}
	// Unmarshal AK params
	var params attest.AttestationParameters
	if err := json.Unmarshal(init.AKParams, &params); err != nil {
		return nil, nil, err
	}
	return ek, &params, nil
}

func (s *RemoteAttestationServer) GenerateChallenge(init *AttestationInit) (*AttestationChallenge, []byte, error) {
	ek, akParams, err := s.ParseInit(init)
	if err != nil {
		return nil, nil, err
	}
	ap := attest.ActivationParameters{EK: ek.Public, AK: *akParams}
	secret, ec, err := ap.Generate()
	if err != nil {
		return nil, nil, err
	}
	chBytes, err := json.Marshal(ec)
	if err != nil {
		return nil, nil, err
	}
	return &AttestationChallenge{EncryptedCredential: chBytes}, secret, nil
}

func (s *RemoteAttestationServer) VerifyProof(init *AttestationInit, proof *AttestationProof, expectedSecret []byte) (VerificationResult, error) {
	if !equalBytes(expectedSecret, proof.Secret) {
		return VerificationResult{}, fmt.Errorf("invalid secret")
	}

	// Get AK public from init's AK params
	_, akParams, err := s.ParseInit(init)
	if err != nil {
		return VerificationResult{}, err
	}
	// Convert AK public to crypto.PublicKey
	akPub, err := akPublicFromParams(akParams)
	if err != nil {
		return VerificationResult{}, err
	}

	// Parse and verify the PCR quote
	var pq struct {
		Quote struct {
			Version   string `json:"version"`
			Quote     []byte `json:"quote"`
			Signature []byte `json:"signature"`
		} `json:"quote"`
		PCRs map[int][]byte `json:"pcrs"`
	}
	if err := json.Unmarshal(proof.PCRQuote, &pq); err != nil {
		return VerificationResult{}, fmt.Errorf("unmarshaling PCR quote: %w", err)
	}

	// Verify the PCR quote signature and extract verified PCR values using tpm-helpers
	verifiedPCRs, err := tpm.VerifyPCRQuote(proof.PCRQuote, akPub)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("PCR quote verification failed: %w", err)
	}

	return VerificationResult{AKPublic: akPub, PCRs: verifiedPCRs}, nil
}

func (s *RemoteAttestationServer) IssuePassphrase(ctx context.Context, init *AttestationInit, proof *AttestationProof, expectedSecret []byte) ([]byte, error) {
	vr, err := s.VerifyProof(init, proof, expectedSecret)
	if err != nil {
		return nil, err
	}
	// Derive TPM hash from EK again for input to Attestator
	ek, _, err := s.ParseInit(init)
	if err != nil {
		return nil, err
	}
	tpmHash, err := ComputeTPMHashFromEK(ek)
	if err != nil {
		return nil, err
	}
	// Encode EK to PEM for the attestator
	ekPEM, err := EncodeEKToPEM(ek)
	if err != nil {
		return nil, fmt.Errorf("encoding EK to PEM: %w", err)
	}

	// Build request for Attestator
	req := AttestationRequest{
		TPMHash:            tpmHash,
		PCRs:               vr.PCRs,
		EKPEM:              ekPEM,
		DeferPCREnrollment: init.DeferPCREnrollment,
	}
	return s.attestator.IssuePassphrase(ctx, req)
}

// Helpers
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ComputeTPMHashFromEK(ek *attest.EK) (string, error) {
	spki, err := x509.MarshalPKIXPublicKey(ek.Public)
	if err != nil {
		return "", fmt.Errorf("marshal EK: %w", err)
	}
	sum := sha256.Sum256(spki)
	return fmt.Sprintf("%x", sum[:]), nil
}

func akPublicFromParams(params *attest.AttestationParameters) (crypto.PublicKey, error) {
	pub, err := tpm2.Unmarshal[tpm2.TPMTPublic](params.Public)
	if err != nil {
		return nil, fmt.Errorf("unmarshal TPM public: %w", err)
	}
	switch pub.Type {
	case tpm2.TPMAlgRSA:
		rsaParms, err := pub.Parameters.RSADetail()
		if err != nil {
			return nil, fmt.Errorf("rsa params: %w", err)
		}
		rsaUnique, err := pub.Unique.RSA()
		if err != nil {
			return nil, fmt.Errorf("rsa unique: %w", err)
		}
		n := new(big.Int).SetBytes(rsaUnique.Buffer)
		e := int(rsaParms.Exponent)
		if e == 0 {
			e = DefaultRSAPublicExponent
		}
		return &rsa.PublicKey{N: n, E: e}, nil

	case tpm2.TPMAlgECC:
		eccParms, err := pub.Parameters.ECCDetail()
		if err != nil {
			return nil, fmt.Errorf("ecc params: %w", err)
		}
		eccUnique, err := pub.Unique.ECC()
		if err != nil {
			return nil, fmt.Errorf("ecc unique: %w", err)
		}
		var curve elliptic.Curve
		switch eccParms.CurveID {
		case tpm2.TPMECCNistP256:
			curve = elliptic.P256()
		case tpm2.TPMECCNistP384:
			curve = elliptic.P384()
		case tpm2.TPMECCNistP521:
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("unsupported curve: %v", eccParms.CurveID)
		}
		x := new(big.Int).SetBytes(eccUnique.X.Buffer)
		y := new(big.Int).SetBytes(eccUnique.Y.Buffer)
		return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
	default:
		return nil, fmt.Errorf("unsupported key type: %v", pub.Type)
	}
}

// EncodeEKToPEM encodes an EK to PEM format
func EncodeEKToPEM(ek *attest.EK) ([]byte, error) {
	if ek.Certificate != nil {
		// If we have a certificate, encode it as PEM
		pemBlock := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: ek.Certificate.Raw,
		}
		return pem.EncodeToMemory(pemBlock), nil
	}

	// Otherwise, encode the public key as PEM
	data, err := x509.MarshalPKIXPublicKey(ek.Public)
	if err != nil {
		return nil, fmt.Errorf("marshaling EK public key: %w", err)
	}

	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: data,
	}
	return pem.EncodeToMemory(pemBlock), nil
}
