## Attestation Package

This package implements a clean, close-circle API for the kcrypt-challenger remote attestation flow. It separates client and server responsibilities and defines the wire messages used between them, so consumers can simply pass the messages from one step to the next and only check for errors in between.

Important: The client decides which PCRs to include/enroll/validate. The server validates according to its selective enrollment policy. The PCR quote is bound to a nonce: the secret the server generates for the credential challenge. The client recovers it by activating the credential and quotes with it as the nonce, and the server verifies the quote against the secret it issued, so a quote recorded on an earlier boot does not verify.

### High-Level Flow

Client (node with TPM):
1) CreateInit → send to server
2) Receive Challenge
3) HandleChallenge → send Proof to server

Server (kcrypt-challenger):
1) ParseInit
2) GenerateChallenge (EK+AK only; PCRs not needed here)
3) VerifyProof (secret + PCR quote signature verification + PCR consistency verification) → proceed with selective enrollment and passphrase release

### Wire Messages (owned by this package)
- Init: EKPublic (SPKI DER), AKParams (JSON-encoded AK attestation parameters), DeferPCREnrollment flag (for LiveCD mode)
- Challenge: EncryptedCredential (no PCRs)
- Proof: Secret (credential activation, also the quote nonce), PCRQuote (JSON with quote + selected PCR values)

### Client API
- NewRemoteAttestationClient(opts ...tpm.Option) (*RemoteAttestationClient, error)
- Close() error
- CreateInit() (*Init, error)
- CreateInitDeferredEnrollment() (*Init, error)  // For LiveCD mode
- HandleChallenge(challenge *Challenge, pcrs []int) (*Proof, error)

Internally, the client uses transient AKs (no persistent AK storage). It sends EK + AK attestation data in Init, and later returns the secret and PCR quote in Proof.

**LiveCD Mode Support:**
- `CreateInit()`: Normal enrollment - PCRs will be enrolled with current values
- `CreateInitDeferredEnrollment()`: LiveCD mode - sets `DeferPCREnrollment: true`, PCRs stored as empty strings for later enrollment after installation

### Server API
- NewRemoteAttestationServer(attestator Attestator) *RemoteAttestationServer
- ParseInit(init *Init) (*attest.EK, *attest.AttestationParameters, error)
- GenerateChallenge(init *Init) (*Challenge, []byte, error)  // the []byte is the secret to keep for VerifyProof
- VerifyProof(init *Init, proof *Proof, expectedSecret []byte) (VerificationResult, error)
- IssuePassphrase(ctx context.Context, init *Init, proof *Proof, expectedSecret []byte) ([]byte, error)

The server accepts Init, generates an EncryptedCredential (challenge) bound to EK+AK params (no PCRs), and later verifies the secret, and the PCR quote signature and nonce, using the AK public contained within the attestation parameters. The server also verifies that the provided PCR values are consistent with the TPM quote to ensure they are cryptographically bound to the quote. After verification, it delegates policy/enrollment to the injected Attestator and returns a passphrase or error.

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
    IssuePassphrase(ctx context.Context, req Request) ([]byte, error)
}

type Request struct {
    TPMHash            string            // derived from EK (server-enrolled identity)
    PCRs               map[int][]byte    // client-selected PCRs from verified quote
    EKPEM              []byte            // optional: EK in PEM/DER for auditing/forensics
    DeferPCREnrollment bool              // LiveCD mode: defer PCR enrollment (store as empty strings)
    // Optional: partition/volume metadata for policy, if the consumer passes it through
}
```

Notes:
- AK public is not passed to the Attestator. The library verifies the EK→AK→PCR chain and only then calls the Attestator with data relevant to policy (PCRs, TPM identity).
