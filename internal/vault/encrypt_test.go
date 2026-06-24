package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
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

	// Note: .autokey file may not exist if OS keyring is the primary store
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

// TestVaultEncryptDecryptWithPassword tests PBKDF2-based encryption with main password
func TestVaultEncryptDecryptWithPassword(t *testing.T) {
	dir := t.TempDir()
	autoKeyPath := filepath.Join(dir, ".autokey")

	v, err := NewVault(autoKeyPath)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}
	v.SetMainPassword("my-master-pass")

	plaintext := "secret-ssh-password-456"
	encrypted, err := v.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Verify v2 version tag is present
	var vd VaultData
	if err := json.Unmarshal([]byte(encrypted), &vd); err != nil {
		t.Fatalf("Failed to parse encrypted data: %v", err)
	}
	if vd.Version != vaultVersionV2 {
		t.Errorf("Expected version %q, got %q", vaultVersionV2, vd.Version)
	}

	// Decrypt with same password
	v2, _ := NewVault(autoKeyPath)
	v2.SetMainPassword("my-master-pass")
	decrypted, err := v2.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("Decrypt mismatch: got %q, want %q", decrypted, plaintext)
	}
}

// TestVaultLegacyKDFBackwardCompat verifies old-format data (no version) still decrypts
func TestVaultLegacyKDFBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	autoKeyPath := filepath.Join(dir, ".autokey")

	v, err := NewVault(autoKeyPath)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}
	v.SetMainPassword("legacy-pass")

	// Simulate legacy encryption: use deriveKeyLegacy, no version field
	plaintext := "old-format-secret"
	salt := make([]byte, SaltLength)
	for i := range salt {
		salt[i] = byte(i)
	}
	legacyKey := deriveKeyLegacy("legacy-pass", salt)

	// Manually build legacy VaultData (no Version field) using real AES-GCM
	block, err := aes.NewCipher(legacyKey)
	if err != nil {
		t.Fatalf("aes.NewCipher failed: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM failed: %v", err)
	}
	nonce := make([]byte, NonceLength)
	for i := range nonce {
		nonce[i] = byte(i + 32)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	legacyData := VaultData{
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	legacyJSON, _ := json.Marshal(legacyData)

	// Decrypt legacy format with same password
	decrypted, err := v.Decrypt(string(legacyJSON))
	if err != nil {
		t.Fatalf("Failed to decrypt legacy format: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("Legacy decrypt mismatch: got %q, want %q", decrypted, plaintext)
	}
}
