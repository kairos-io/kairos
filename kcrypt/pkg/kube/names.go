package kube

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SafeKubeName ensures a string conforms to Kubernetes naming constraints
// - Maximum 63 characters
// - Must be a valid DNS subdomain (alphanumeric and hyphens only)
// - Must start and end with alphanumeric characters
// - Includes checksum to avoid collisions when truncating
func SafeKubeName(name string) string {
	originalName := name

	// If the name is already short enough and valid, return it as-is
	if len(name) <= 63 && IsValidKubeName(name) {
		return name
	}

	// Calculate checksum early based on original name to avoid collisions
	hash := sha256.Sum256([]byte(originalName))
	checksum := hex.EncodeToString(hash[:])[:8] // Use first 8 chars of hash

	// Clean the name first
	cleaned := SanitizeKubeName(name)

	// If cleaned name is empty, use the checksum as the name
	if cleaned == "" {
		return "kube-" + checksum
	}

	// If still too long, we need to truncate and add a checksum
	if len(cleaned) > 63 {
		// Truncate to leave room for checksum (63 - 9 for "-" + 8 char checksum = 54)
		maxBaseLength := 54
		if len(cleaned) > maxBaseLength {
			cleaned = cleaned[:maxBaseLength]
			cleaned = strings.TrimSuffix(cleaned, "-")
		}

		// Append checksum
		cleaned = cleaned + "-" + checksum
	}

	return cleaned
}

// SanitizeKubeName cleans a string to be a valid Kubernetes name
func SanitizeKubeName(name string) string {
	// Replace invalid characters with hyphens
	// Keep only alphanumeric characters and hyphens, convert uppercase to lowercase
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		} else if r >= 'A' && r <= 'Z' {
			// Convert uppercase to lowercase
			result.WriteRune(r + 32)
		} else {
			result.WriteRune('-')
		}
	}

	cleaned := result.String()

	// Remove leading/trailing hyphens and ensure it starts/ends with alphanumeric
	cleaned = strings.Trim(cleaned, "-")

	return cleaned
}

// IsValidKubeName checks if a name is already a valid Kubernetes name
func IsValidKubeName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}

	// Must start and end with alphanumeric
	if !IsAlphanumeric(rune(name[0])) || !IsAlphanumeric(rune(name[len(name)-1])) {
		return false
	}

	// All characters must be alphanumeric or hyphens
	for _, r := range name {
		if !IsAlphanumeric(r) && r != '-' {
			return false
		}
	}

	return true
}

// IsAlphanumeric checks if a rune is alphanumeric
func IsAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
