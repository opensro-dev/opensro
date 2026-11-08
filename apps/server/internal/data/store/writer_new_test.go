package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteNewFileAtomicPublishesOnceWithoutClobber(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "credential")
	if err := WriteNewFileAtomic(path, []byte("first\n")); err != nil {
		t.Fatalf("WriteNewFileAtomic(first): %v", err)
	}
	if err := WriteNewFileAtomic(path, []byte("second\n")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("WriteNewFileAtomic(second) = %v, want os.ErrExist", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "first\n" {
		t.Fatalf("published payload = %q, want first writer", payload)
	}
}
