package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Initialize creates an empty current-schema authority database for a fresh
// deployment. It never overwrites an existing database or recovery
// generation.
func Initialize(dir string) error {
	var err error
	dir, err = resolveAuthorityDir(dir)
	if err != nil {
		return err
	}
	if err := ensureAuthorityDirectory(dir); err != nil {
		return fmt.Errorf("authority dir: %w", err)
	}

	lockFile, warning, err := acquireLock(filepath.Join(dir, LockFileName), time.Now())
	if err != nil {
		return err
	}
	defer releaseLock(lockFile)
	if warning != "" {
		fmt.Fprintf(os.Stderr, "store: %s\n", warning)
	}

	for _, name := range []string{DBFileName, DBBakFileName} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists: refusing to initialize over authority data", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("checking %s: %w", path, err)
		}
	}

	data := emptyAuthoritySeed(time.Now().UnixMilli())
	if err := createAuthorityDatabase(filepath.Join(dir, DBFileName), data); err != nil {
		return fmt.Errorf("initializing authority database: %w", err)
	}
	return nil
}
