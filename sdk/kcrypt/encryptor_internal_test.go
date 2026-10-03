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
	// Autoload sorts by the glob, so "a-broken" is asked before "b-good".
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
