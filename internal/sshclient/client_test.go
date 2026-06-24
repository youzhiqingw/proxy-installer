package sshclient

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// NormalizeHost tests
// ---------------------------------------------------------------------------

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain hostname", "192.168.1.1", "192.168.1.1"},
		{"http prefix", "http://example.com", "example.com"},
		{"https prefix", "https://example.com", "example.com"},
		{"brackets", "[::1]", "::1"},
		{"http and brackets combined", "http://[::1]", "::1"},
		{"hostname with spaces", "  host.example.com  ", "host.example.com"},
		{"empty string", "", ""},
		{"only brackets", "[]", ""},
		{"domain with port", "http://example.com:8080", "example.com"},
		{"https domain with path", "https://example.com/path", "example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeHost(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeHost(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HostKeyStore helpers
// ---------------------------------------------------------------------------

func newTempStore(t *testing.T) *HostKeyStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hostkeys.json")
	store, err := NewHostKeyStore(path)
	if err != nil {
		t.Fatalf("NewHostKeyStore: %v", err)
	}
	return store
}

func storePath(s *HostKeyStore) string {
	return s.path
}

// ---------------------------------------------------------------------------
// NewHostKeyStore — creates store, handles missing file gracefully
// ---------------------------------------------------------------------------

func TestNewHostKeyStore_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	store, err := NewHostKeyStore(path)
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if len(store.Entries()) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(store.Entries()))
	}
}

// ---------------------------------------------------------------------------
// Add and Get
// ---------------------------------------------------------------------------

func TestAddAndGet(t *testing.T) {
	store := newTempStore(t)
	keys := []byte("ssh-rsa AAAA...")

	if err := store.Add("host1", 22, keys); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := store.Get("host1", 22)
	if !ok {
		t.Fatal("expected to find entry after Add")
	}
	if !bytes.Equal(got, keys) {
		t.Errorf("Get keys = %q, want %q", got, keys)
	}
}

// ---------------------------------------------------------------------------
// Get non-existent entry
// ---------------------------------------------------------------------------

func TestGetNonExistent(t *testing.T) {
	store := newTempStore(t)
	_, ok := store.Get("no-such-host", 22)
	if ok {
		t.Error("expected ok=false for non-existent entry")
	}
}

// ---------------------------------------------------------------------------
// Add updates existing entry (duplicate add)
// ---------------------------------------------------------------------------

func TestAddDuplicateUpdates(t *testing.T) {
	store := newTempStore(t)
	keys1 := []byte("key-v1")
	keys2 := []byte("key-v2")

	_ = store.Add("host1", 22, keys1)
	if err := store.Add("host1", 22, keys2); err != nil {
		t.Fatalf("Add duplicate: %v", err)
	}

	got, ok := store.Get("host1", 22)
	if !ok {
		t.Fatal("expected entry after duplicate Add")
	}
	if !bytes.Equal(got, keys2) {
		t.Errorf("Get after duplicate Add = %q, want %q", got, keys2)
	}

	// Should still be only one entry.
	entries := store.Entries()
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after duplicate Add, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// Remove
// ---------------------------------------------------------------------------

func TestAddAndRemove(t *testing.T) {
	store := newTempStore(t)
	_ = store.Add("host1", 22, []byte("key"))

	removed := store.Remove("host1", 22)
	if !removed {
		t.Error("expected Remove to return true")
	}

	_, ok := store.Get("host1", 22)
	if ok {
		t.Error("expected entry to be gone after Remove")
	}
	if len(store.Entries()) != 0 {
		t.Errorf("expected 0 entries after Remove, got %d", len(store.Entries()))
	}
}

// ---------------------------------------------------------------------------
// Remove non-existent
// ---------------------------------------------------------------------------

func TestRemoveNonExistent(t *testing.T) {
	store := newTempStore(t)
	removed := store.Remove("ghost", 9999)
	if removed {
		t.Error("expected Remove to return false for non-existent entry")
	}
}

// ---------------------------------------------------------------------------
// Entries
// ---------------------------------------------------------------------------

func TestEntries(t *testing.T) {
	store := newTempStore(t)
	_ = store.Add("a", 22, []byte("ka"))
	_ = store.Add("b", 2222, []byte("kb"))
	_ = store.Add("c", 8022, []byte("kc"))

	entries := store.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	hosts := map[string]bool{}
	for _, e := range entries {
		hosts[e.Host] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !hosts[want] {
			t.Errorf("missing host %q in Entries()", want)
		}
	}
}

// ---------------------------------------------------------------------------
// Save and Reload persistence
// ---------------------------------------------------------------------------

func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hostkeys.json")

	// Create store, add entries.
	s1, err := NewHostKeyStore(path)
	if err != nil {
		t.Fatalf("NewHostKeyStore: %v", err)
	}
	_ = s1.Add("persist-host", 22, []byte("persist-key"))
	_ = s1.Add("persist-other", 2222, []byte("persist-other-key"))

	// Open a second store from the same file — data should be loaded.
	s2, err := NewHostKeyStore(path)
	if err != nil {
		t.Fatalf("reload NewHostKeyStore: %v", err)
	}

	got, ok := s2.Get("persist-host", 22)
	if !ok {
		t.Fatal("expected entry after reload")
	}
	if !bytes.Equal(got, []byte("persist-key")) {
		t.Errorf("reloaded keys = %q, want %q", got, "persist-key")
	}

	entries := s2.Entries()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries after reload, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// Reload preserves entry fields (Host, Port, Hash, Added)
// ---------------------------------------------------------------------------

func TestReloadPreservesFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hostkeys.json")

	s1, err := NewHostKeyStore(path)
	if err != nil {
		t.Fatalf("NewHostKeyStore: %v", err)
	}
	_ = s1.Add("fieldhost", 22, []byte("fieldkey"))

	s2, err := NewHostKeyStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	entries := s2.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Host != "fieldhost" {
		t.Errorf("Host = %q, want %q", e.Host, "fieldhost")
	}
	if e.Port != 22 {
		t.Errorf("Port = %d, want 22", e.Port)
	}
	if e.Hash == "" {
		t.Error("expected non-empty Hash after reload")
	}
	if e.Added == "" {
		t.Error("expected non-empty Added timestamp after reload")
	}
}

// ---------------------------------------------------------------------------
// Remove persists to disk
// ---------------------------------------------------------------------------

func TestRemovePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hostkeys.json")

	s1, _ := NewHostKeyStore(path)
	_ = s1.Add("ephemeral", 22, []byte("key"))
	_ = s1.Add("permanent", 22, []byte("key"))
	s1.Remove("ephemeral", 22)

	// Reload from disk.
	s2, err := NewHostKeyStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	_, ok := s2.Get("ephemeral", 22)
	if ok {
		t.Error("expected removed entry to not survive reload")
	}
	_, ok = s2.Get("permanent", 22)
	if !ok {
		t.Error("expected surviving entry to persist")
	}
}

// ---------------------------------------------------------------------------
// Load handles corrupt JSON gracefully
// ---------------------------------------------------------------------------

func TestLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hostkeys.json")

	if err := os.WriteFile(path, []byte("{not valid json"), 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	_, err := NewHostKeyStore(path)
	if err == nil {
		t.Fatal("expected error when loading corrupt JSON file")
	}
}

// ---------------------------------------------------------------------------
// Different ports for same host are distinct entries
// ---------------------------------------------------------------------------

func TestSameHostDifferentPorts(t *testing.T) {
	store := newTempStore(t)
	_ = store.Add("multi", 22, []byte("port22"))
	_ = store.Add("multi", 2222, []byte("port2222"))

	got1, ok1 := store.Get("multi", 22)
	got2, ok2 := store.Get("multi", 2222)
	if !ok1 || !ok2 {
		t.Fatal("expected both port entries to be found")
	}
	if !bytes.Equal(got1, []byte("port22")) {
		t.Errorf("port 22 keys = %q", got1)
	}
	if !bytes.Equal(got2, []byte("port2222")) {
		t.Errorf("port 2222 keys = %q", got2)
	}
	if len(store.Entries()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(store.Entries()))
	}
}

// ---------------------------------------------------------------------------
// On-disk format is valid JSON array
// ---------------------------------------------------------------------------

func TestOnDiskFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hostkeys.json")

	store, _ := NewHostKeyStore(path)
	_ = store.Add("fmt-host", 443, []byte("fmt-key"))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read on-disk file: %v", err)
	}

	var entries []HostKeyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("on-disk data is not valid JSON array: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry on disk, got %d", len(entries))
	}
}
