package vault

// keyring.go - Keyring is not implemented for MVP
// The .autokey file fallback is sufficient for Windows

// getKeyFromKeyring attempts to load key from OS keyring
// Returns error to fall back to .autokey file
func getKeyFromKeyring() ([]byte, error) {
	return nil, errNoKeyring
}

// saveKeyToKeyring attempts to save key to OS keyring
// Returns error to fall back to .autokey file
func saveKeyToKeyring(key []byte) error {
	return errNoKeyring
}

var errNoKeyring = errorf("keyring not available")

func errorf(format string, args ...interface{}) error {
	return &keyringError{fmt: format, args: args}
}

type keyringError struct {
	fmt  string
	args []interface{}
}

func (e *keyringError) Error() string {
	return e.fmt
}
