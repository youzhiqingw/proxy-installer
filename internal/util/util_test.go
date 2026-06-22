package util

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// ShellQuote
// ---------------------------------------------------------------------------

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "''"},
		{"normal word", "hello", "'hello'"},
		{"with spaces", "hello world", "'hello world'"},
		{"with single quote", "it's", "'it'\"'\"'s'"},
		{"only single quote", "'", "''\"'\"''"},
		{"double quotes preserved", `"quoted"`, `'"quoted"'`},
		{"special chars", "a$b`c`", "'a$b`c`'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShellQuote(tt.in)
			if got != tt.want {
				t.Errorf("ShellQuote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// B64
// ---------------------------------------------------------------------------

func TestB64(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"hello", "hello", base64.StdEncoding.EncodeToString([]byte("hello"))},
		{"unicode", "你好", base64.StdEncoding.EncodeToString([]byte("你好"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := B64(tt.in)
			if got != tt.want {
				t.Errorf("B64(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// B64JSON
// ---------------------------------------------------------------------------

func TestB64JSON(t *testing.T) {
	t.Run("map value", func(t *testing.T) {
		input := map[string]string{"key": "value"}
		got := B64JSON(input)

		// Decode base64 then unmarshal JSON to verify round-trip.
		raw, err := base64.StdEncoding.DecodeString(got)
		if err != nil {
			t.Fatalf("base64 decode error: %v", err)
		}
		var decoded map[string]string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("json unmarshal error: %v", err)
		}
		if !reflect.DeepEqual(decoded, input) {
			t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, input)
		}
	})

	t.Run("struct value", func(t *testing.T) {
		type sample struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		input := sample{Name: "alice", Age: 30}
		got := B64JSON(input)

		raw, err := base64.StdEncoding.DecodeString(got)
		if err != nil {
			t.Fatalf("base64 decode error: %v", err)
		}
		var decoded sample
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("json unmarshal error: %v", err)
		}
		if decoded != input {
			t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, input)
		}
	})
}

// ---------------------------------------------------------------------------
// ParseKeyValue
// ---------------------------------------------------------------------------

func TestParseKeyValue(t *testing.T) {
	tests := []struct {
		name string
		text string
		want map[string]string
	}{
		{
			"multi-line",
			"HOST=example.com\nPORT=8080",
			map[string]string{"HOST": "example.com", "PORT": "8080"},
		},
		{
			"empty value",
			"KEY=\nOTHER=val",
			map[string]string{"KEY": "", "OTHER": "val"},
		},
		{
			"missing equals sign skipped",
			"noequalshere\nKEY=val",
			map[string]string{"KEY": "val"},
		},
		{
			"whitespace trimmed on line and value only",
			"  KEY  =  value  \n  A  =  B  ",
			map[string]string{"KEY  ": "value", "A  ": "B"},
		},
		{
			"value contains equals",
			"CONN=host=db;port=5432",
			map[string]string{"CONN": "host=db;port=5432"},
		},
		{
			"empty input",
			"",
			map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseKeyValue(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseKeyValue() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StripANSI
// ---------------------------------------------------------------------------

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no escapes", "plain text", "plain text"},
		{"simple color code", "\x1b[31mhello\x1b[0m", "hello"},
		{"bold + color", "\x1b[1m\x1b[32mOK\x1b[0m", "OK"},
		{"empty after strip", "\x1b[31m\x1b[0m", ""},
		{"trailing space trimmed", "  hello  ", "hello"},
		{"mixed content", "pre\x1b[33m mid\x1b[0m post", "pre mid post"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripANSI(tt.in)
			if got != tt.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TrimForMessage
// ---------------------------------------------------------------------------

func TestTrimForMessage(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"max zero returns full string", "hello world", 0, "hello world"},
		{"max negative returns full string", "hello", -5, "hello"},
		{"max greater than length", "hi", 100, "hi"},
		{"exact length", "hello", 5, "hello"},
		{"truncation adds ellipsis", "hello world", 5, "hello..."},
		{"ANSI stripped before truncation", "\x1b[31mhello world\x1b[0m", 5, "hello..."},
		{"whitespace trimmed first", "  hello world  ", 5, "hello..."},
		{"short string no truncation", "ok", 10, "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrimForMessage(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("TrimForMessage(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FlattenJSON / FlattenAny
// ---------------------------------------------------------------------------

func TestFlattenJSON(t *testing.T) {
	t.Run("nested map", func(t *testing.T) {
		input := map[string]any{
			"server": map[string]any{
				"host": "example.com",
				"port": float64(8080),
			},
		}
		out := map[string]string{}
		FlattenJSON("", input, out)

		if out["server.host"] != "example.com" {
			t.Errorf("server.host = %q, want %q", out["server.host"], "example.com")
		}
		if out["server.port"] != "8080" {
			t.Errorf("server.port = %q, want %q", out["server.port"], "8080")
		}
	})

	t.Run("array", func(t *testing.T) {
		input := map[string]any{
			"items": []any{"alpha", "beta"},
		}
		out := map[string]string{}
		FlattenJSON("", input, out)

		if out["items[0]"] != "alpha" {
			t.Errorf("items[0] = %q, want %q", out["items[0]"], "alpha")
		}
		if out["items[1]"] != "beta" {
			t.Errorf("items[1] = %q, want %q", out["items[1]"], "beta")
		}
	})

	t.Run("scalar string", func(t *testing.T) {
		out := map[string]string{}
		FlattenJSON("key", "value", out)
		if out["key"] != "value" {
			t.Errorf("got %q, want %q", out["key"], "value")
		}
	})

	t.Run("scalar float64", func(t *testing.T) {
		out := map[string]string{}
		FlattenJSON("n", float64(3.14), out)
		if out["n"] != "3.14" {
			t.Errorf("got %q, want %q", out["n"], "3.14")
		}
	})

	t.Run("scalar bool true", func(t *testing.T) {
		out := map[string]string{}
		FlattenJSON("flag", true, out)
		if out["flag"] != "true" {
			t.Errorf("got %q, want %q", out["flag"], "true")
		}
	})

	t.Run("scalar bool false", func(t *testing.T) {
		out := map[string]string{}
		FlattenJSON("flag", false, out)
		if out["flag"] != "false" {
			t.Errorf("got %q, want %q", out["flag"], "false")
		}
	})

	t.Run("nil value ignored", func(t *testing.T) {
		out := map[string]string{}
		FlattenJSON("key", nil, out)
		if _, exists := out["key"]; exists {
			t.Errorf("nil should not produce an entry, got %q", out["key"])
		}
	})

	t.Run("map[string]string directly", func(t *testing.T) {
		input := map[string]any{
			"tags": map[string]string{"env": "prod", "region": "us-east"},
		}
		out := map[string]string{}
		FlattenJSON("", input, out)
		if out["tags.env"] != "prod" {
			t.Errorf("tags.env = %q, want %q", out["tags.env"], "prod")
		}
		if out["tags.region"] != "us-east" {
			t.Errorf("tags.region = %q, want %q", out["tags.region"], "us-east")
		}
	})

	t.Run("string with ANSI stripped", func(t *testing.T) {
		out := map[string]string{}
		FlattenJSON("k", "\x1b[31mred\x1b[0m", out)
		if out["k"] != "red" {
			t.Errorf("got %q, want %q", out["k"], "red")
		}
	})
}

func TestFlattenAny(t *testing.T) {
	input := map[string]any{
		"a": "one",
		"b": float64(2),
		"c": true,
	}
	got := FlattenAny(input)

	want := map[string]string{
		"a": "one",
		"b": "2",
		"c": "true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FlattenAny() = %+v, want %+v", got, want)
	}
}

// ---------------------------------------------------------------------------
// FirstValue
// ---------------------------------------------------------------------------

func TestFirstValue(t *testing.T) {
	m := map[string]string{
		"primary":   "",
		"secondary": "backup",
		"tertiary":  "third",
		"blank":     "   ",
	}

	tests := []struct {
		name string
		keys []string
		want string
	}{
		{"first key has value", []string{"secondary", "tertiary"}, "backup"},
		{"skip empty, pick second", []string{"primary", "secondary"}, "backup"},
		{"all empty", []string{"primary", "blank"}, ""},
		{"missing keys", []string{"nonexistent"}, ""},
		{"no keys", []string{}, ""},
		{"whitespace-only skipped", []string{"blank", "tertiary"}, "third"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FirstValue(m, tt.keys...)
			if got != tt.want {
				t.Errorf("FirstValue(m, %v) = %q, want %q", tt.keys, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CompactJoin
// ---------------------------------------------------------------------------

func TestCompactJoin(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{"all non-empty", []string{"a", "b", "c"}, "a / b / c"},
		{"empty strings filtered", []string{"a", "", "c"}, "a / c"},
		{"whitespace-only filtered", []string{"a", "  ", "c"}, "a / c"},
		{"all empty", []string{"", "", ""}, ""},
		{"single value", []string{"solo"}, "solo"},
		{"empty input", []string{}, ""},
		{"leading and trailing empty", []string{"", "mid", ""}, "mid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompactJoin(tt.values...)
			if got != tt.want {
				t.Errorf("CompactJoin(%v) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SortedKeys
// ---------------------------------------------------------------------------

func TestSortedKeys(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]string
		want []string
	}{
		{
			"alphabetical",
			map[string]string{"banana": "2", "apple": "1", "cherry": "3"},
			[]string{"apple", "banana", "cherry"},
		},
		{
			"single key",
			map[string]string{"only": "1"},
			[]string{"only"},
		},
		{
			"empty map",
			map[string]string{},
			[]string{},
		},
		{
			"numeric-looking keys",
			map[string]string{"10": "x", "2": "y", "1": "z"},
			[]string{"1", "10", "2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SortedKeys(tt.m)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SortedKeys() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LooksLikeHTML
// ---------------------------------------------------------------------------

func TestLooksLikeHTML(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"doctype html", "<!DOCTYPE html><html><body>hi</body></html>", true},
		{"lowercase html tag", "<html><body>content</body></html>", true},
		{"head tag", "<head><title>Page</title></head>", true},
		{"plain text", `{"status":"ok","data":[]}`, false},
		{"JSON response", `{"error": "not found"}`, false},
		{"empty string", "", false},
		{"case insensitive DOCTYPE", "<!doctype HTML>", true},
		{"leading whitespace", "   <html>", true},
		{"long non-HTML prefix then html tag", string(make([]byte, 300)) + "<html>", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeHTML(tt.text)
			if got != tt.want {
				t.Errorf("LooksLikeHTML(%q) = %v, want %v", truncate(tt.text, 60), got, tt.want)
			}
		})
	}
}

// truncate is a test helper to keep test output readable.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
