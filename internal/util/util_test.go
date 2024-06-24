package util

import "testing"

func TestGetKSUID(t *testing.T) {
	id := GetKSUID()

	if id == "" {
		t.Error("id is empty")
	}
}
