package attestation_test

import (
	"context"
	"encoding/json"

	"github.com/google/go-attestation/attest"
	. "github.com/kairos-io/kairos-challenger/pkg/attestation"
	tpmhelpers "github.com/kairos-io/tpm-helpers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type dummyAttestator struct{}

func (d *dummyAttestator) IssuePassphrase(ctx context.Context, req AttestationRequest) ([]byte, error) {
	return []byte("passphrase"), nil
}

var _ = Describe("Remote attestation end-to-end", func() {
	It("client and server roundtrip", func() {
		// Client: create init
		client, err := NewRemoteAttestationClient(tpmhelpers.Emulated, tpmhelpers.EmulatedHostSeed())
		Expect(err).ToNot(HaveOccurred())
		defer client.Close() //nolint:errcheck

		init, err := client.CreateInit()
		Expect(err).ToNot(HaveOccurred())
		Expect(init).ToNot(BeNil())
		Expect(init.EKPublic).ToNot(BeEmpty())
		Expect(init.AKParams).ToNot(BeEmpty())

		// Server: parse init and generate challenge
		server := NewRemoteAttestationServer(&dummyAttestator{})
		challenge, expectedSecret, err := server.GenerateChallenge(init)
		Expect(err).ToNot(HaveOccurred())
		Expect(challenge).ToNot(BeNil())
		Expect(challenge.EncryptedCredential).ToNot(BeEmpty())
		Expect(expectedSecret).ToNot(BeEmpty())

		// Client: handle challenge with chosen PCRs
		proof, err := client.HandleChallenge(challenge, []int{0, 7, 11})
		Expect(err).ToNot(HaveOccurred())
		Expect(proof).ToNot(BeNil())
		Expect(proof.Secret).ToNot(BeEmpty())
		Expect(proof.PCRQuote).ToNot(BeEmpty())

		// Server: verify proof and issue passphrase
		vr, err := server.VerifyProof(init, proof, expectedSecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(vr.PCRs).ToNot(BeNil())

		// Issue passphrase via attestator
		pass, err := server.IssuePassphrase(context.Background(), init, proof, expectedSecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(pass).To(Equal([]byte("passphrase")))
	})

	It("methods validate input formats", func() {
		// malformed init (invalid EK public key)
		server := NewRemoteAttestationServer(&dummyAttestator{})
		malformedInit := &AttestationInit{
			EKPublic: []byte("not-valid-spki"),
			AKParams: []byte("{}"),
		}
		_, _, err := server.GenerateChallenge(malformedInit)
		Expect(err).To(MatchError(ContainSubstring("parse EK SPKI")))

		// client handle malformed challenge
		client, err := NewRemoteAttestationClient(tpmhelpers.Emulated, tpmhelpers.EmulatedHostSeed())
		Expect(err).ToNot(HaveOccurred())
		defer client.Close() //nolint:errcheck

		malformedChallenge := &AttestationChallenge{
			EncryptedCredential: []byte("not-json"),
		}
		_, err = client.HandleChallenge(malformedChallenge, []int{0})
		Expect(err).To(MatchError(ContainSubstring("invalid character")))
	})

	It("server parse init decodes AK params", func() {
		client, err := NewRemoteAttestationClient(tpmhelpers.Emulated, tpmhelpers.EmulatedHostSeed())
		Expect(err).ToNot(HaveOccurred())
		defer client.Close() //nolint:errcheck

		init, err := client.CreateInit()
		Expect(err).ToNot(HaveOccurred())

		ek, akParams, err := NewRemoteAttestationServer(&dummyAttestator{}).ParseInit(init)
		Expect(err).ToNot(HaveOccurred())
		Expect(ek).ToNot(BeNil())
		Expect(akParams).ToNot(BeNil())

		// ensure akParams is valid
		var ap attest.AttestationParameters
		akParamsBytes, err := json.Marshal(akParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(json.Unmarshal(akParamsBytes, &ap)).To(Succeed())
	})
})
