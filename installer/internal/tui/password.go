package tui

import (
	"crypto/rand"
	"fmt"

	"github.com/tredoe/osutil/user/crypt/sha512_crypt"
)

const cryptSaltCharacters = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var passwordHasher = hashPassword

func hashPassword(password string) (string, error) {
	random := make([]byte, sha512_crypt.SaltLenMax)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	for i := range random {
		random[i] = cryptSaltCharacters[int(random[i])%len(cryptSaltCharacters)]
	}
	salt := append([]byte(sha512_crypt.MagicPrefix), random...)
	return sha512_crypt.New().Generate([]byte(password), salt)
}
