package auth_test

import (
	"strings"
	"testing"
	"time"

	"mcphe/auth"
)

func TestCreateAndValidateToken(t *testing.T) {
	secret := "test-secret"
	username := "testuser"
	ttl := 24 * time.Hour

	token, err := auth.Create("mcphe", "1.0.0", username, ttl, secret)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	id, err := auth.Validate("mcphe", "1.0.0", token, secret)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if id.Username != username {
		t.Errorf("Expected username %s, got %s", username, id.Username)
	}
	if id.Progname != "mcphe" {
		t.Errorf("Expected progname %s, got %s", "mcphe", id.Progname)
	}
	if id.Version != "1.0.0" {
		t.Errorf("Expected version %s, got %s", "1.0.0", id.Version)
	}

	if time.Now().Unix() > id.ExpiresAt {
		t.Errorf("Token expired immediately")
	}
}

func TestValidateTokenExpired(t *testing.T) {
	secret := "test-secret"
	username := "testuser"
	ttl := 1 * time.Millisecond

	token, err := auth.Create("mcphe", "1.0.0", username, ttl, secret)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	time.Sleep(1 * time.Second)
	id, err := auth.Validate("mcphe", "1.0.0", token, secret)
	if err == nil {
		t.Errorf("Expected token to be expired, but got id: %+v", id)
	}
}

func TestValidateTokenBadSignature(t *testing.T) {
	secret := "test-secret"
	username := "testuser"
	ttl := 24 * time.Hour

	token, err := auth.Create("mcphe", "1.0.0", username, ttl, secret)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Tamper with the token to invalidate the signature
	parts := strings.Split(token, ".")
	if len(parts) == 2 {
		tamperedToken := parts[0] + ".bad_signature"
		id, err := auth.Validate("mcphe", "1.0.0", tamperedToken, secret)
		if err == nil {
			t.Errorf("Expected token validation to fail for tampered token, but got id: %+v", id)
		}
	}
}

