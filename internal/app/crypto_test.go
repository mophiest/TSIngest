package app

import (
	"bytes"
	"testing"
)

func TestSecretEncryptionRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	encrypted, err := EncryptSecret(key, "strong-srt-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "strong-srt-passphrase" {
		t.Fatal("secret was stored in plaintext")
	}
	plain, err := DecryptSecret(key, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "strong-srt-passphrase" {
		t.Fatalf("unexpected plaintext %q", plain)
	}
}

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("invalid password accepted")
	}
}

func TestValidateSettings(t *testing.T) {
	valid := SystemSettings{MP4Concurrency: 2, SoftFreePercent: 10, SoftFreeGiB: 100, HardFreePercent: 5, HardFreeGiB: 20}
	if err := ValidateSettings(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	valid.HardFreePercent = 12
	if err := ValidateSettings(valid); err == nil {
		t.Fatal("invalid watermarks accepted")
	}
}
