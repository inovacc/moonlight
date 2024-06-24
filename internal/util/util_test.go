package util

import "testing"

func TestGetKSUID(t *testing.T) {
	id := GetID()

	if id == "" {
		t.Error("id is empty")
	}
}
