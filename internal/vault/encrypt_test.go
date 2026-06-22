package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVaultEncryptDecrypt(t *testing.T) {
	dir := t.TempDir()
	autoKeyPath := filepath.Join(dir, ".autokey")

	v, err := NewVault(autoKeyPath)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	plaintext := "my-secret-password-123"

	encrypted, err := v.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if encrypted == plaintext {
		t.Error("Encrypted text should differ from plaintext")
	}

	decrypted, err := v.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("Decrypt mismatch: got %q, want %q", decrypted, plaintext)
	}

	if _, err := os.Stat(autoKeyPath); err != nil {
		t.Errorf(".autokey file not created: %v", err)
	}
}

func TestVaultDecryptInvalidData(t *testing.T) {
	dir := t.TempDir()
	autoKeyPath := filepath.Join(dir, ".autokey")

	v, err := NewVault(autoKeyPath)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	_, err = v.Decrypt("not-valid-json")
	if err == nil {
		t.Error("Expected error for invalid encrypted data")
	}
}

func TestVaultAutoKeyPersistence(t *testing.T) {
	dir := t.TempDir()
	autoKeyPath := filepath.Join(dir, ".autokey")

	v1, err := NewVault(autoKeyPath)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	plaintext := "test-password"
	encrypted, err := v1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	v2, err := NewVault(autoKeyPath)
	if err != nil {
		t.Fatalf("Second NewVault failed: %v", err)
	}

	decrypted, err := v2.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt with reloaded key failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("Decrypt mismatch after reload: got %q, want %q", decrypted, plaintext)
	}
}
