package cmd

import (
	"fmt"
	"os"
	"strings"
)

// ResolveRegistrationToken returns the registration token to run with,
// treating envValue as a seed rather than a hard override.
//
// The seed marker at seedPath records the envValue applied on a previous
// startup. When it matches the current envValue, the token file at
// tokenPath is authoritative so a runtime rotation (which writes only
// tokenPath) survives restarts. When it differs the operator is
// supplying a new seed (fresh install, helm upgrade with a new value),
// so both files are overwritten.
//
// When envValue is empty the token file wins if present, otherwise a
// fresh token is generated and persisted to tokenPath. The seed marker
// is left alone; a later restart with envValue set can still rotate.
func ResolveRegistrationToken(envValue, tokenPath, seedPath string) (string, error) {
	envValue = strings.TrimSpace(envValue)

	if envValue != "" {
		lastSeed, seedExists, err := readTrimmed(seedPath)
		if err != nil {
			return "", fmt.Errorf("read registration token seed marker: %w", err)
		}
		if !seedExists || lastSeed != envValue {
			if err := writeSecretFile(tokenPath, envValue); err != nil {
				return "", fmt.Errorf("persist registration token: %w", err)
			}
			if err := writeSecretFile(seedPath, envValue); err != nil {
				return "", fmt.Errorf("persist registration token seed marker: %w", err)
			}
			return envValue, nil
		}
	}

	token, exists, err := readTrimmed(tokenPath)
	if err != nil {
		return "", fmt.Errorf("read registration token: %w", err)
	}
	if exists && token != "" {
		return token, nil
	}

	if envValue != "" {
		if err := writeSecretFile(tokenPath, envValue); err != nil {
			return "", fmt.Errorf("persist registration token: %w", err)
		}
		return envValue, nil
	}

	generated := generateToken(16)
	if err := writeSecretFile(tokenPath, generated); err != nil {
		return "", fmt.Errorf("persist registration token: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Generated registration token: %s (saved to %s)\n", generated, tokenPath)
	return generated, nil
}

func readTrimmed(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimRight(string(data), "\r\n"), true, nil
}

func writeSecretFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0600)
}
