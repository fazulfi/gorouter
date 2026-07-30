package auth

import (
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// PasswordService handles bcrypt hashing and verification of passwords.
type PasswordService struct{}

// NewPasswordService creates a PasswordService with bcrypt cost 12.
func NewPasswordService() *PasswordService {
	return &PasswordService{}
}

// HashPassword hashes a plaintext password using bcrypt with cost 12.
func (s *PasswordService) HashPassword(plaintext string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// VerifyPassword compares a plaintext password against a bcrypt hash.
// Returns true if they match, false otherwise (constant-time comparison via bcrypt).
func (s *PasswordService) VerifyPassword(hash, plaintext string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
	return err == nil
}
