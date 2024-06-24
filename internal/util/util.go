package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/segmentio/ksuid"
)

func NewSHA256(data string) string {
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:])
}

func GetID() string {
	id := ksuid.New()
	return hex.EncodeToString(id.Bytes())
}
