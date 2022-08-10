package utils

import "strings"

const sep = "_CREDENTIALS_"

func EncodeRecoveryToken(data ...string) string {
	return strings.Join(data, sep)
}

func DecodeRecoveryToken(recoverytoken string) []string {
	return strings.Split(recoverytoken, sep)
}
