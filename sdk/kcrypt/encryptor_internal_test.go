package kcrypt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/kcrypt/bus"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// writeDiscoveryProvider drops an executable discovery provider in dir that
// answers the password event with the given JSON response body.
func writeDiscoveryProvider(t *testing.T, dir, name, response string) {
	t.Helper()

	script := "#!/bin/bash\ncat > /dev/null\necho '" + response + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0777); err != nil {
		t.Fatalf("writing provider %s: %s", name, err)
	}
}

// discoveryProviderDir makes the providers written into the returned directory
// the ones the bus finds: it autoloads from the working directory as well as
// from the fixed system paths.
func discoveryProviderDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	return dir
}

func newRemoteKMSEncryptorForTest() *RemoteKMSEncryptor {
	return &RemoteKMSEncryptor{
		logger:       logger.NewKairosLogger("kcrypt-test", "fatal", true),
		kcryptConfig: &bus.KcryptConfig{ChallengerServer: "https://kms.example"},
	}
}

func TestGetPasswordFromChallengerKeepsTheAnswerOfAWorkingProvider(t *testing.T) {
	dir := discoveryProviderDir(t)
	// Both providers are asked at once; only "b-good" has an answer.
	writeDiscoveryProvider(t, dir, "kcrypt-discovery-a-broken", `{"error":"the KMS is unreachable"}`)
	writeDiscoveryProvider(t, dir, "kcrypt-discovery-b-good", `{"data":"s3cret"}`)

	password, err := newRemoteKMSEncryptorForTest().
		getPasswordFromChallenger(&partitions.Partition{Name: "vda2", FilesystemLabel: "COS_PERSISTENT"})
	if err != nil {
		t.Fatalf("expected the working provider to answer, got error: %s", err)
	}
	if password != "s3cret" {
		t.Fatalf("expected the password from the working provider, got %q", password)
	}
}

func TestGetPasswordFromChallengerReportsWhyNoProviderAnswered(t *testing.T) {
	dir := discoveryProviderDir(t)
	writeDiscoveryProvider(t, dir, "kcrypt-discovery-broken", `{"error":"the KMS is unreachable"}`)

	password, err := newRemoteKMSEncryptorForTest().
		getPasswordFromChallenger(&partitions.Partition{Name: "vda2", FilesystemLabel: "COS_PERSISTENT"})
	if err == nil {
		t.Fatal("expected an error when every provider failed")
	}
	if password != "" {
		t.Fatalf("expected no password, got %q", password)
	}
	if !strings.Contains(err.Error(), "the KMS is unreachable") {
		t.Fatalf("expected the provider's own error, got %q", err)
	}
}

// Two providers that both have an answer race on the single password the
// handler keeps, which is why that variable is guarded.
func TestGetPasswordFromChallengerWithTwoAnsweringProviders(t *testing.T) {
	dir := discoveryProviderDir(t)
	writeDiscoveryProvider(t, dir, "kcrypt-discovery-a-good", `{"data":"first"}`)
	writeDiscoveryProvider(t, dir, "kcrypt-discovery-b-good", `{"data":"second"}`)

	password, err := newRemoteKMSEncryptorForTest().
		getPasswordFromChallenger(&partitions.Partition{Name: "vda2", FilesystemLabel: "COS_PERSISTENT"})
	if err != nil {
		t.Fatalf("expected a password, got error: %s", err)
	}
	// Either provider may answer first, but exactly one answer is kept.
	if password != "first" && password != "second" {
		t.Fatalf("expected one provider's password, got %q", password)
	}
}

// Every provider that failed has to reach the caller, not just the first one
// to answer.
func TestGetPasswordFromChallengerReportsEveryFailure(t *testing.T) {
	dir := discoveryProviderDir(t)
	writeDiscoveryProvider(t, dir, "kcrypt-discovery-a-broken", `{"error":"the KMS is unreachable"}`)
	writeDiscoveryProvider(t, dir, "kcrypt-discovery-b-broken", `{"error":"the token expired"}`)

	_, err := newRemoteKMSEncryptorForTest().
		getPasswordFromChallenger(&partitions.Partition{Name: "vda2", FilesystemLabel: "COS_PERSISTENT"})
	if err == nil {
		t.Fatal("expected an error when every provider failed")
	}
	for _, want := range []string{"the KMS is unreachable", "the token expired"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in the error, got %q", want, err)
		}
	}
}
