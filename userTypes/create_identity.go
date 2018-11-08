package UserTypes

import (
	"crypto/sha256"
	"encoding/hex"
)

func CreateIdentity(userId string, publicKey string, userIdentifier string) string {
	toHash := []byte(userId + publicKey + userIdentifier)
	h := sha256.New()
	h.Write(toHash)
	return hex.EncodeToString(h.Sum(nil))
}
