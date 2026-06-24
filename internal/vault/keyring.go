package vault

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

// keyringService is the service name used for OS keyring entries
const keyringService = "proxy-installer"

// keyringUser is the account/user identifier for the keyring entry
const keyringUser = "autokey"

// errNoKeyring is returned when the OS keyring is unavailable
var errNoKeyring = errors.New("keyring not available")

// getKeyFromKeyring attempts to load the encryption key from the OS keyring
// Returns the key bytes or an error if unavailable
func getKeyFromKeyring() ([]byte, error) {
	secret, err := keyring.Get(keyringService, keyringUser)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, errNoKeyring
		}
		return nil, fmt.Errorf("keyring get: %w", err)
	}
	key, err := hex.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("keyring decode: %w", err)
	}
	if len(key) != KeyLength {
		return nil, fmt.Errorf("keyring key length mismatch: got %d, want %d", len(key), KeyLength)
	}
	return key, nil
}

// saveKeyToKeyring stores the encryption key in the OS keyring
// The key is hex-encoded before storage
func saveKeyToKeyring(key []byte) error {
	if len(key) != KeyLength {
		return fmt.Errorf("invalid key length: got %d, want %d", len(key), KeyLength)
	}
	secret := hex.EncodeToString(key)
	if err := keyring.Set(keyringService, keyringUser, secret); err != nil {
		return fmt.Errorf("keyring set: %w", err)
	}
	return nil
}

// MigrateAutoKeyToKeyring copies the key from .autokey file to the OS keyring
// This is called once on first use to seamlessly upgrade key storage
// Returns nil on success or if migration was not needed
func MigrateAutoKeyToKeyring(autoKeyPath string) error {
	// Check if key already exists in keyring
	if _, err := getKeyFromKeyring(); err == nil {
		return nil // Already in keyring, no migration needed
	}

	// Try to load from .autokey file
	key, err := LoadAutoKey(autoKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file to migrate from
		}
		return fmt.Errorf("load autokey for migration: %w", err)
	}
	if len(key) != KeyLength {
		return fmt.Errorf("autokey length mismatch: got %d, want %d", len(key), KeyLength)
	}

	// Save to OS keyring
	if err := saveKeyToKeyring(key); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to migrate autokey to OS keyring: %v\n", err)
		return err
	}

	fmt.Fprintf(os.Stdout, "INFO: Auto key migrated to OS keyring\n")
	return nil
}
