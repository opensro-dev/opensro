package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeRequiresAuthorityDirectory(t *testing.T) {
	t.Setenv(EnvStateDir, "")
	if err := Initialize(""); err == nil ||
		!strings.Contains(err.Error(), "authority directory is required") {
		t.Fatalf("Initialize(\"\") error = %v, want required-directory refusal", err)
	}
}

func TestInitializeCreatesFreshProductionStore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := Initialize(dir); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, DBFileName)); err != nil {
		t.Fatalf("initialized database: %v", err)
	}
	authority, err := Open(dir, Options{RequireStore: true})
	if err != nil {
		t.Fatalf("production open of initialized store: %v", err)
	}
	authority.Close()

	if err := Initialize(dir); err == nil {
		t.Fatal("second Initialize overwrote an existing store")
	}
}
