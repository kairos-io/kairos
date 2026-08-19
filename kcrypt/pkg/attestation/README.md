## Attestation Package

This package implements a clean, close-circle API for the kcrypt-challenger remote attestation flow. It separates client and server responsibilities and defines the wire messages used between them, so consumers can simply pass bytes from one step to the next and only check for errors in between.

Important: The client decides which PCRs to include/enroll/validate. The server validates according to its selective enrollment policy. Nonces are not used in this flow (sequential, authenticated channel).

### High-Level Flow

Client (node with TPM):
1) CreateInit → send to server
2) Receive AttestationChallenge
3) HandleChallenge → send AttestationProof to server

Server (kcrypt-challenger):
1) ParseInit
2) GenerateChallenge (EK+AK only; PCRs not needed here)
3) VerifyProof (secret + PCR quote signature verification + PCR consistency verification) → proceed with selective enrollment and passphrase release

### Wire Messages (owned by this package)
- AttestationInit: EK (PEM/DER), AK AttestationParameters, optional AK public, DeferPCREnrollment flag (for LiveCD mode)
- AttestationChallenge: EncryptedCredential (no PCRs)
- AttestationProof: Secret (credential activation), PCRQuote (JSON with quote + selected PCR values)

### Client API
- NewRemoteAttestationClient(opts ...tpm.Option) (*RemoteAttestationClient, error)
- Close() error
- CreateInit() (*AttestationInit, error)
- CreateInitDeferredEnrollment() (*AttestationInit, error)  // For LiveCD mode
- HandleChallenge(challenge *AttestationChallenge, pcrs []int) (*AttestationProof, error)

Internally, the client uses transient AKs (no persistent AK storage). It sends EK + AK attestation data in AttestationInit, and later returns the secret and PCR quote in AttestationProof.

**LiveCD Mode Support:**
- `CreateInit()`: Normal enrollment - PCRs will be enrolled with current values
- `CreateInitDeferredEnrollment()`: LiveCD mode - sets `DeferPCREnrollment: true`, PCRs stored as empty strings for later enrollment after installation

### Server API
- NewRemoteAttestationServer(attestator Attestator) *RemoteAttestationServer
- ParseInit(initBytes []byte) (ek *attest.EK, akParams *attest.AttestationParameters, err error)
- GenerateChallenge(initBytes []byte) (challengeBytes []byte, secret []byte, err error)
- VerifyProof(initBytes []byte, proofBytes []byte, expectedSecret []byte) (VerificationResult, error)
- IssuePassphrase(initBytes []byte, proofBytes []byte, expectedSecret []byte) ([]byte, error)

The server accepts AttestationInit, generates an EncryptedCredential (challenge) bound to EK+AK params (no PCRs), and later verifies the secret and the PCR quote signature using the AK public contained within the attestation parameters. The server also verifies that the provided PCR values are consistent with the TPM quote to ensure they are cryptographically bound to the quote. After verification, it delegates policy/enrollment to the injected Attestator and returns a passphrase or error.

### Selective Enrollment
This package does not implement enrollment. The server injects an Attestator which receives final verified attestation data and decides enrollment/validation and passphrase issuance. Typical policies (for reference):
- Empty value: accept any, update stored value (re-enrollment)
- Set value: enforce exact match (strict)
- Omitted: skip entirely

**Note on Initial Enrollment Scenarios:**
The Attestator implementation (e.g., ChallengerAttestator in pkg/challenger/) may support additional enrollment scenarios when a SealedVolume exists without attestation data:

1. **Static passphrase setup (recommended)**:
   - Operator creates: SealedVolume (no attestation) + Secret (pre-defined passphrase) + partition references Secret
   - System learns: EK, AK, all PCRs via TOFU on first connection
   - Secret: Pre-defined, controlled by operator

2. **Secret reuse**:
   - Operator recreates SealedVolume (no attestation) after deletion, partition references existing Secret
   - System learns: EK, AK, all PCRs via TOFU
   - Secret: Reused from previous enrollment

3. **Deferred TOFU (edge case, not recommended)**:
   - Operator creates: SealedVolume (no attestation) + no Secret reference in partition
   - System creates: Secret (auto-generated passphrase) + learns EK, AK, all PCRs
   - Secret: Auto-generated, operator has no control
   - ⚠️ WARNING: This is unusual. If you want TOFU, let the system create the entire SealedVolume. If you pre-create a SealedVolume, you probably want to control the passphrase (scenario 1).

4. **Partial attestation data (selective enrollment)**:
   - Operator creates: SealedVolume with specific PCR entries (empty or set) + Secret
   - System tracks: Only specified PCRs, omitted PCRs are ignored entirely
   - Secret: Pre-defined or referenced

### Implementation Notes
- EK→AK binding is proven by successful credential activation (no separate AK certification step required).
- PCR quote structure contains both quote/signature and the actual selected PCR values.
- PCR quote signature is cryptographically verified using the AK public key to ensure PCR authenticity.
- PCR values are verified against the TPM quote digest to ensure they are cryptographically bound to the quote.
- This package owns message marshalling/unmarshalling so consumers don't need to manage encoding details.

### Attestator (Injected Policy)
The Attestator is provided by the consumer and is called after cryptographic verification succeeds. It decides selective enrollment/validation and returns the passphrase.

Interface (conceptual):

```
type Attestator interface {
    IssuePassphrase(ctx context.Context, req AttestationRequest) ([]byte, error)
}

type AttestationRequest struct {
    TPMHash            string            // derived from EK (server-enrolled identity)
    PCRs               map[int][]byte    // client-selected PCRs from verified quote
    EKPEM              []byte            // optional: EK in PEM/DER for auditing/forensics
    DeferPCREnrollment bool              // LiveCD mode: defer PCR enrollment (store as empty strings)
    // Optional: partition/volume metadata for policy, if the consumer passes it through
}
```

Notes:
- AK public is not passed to the Attestator. The library verifies the EK→AK→PCR chain and only then calls the Attestator with data relevant to policy (PCRs, TPM identity).
