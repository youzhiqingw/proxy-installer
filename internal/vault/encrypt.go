package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/crypto/pbkdf2"
)

// SaltLength is the length of the salt in bytes
const SaltLength = 16

// KeyLength is the length of the derived key in bytes
const KeyLength = 32

// NonceLength is the length of the nonce in bytes for AES-GCM
const NonceLength = 12

// PBKDF2Iterations is the number of iterations for PBKDF2 key derivation (v2)
const PBKDF2Iterations = 600_000

// vaultVersionV2 is the version tag for PBKDF2-encrypted data
const vaultVersionV2 = "v2"

// Vault stores the encryption keys in memory
type Vault struct {
	autoKey     []byte
	mainPass    string
	autoKeyPath string
}

// VaultData is the format for storing encrypted vault data
type VaultData struct {
	Version    string `json:"version,omitempty"` // "v2" for PBKDF2; empty = legacy XOR KDF
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// NewVault creates a new Vault instance
// Key loading priority: OS keyring → .autokey file → generate new
// On first use, migrates .autokey to OS keyring automatically
func NewVault(autoKeyPath string) (*Vault, error) {
	v := &Vault{
		autoKeyPath: autoKeyPath,
	}

	// 1. Try OS keyring first
	if key, err := getKeyFromKeyring(); err == nil && len(key) == KeyLength {
		v.autoKey = key
		return v, nil
	}

	// 2. Try .autokey file and migrate to keyring
	if key, err := LoadAutoKey(autoKeyPath); err == nil && len(key) == KeyLength {
		v.autoKey = key
		// Best-effort migration to OS keyring (keyring already confirmed empty in step 1)
		if err := saveKeyToKeyring(key); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to migrate autokey to OS keyring: %v\n", err)
		}
		return v, nil
	}

	// 3. Generate new key
	key := make([]byte, KeyLength)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate auto key: %w", err)
	}
	v.autoKey = key

	// Save to both OS keyring and .autokey file for redundancy
	if err := saveKeyToKeyring(key); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to save key to OS keyring: %v\n", err)
	}
	if err := SaveAutoKey(autoKeyPath, key); err != nil {
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
// If main password is set, the auto key is first derived from it using PBKDF2
func (v *Vault) Encrypt(data string) (string, error) {
	// Use auto key as base
	key := v.autoKey
	var salt []byte

	// If main password is set, derive key using PBKDF2
	if v.mainPass != "" {
		salt = make([]byte, SaltLength)
		if _, err := rand.Read(salt); err != nil {
			return "", fmt.Errorf("failed to generate salt: %w", err)
		}
		key = deriveKeyPBKDF2(v.mainPass, salt)
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
	saltForStorage := v.autoKey[:SaltLength]
	version := ""
	if salt != nil {
		saltForStorage = salt
		version = vaultVersionV2
	}
	result := VaultData{
		Version:    version,
		Salt:       base64.StdEncoding.EncodeToString(saltForStorage),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}

	encryptedData, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(encryptedData), nil
}

// Decrypt decrypts data using AES-GCM with the auto key
// Supports both legacy (XOR KDF) and v2 (PBKDF2) formats
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
	if len(salt) < SaltLength {
		return "", fmt.Errorf("corrupted vault data: salt too short (%d bytes, need %d)", len(salt), SaltLength)
	}
	nonce, err := base64.StdEncoding.DecodeString(vd.Nonce)
	if err != nil {
		return "", fmt.Errorf("failed to decode nonce: %w", err)
	}
	if len(nonce) < NonceLength {
		return "", fmt.Errorf("corrupted vault data: nonce too short (%d bytes, need %d)", len(nonce), NonceLength)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(vd.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	// Use auto key as base
	key := v.autoKey

	// If main password is set, derive key based on version
	if v.mainPass != "" {
		switch vd.Version {
		case vaultVersionV2:
			key = deriveKeyPBKDF2(v.mainPass, salt[:SaltLength])
		default:
			// Legacy: XOR-based KDF (backward compatibility)
			key = deriveKeyLegacy(v.mainPass, salt[:SaltLength])
		}
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
		fmt.Fprintf(os.Stderr, "WARNING: decryption failed: %v\n", err)
		return data, fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}

// deriveKeyPBKDF2 derives a key from password using PBKDF2 with SHA-256
// This is the secure replacement for the legacy XOR-based KDF
func deriveKeyPBKDF2(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, PBKDF2Iterations, KeyLength, sha256.New)
}

// deriveKeyLegacy is the original XOR-based KDF kept for backward compatibility
// Deprecated: only used to decrypt data encrypted before the PBKDF2 migration
func deriveKeyLegacy(password string, salt []byte) []byte {
	result := make([]byte, KeyLength)
	copy(result, salt)

	for i := 0; i < 10000; i++ {
		for j := 0; j < len(password); j++ {
			result[j%KeyLength] ^= password[j]
		}
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
