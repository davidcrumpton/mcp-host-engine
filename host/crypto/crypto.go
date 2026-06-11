package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

func RandomBytes(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return string(b), nil
}

func Sha256(data string) (string, error) {
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil)), nil
}