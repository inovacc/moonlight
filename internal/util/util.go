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

func RemoveDuplicates(elements []string) []string {
	// Create a map to track seen elements
	seen := make(map[string]struct{})
	var result []string

	for _, element := range elements {
		if _, found := seen[element]; !found {
			seen[element] = struct{}{}
			result = append(result, element)
		}
	}
	return result
}
