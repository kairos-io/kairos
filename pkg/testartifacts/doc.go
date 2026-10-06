// Package testartifacts generates the binary artifacts tests need (keys,
// signed EFI executables, EFI variable contents) at test time, so that no
// generated executable or unreviewable binary has to live in the
// repository.
//
// It is meant for tests only. Its API carries no compatibility promise.
package testartifacts
