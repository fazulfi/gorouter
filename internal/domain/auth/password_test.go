package auth

import (
	"testing"
)

func TestPasswordService_HashPassword(t *testing.T) {
	svc := NewPasswordService()

	hash, err := svc.HashPassword("my-secure-password")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword returned empty hash")
	}
}

func TestPasswordService_VerifyPassword_Correct(t *testing.T) {
	svc := NewPasswordService()

	hash, err := svc.HashPassword("my-secure-password")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !svc.VerifyPassword(hash, "my-secure-password") {
		t.Fatal("VerifyPassword returned false for correct password")
	}
}

func TestPasswordService_VerifyPassword_Wrong(t *testing.T) {
	svc := NewPasswordService()

	hash, err := svc.HashPassword("my-secure-password")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if svc.VerifyPassword(hash, "wrong-password") {
		t.Fatal("VerifyPassword returned true for wrong password")
	}
}

func TestPasswordService_VerifyPassword_EmptyHash(t *testing.T) {
	svc := NewPasswordService()

	if svc.VerifyPassword("", "password") {
		t.Fatal("VerifyPassword returned true for empty hash")
	}
}

func TestPasswordService_VerifyPassword_InvalidHash(t *testing.T) {
	svc := NewPasswordService()

	if svc.VerifyPassword("not-a-valid-hash", "password") {
		t.Fatal("VerifyPassword returned true for invalid hash")
	}
}

func TestPasswordService_HashPassword_EmptyString(t *testing.T) {
	svc := NewPasswordService()

	hash, err := svc.HashPassword("")
	if err != nil {
		t.Fatalf("HashPassword failed for empty string: %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword returned empty hash for empty password")
	}
}

// knownBootstrapHash is the bcrypt hash of "12345678" used in the foundation migration.
// This must remain stable for replayable migrations.
const knownBootstrapHash = "$2a$10$REBPWXtQ9mmup2iap9ibrOiFORTDTwVt/Nd49wrJVXbjhnIc70a2."

func TestPasswordService_BootstrapHashIsValid(t *testing.T) {
	svc := NewPasswordService()
	if !svc.VerifyPassword(knownBootstrapHash, "12345678") {
		t.Fatal("bootstrap hash does not match password '12345678'")
	}
}

func TestPasswordService_BootstrapHashRejectsWrongPassword(t *testing.T) {
	svc := NewPasswordService()
	if svc.VerifyPassword(knownBootstrapHash, "wrong-password") {
		t.Fatal("bootstrap hash should NOT match any other password")
	}
}

func TestPasswordService_HashPassword_ProducesBcryptHash(t *testing.T) {
	svc := NewPasswordService()

	hash, err := svc.HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// bcrypt hashes start with $2a$ or $2b$
	if len(hash) < 4 || hash[:4] != "$2a$" {
		t.Fatalf("Hash does not look like bcrypt: %s", hash)
	}
}
