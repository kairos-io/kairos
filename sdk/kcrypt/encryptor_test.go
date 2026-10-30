package kcrypt

import (
	"errors"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// TestTPMWithPCREncryptorRejectsUnreadableBindings pins where an unreadable PCR
// binding is allowed to fail. Only Encrypt consumes the bindings, while
// UnlockAllEncryptedPartitions builds an encryptor on every boot through
// GetEncryptor, so a bind-pcrs that cannot be read (edited under /oem after
// install, or written in a shape the collector does not produce) must refuse an
// enrollment and must not keep an already-encrypted partition locked.
func TestTPMWithPCREncryptorRejectsUnreadableBindings(t *testing.T) {
	bindingErr := errors.New("reading the PCR bindings: bind-pcrs")

	e := &TPMWithPCREncryptor{
		logger:     logger.NewNullLogger(),
		bindingErr: bindingErr,
	}

	// No partitions, so nothing but the guard can refuse: the binding error is
	// raised before any enrollment is attempted, not as a side effect of one.
	if err := e.Encrypt(nil); !errors.Is(err, bindingErr) {
		t.Fatalf("Encrypt(nil) error = %v, want %v", err, bindingErr)
	}

	if err := e.Encrypt([]string{"COS_PERSISTENT"}); !errors.Is(err, bindingErr) {
		t.Fatalf("Encrypt error = %v, want %v", err, bindingErr)
	}
}

// TestTPMWithPCREncryptorWithoutBindingError is the other half: with the
// bindings read successfully there is no guard left to trip.
func TestTPMWithPCREncryptorWithoutBindingError(t *testing.T) {
	e := &TPMWithPCREncryptor{logger: logger.NewNullLogger()}

	if err := e.Encrypt(nil); err != nil {
		t.Fatalf("Encrypt(nil) error = %v, want nil", err)
	}
}
