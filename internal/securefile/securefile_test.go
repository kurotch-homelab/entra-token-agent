package securefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "token")
	if err := Write(path, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "secret" {
		t.Fatalf("got %q", b)
	}
	assertPrivate(t, path)
	if err := Write(path, []byte("rotated")); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil || string(b) != "rotated" {
		t.Fatalf("replacement failed: %q, %v", b, err)
	}
	assertPrivate(t, path)
}
