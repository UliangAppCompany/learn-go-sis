package auth

import (
	"testing"
)

func TestHashAndCheck(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if hash == "correct horse" {
		t.Fatalf("password stored in plaintext")
	}

	if err := CheckPassword(hash, "correct horse"); err != nil {
		t.Errorf("correct password rejected: %v", err)
	}
	if err := CheckPassword(hash, "wrong"); err == nil {
		t.Error("wrong password accepted")
	}
}
