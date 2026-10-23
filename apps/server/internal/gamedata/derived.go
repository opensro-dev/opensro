/*
===========================================================================

derived.go - one table of the projection's derived data

Derived data is what the bundle build copies from the operator's own
<game root>/source-data/server (DerivedDirName). Each loader reads its table
through ReadDerived: an empty folder or an absent table is not an error,
only the feature that table feeds stays off.

===========================================================================
*/
package gamedata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

/*
================
ReadDerived

The bytes of name (a slash path) under dir, or nil when dir is empty or
the table is absent.
================
*/
func ReadDerived(dir, name string) ([]byte, error) {
	if dir == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("derived %s: %w", name, err)
	}
	return data, nil
}
