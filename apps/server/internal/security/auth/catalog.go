/*
===========================================================================

catalog.go - the deploy-time account catalog

A strict JSON file of account ids and bcrypt hashes: the seed the live
store (accounts.go) inserts from, and the authority tests read. Ids that
differ only in ASCII case are one login and are refused together.

===========================================================================
*/
// The account catalog is the global login credential authority.
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"golang.org/x/crypto/bcrypt"
	"opensro.online/server/internal/domain"
)

const (
	MaxFileBytes  int64 = 1 << 20
	MaxBcryptCost       = 12
)

// Catalog is an immutable account-id to bcrypt-hash index. ids maps each
// folded id (domain.FoldAccountID) to the id as written, so a login resolves
// case-insensitively like the live store (Accounts.Credential).
type Catalog struct {
	hashes map[string][]byte
	ids    map[string]string
}

/*
================
Load
================
*/
// Load reads a strict regular-file account catalog. Symlinks and file swaps
// are refused because this file is the global login authority.
func Load(path string) (*Catalog, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file (symlinks are not accepted)")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) {
		return nil, fmt.Errorf("account file changed while opening")
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("file is %d bytes, limit is %d", info.Size(), MaxFileBytes)
	}
	payload, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > MaxFileBytes {
		return nil, fmt.Errorf("file grew past %d bytes while reading", MaxFileBytes)
	}

	var rows []struct {
		ID           string `json:"id"`
		PasswordHash string `json:"passwordHash"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rows); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, fmt.Errorf("trailing JSON: %w", err)
	}

	hashes := make(map[string][]byte, len(rows))
	ids := make(map[string]string, len(rows))
	for _, row := range rows {
		if !domain.AccountIDValid(row.ID) {
			return nil, fmt.Errorf("account id %q is invalid", row.ID)
		}
		if row.ID == domain.ReservedAccountID {
			return nil, fmt.Errorf("account id %q is reserved", row.ID)
		}
		// Ids differing only in case are one login (the store's folded index).
		if existing, duplicate := ids[domain.FoldAccountID(row.ID)]; duplicate {
			return nil, fmt.Errorf("duplicate account id %q (already %q)", row.ID, existing)
		}
		hash := []byte(row.PasswordHash)
		cost, err := bcrypt.Cost(hash)
		if err != nil {
			return nil, fmt.Errorf("account %q has invalid bcrypt passwordHash: %w", row.ID, err)
		}
		if cost < bcrypt.DefaultCost || cost > MaxBcryptCost {
			return nil, fmt.Errorf(
				"account %q bcrypt cost %d is outside %d..%d",
				row.ID,
				cost,
				bcrypt.DefaultCost,
				MaxBcryptCost,
			)
		}
		hashes[row.ID] = append([]byte(nil), hash...)
		ids[domain.FoldAccountID(row.ID)] = row.ID
	}
	if len(hashes) == 0 {
		return nil, fmt.Errorf("no accounts in file")
	}
	return &Catalog{hashes: hashes, ids: ids}, nil
}

/*
================
PasswordHash
================
*/
// PasswordHash returns a detached bcrypt hash for login comparison.
func (catalog *Catalog) PasswordHash(accountID string) ([]byte, bool) {
	if catalog == nil {
		return nil, false
	}
	hash, ok := catalog.hashes[accountID]
	return append([]byte(nil), hash...), ok
}

/*
================
Credential
================
*/
// Credential resolves a typed login id to the catalog id and its hash,
// ignoring ASCII case as Accounts.Credential does.
func (catalog *Catalog) Credential(typedID string) (string, []byte, bool) {
	if catalog == nil {
		return "", nil, false
	}
	id, ok := catalog.ids[domain.FoldAccountID(typedID)]
	if !ok {
		return "", nil, false
	}
	return id, append([]byte(nil), catalog.hashes[id]...), true
}

/*
================
IDs
================
*/
// IDs returns detached account IDs for shard-state audits.
func (catalog *Catalog) IDs() []string {
	if catalog == nil {
		return nil
	}
	ids := make([]string, 0, len(catalog.hashes))
	for id := range catalog.hashes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

/*
================
Len
================
*/
// Len returns the number of configured global accounts.
func (catalog *Catalog) Len() int {
	if catalog == nil {
		return 0
	}
	return len(catalog.hashes)
}
