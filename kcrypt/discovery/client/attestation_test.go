package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/kairos-io/kairos/v4/kcrypt/pkg/attestation"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	tpm "github.com/kairos-io/tpm-helpers"
)

func TestPerformTPMAttestationServerErrorInsteadOfChallenge(t *testing.T) {
	const serverErr = "Challenge generation: generating challenge: invalid EK"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrading connection: %v", err)
			return
		}
		defer conn.Close() //nolint:errcheck

		var init attestation.Init
		if err := conn.ReadJSON(&init); err != nil {
			t.Errorf("reading init: %v", err)
			return
		}
		_ = conn.WriteJSON(attestation.Response{Error: serverErr})
	}))
	defer srv.Close()

	attestationClient, err := attestation.NewRemoteAttestationClient(tpm.Emulated, tpm.EmulatedHostSeed())
	if err != nil {
		t.Fatalf("creating emulated attestation client: %v", err)
	}
	defer attestationClient.Close() //nolint:errcheck

	c := &Client{Config: newEmptyConfig(), Logger: loggerpkg.NewNullLogger()}
	p := &partitions.Partition{FilesystemLabel: "COS_PERSISTENT", Name: "/dev/sda5", UUID: "uuid"}

	_, err = c.performTPMAttestation(attestationEndpointURL(srv.URL), nil, attestationClient, p)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "attestation failed") {
		t.Errorf("error %q does not mark the failure as permanent (missing \"attestation failed\")", err)
	}
	if !strings.Contains(err.Error(), serverErr) {
		t.Errorf("error %q does not carry the server message %q", err, serverErr)
	}
}
