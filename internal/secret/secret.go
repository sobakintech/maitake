package secret

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

const (
	KeyPrefix    = "mtk_"
	keyPrefixLen = len(KeyPrefix) + 8
)

// Token returns a random URL-safe string with 256 bits of entropy.
func Token() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func Hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// NewAPIKey returns a full key, the short prefix safe to display, and its hash for storage.
func NewAPIKey() (key, prefix string, hash []byte) {
	key = KeyPrefix + Token()
	return key, key[:keyPrefixLen], Hash(key)
}

func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash keeps login timing similar whether or not the username exists.
var dummyHash, _ = HashPassword("maitake-dummy-password")

func BurnPasswordCheck(pw string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(pw))
}

func ValidatePassword(pw string) error {
	if len(pw) < 10 {
		return errors.New("password must be at least 10 characters")
	}
	if len(pw) > 72 {
		return errors.New("password must be at most 72 bytes")
	}
	return nil
}
