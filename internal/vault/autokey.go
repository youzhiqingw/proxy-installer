package vault

import (
	"encoding/base64"
	"os"
)

// LoadAutoKey loads the auto key from .autokey file
func LoadAutoKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// The file contains base64 encoded key
	return base64.StdEncoding.DecodeString(string(data))
}

// SaveAutoKey saves the auto key to .autokey file
func SaveAutoKey(path string, key []byte) error {
	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(key)

	// Create parent directory if not exists
	dir := path[:len(path)-len(".autokey")]
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Write with 0600 permissions
	return os.WriteFile(path, []byte(encoded), 0600)
}
