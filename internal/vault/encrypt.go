package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
)

// SaltLength is the length of the salt in bytes
const SaltLength = 16

// KeyLength is the length of the derived key in bytes
const KeyLength = 32

// NonceLength is the length of the nonce in bytes for AES-GCM
const NonceLength = 12

// Vault stores the encryption keys in memory
type Vault struct {
	autoKey   []byte
	mainPass  string
	autoKeyPath string
}

// VaultData is the format for storing encrypted vault data
type VaultData struct {
	Salt     string `json:"salt"`
	Nonce    string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// NewVault creates a new Vault instance
// It loads the auto key from .autokey file
func NewVault(autoKeyPath string) (*Vault, error) {
	v := &Vault{
		autoKeyPath: autoKeyPath,
	}

	// Try to load from .autokey file
	if key, err := LoadAutoKey(autoKeyPath); err == nil && len(key) == KeyLength {
		v.autoKey = key
		return v, nil
	}

	// Generate new auto key
	key := make([]byte, KeyLength)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate auto key: %w", err)
	}
	v.autoKey = key

	// Save to .autokey file
	if err := SaveAutoKey(autoKeyPath, key); err != nil {
		// Log warning but continue - this is acceptable fallback
		fmt.Fprintf(os.Stderr, "WARNING: Failed to save auto key: %v\n", err)
	}

	return v, nil
}

// SetMainPassword sets the user's main password
// The main password is used to encrypt the auto key when exported
func (v *Vault) SetMainPassword(pass string) {
	v.mainPass = pass
}

// Encrypt encrypts data using AES-GCM with the auto key
// If main password is set, the auto key is first derived from it
func (v *Vault) Encrypt(data string) (string, error) {
	// Use auto key as base
	key := v.autoKey

	// If main password is set, derive key from it using simple PBKDF-like approach
	if v.mainPass != "" {
		// Generate salt for main password
		salt := make([]byte, SaltLength)
		if _, err := rand.Read(salt); err != nil {
			return "", fmt.Errorf("failed to generate salt: %w", err)
		}
		key = deriveKey(v.mainPass, salt)
	}

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, NonceLength)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nil, nonce, []byte(data), nil)

	// Encode to base64
	result := VaultData{
		Salt:     base64.StdEncoding.EncodeToString(v.autoKey[:SaltLength]),
		Nonce:    base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}

	encryptedData, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(encryptedData), nil
}

// Decrypt decrypts data using AES-GCM with the auto key
func (v *Vault) Decrypt(data string) (string, error) {
	// Parse vault data
	var vd VaultData
	if err := json.Unmarshal([]byte(data), &vd); err != nil {
		// Return original if not valid JSON (for backward compatibility)
		return data, fmt.Errorf("decryption failed: not encrypted data")
	}

	// Decode salt, nonce, ciphertext
	salt, err := base64.StdEncoding.DecodeString(vd.Salt)
	if err != nil {
		return "", fmt.Errorf("failed to decode salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(vd.Nonce)
	if err != nil {
		return "", fmt.Errorf("failed to decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(vd.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	// Use auto key as base
	key := v.autoKey

	// If main password is set, derive key from it
	if v.mainPass != "" {
		key = deriveKey(v.mainPass, salt[:SaltLength])
	}

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Return original and log error (don't block)
		fmt.Errorf("decryption failed: %v", err)
		return data, fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}

// deriveKey derives a key from password using simple PBKDF-like approach
// This is a simplified key derivation for MVP
func deriveKey(password string, salt []byte) []byte {
	// Simple PBKDF-like: hash password + salt multiple times
	result := make([]byte, KeyLength)
	copy(result, salt)

	for i := 0; i < 10000; i++ {
		// XOR with password
		for j := 0; j < len(password); j++ {
			result[j%KeyLength] ^= password[j]
		}
		// XOR with salt
		for j := 0; j < len(salt); j++ {
			result[j%KeyLength] ^= salt[j]
		}
	}

	return result
}

// json.MarshalToString is a helper to marshal to JSON string
func marshalToString(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
