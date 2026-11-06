/*
===========================================================================

key.go - the shard's census key: minted once, kept with its secrets

The connection journal's address hashes must stay comparable for the
retention window, so their key cannot be a per-process random like
the refusal log's: a restart would orphan every open correlation.
The shard's state directory is its secret store - the operator token
lives there - so the key lives there too: 32 random bytes, hex, in a
0600 file created on first use and read on every later boot. It never
rides the wire, never enters a log, and rotates only when an operator
deletes the file (which orphans the old hashes on purpose).

===========================================================================
*/
package connjournal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// keyBytes is the size of the minted census key.
const keyBytes = 32

/*
================
LoadOrCreateKey

The key in the file at path, minted there when the file does not
exist. A concurrent mint loses the create race and reads the winner's
key, so two processes can never disagree about it. A file that exists
but does not hold one key is refused, never silently replaced:
replacing it would orphan every hash already journaled.
================
*/
func LoadOrCreateKey(path string) ([]byte, error) {
	if key, found, err := readKey(path); err != nil || found {
		return key, err
	}
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		key, _, err := readKey(path)
		return key, err
	}
	if err != nil {
		return nil, err
	}
	if _, err := file.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

/*
================
readKey
================
*/
func readKey(path string) (key []byte, found bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	key, err = hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != keyBytes {
		return nil, false, fmt.Errorf("connjournal: %s does not hold one %d-byte key", path, keyBytes)
	}
	return key, true, nil
}
