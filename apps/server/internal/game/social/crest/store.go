/*
===========================================================================

store.go - where re-encoded crests live: content-addressed payloads
and the standing crest of every guild and union

Owner contract (2026-11-07): only the re-encoded 256 bytes are ever
stored, named by their SHA-256, in one fixed directory; the user's
file name is never used and no path is ever built from user input.
The native crest name (G{prefix}_{guild}_{revision}.crb) stays the
serving key: the index here says which hash a guild's or union's
current revision holds, so the /marks/ handler resolves a requested
name to the payload file without trusting anything in the request.

A guild's change is rate-limited (one per ChangeCooldown); the limit
and the index live in memory over the index file, which is rewritten
atomically on every change so a restart keeps the standing crests.

Revisions are never reused: a delete returns the wire revision to 0,
but the index remembers the highest revision it ever issued for a
crest, so a crest registered after a delete is named for art no
browser can hold cached under an older revision's immutable URL.

===========================================================================
*/
package crest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChangeCooldown is the owner's per-guild rate limit between crest
// changes (2026-11-07: "e.g. one change per 10 minutes").
const ChangeCooldown = 10 * time.Minute

// The crest kinds an upload or removal names.
const (
	KindGuild = "guild"
	KindUnion = "union"
)

// Ref is one standing crest: the wire revision and the payload hash.
type Ref struct {
	Revision uint32 `json:"revision"`
	Hash     string `json:"hash"`
}

/*
================
Store

The crest directory's owner. Safe for concurrent use.
================
*/
type Store struct {
	dir string

	mu         sync.Mutex
	current    map[string]Ref
	issued     map[string]uint32
	lastChange map[string]time.Time
}

// indexFile is the persisted shape of the store's index: the standing
// crest of every guild and union, and the highest revision ever
// issued for each (never lowered, so revisions are never reused).
type indexFile struct {
	Current map[string]Ref    `json:"current"`
	Issued  map[string]uint32 `json:"issued,omitempty"`
}

/*
================
NewStore

Open (or create) the crest store in dir, reading the standing-crest
index back when one exists.
================
*/
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("crest: open store directory: %w", err)
	}
	store := &Store{
		dir:        dir,
		current:    map[string]Ref{},
		issued:     map[string]uint32{},
		lastChange: map[string]time.Time{},
	}
	raw, err := os.ReadFile(store.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("crest: read index: %w", err)
	}
	var index indexFile
	if err := json.Unmarshal(raw, &index); err != nil {
		return nil, fmt.Errorf("crest: parse index: %w", err)
	}
	for key, ref := range index.Current {
		store.current[key] = ref
	}
	for key, revision := range index.Issued {
		store.issued[key] = revision
	}
	return store, nil
}

/*
================
key

The index key of one crest: kind letter and owner id ("G:12").
================
*/
func key(kind string, id int64) string {
	letter := "G"
	if kind == KindUnion {
		letter = "A"
	}
	return fmt.Sprintf("%s:%d", letter, id)
}

/*
================
indexPath
================
*/
func (store *Store) indexPath() string {
	return filepath.Join(store.dir, "crest-index.json")
}

/*
================
Save

Store one re-encoded payload under its SHA-256 name, returning the
hash. A payload already stored is left as it lies.
================
*/
func (store *Store) Save(payload []byte) (string, error) {
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	path := filepath.Join(store.dir, hash+".crb")
	if _, err := os.Lstat(path); err == nil {
		return hash, nil
	}
	tmp, err := os.CreateTemp(store.dir, ".crest-*")
	if err != nil {
		return "", fmt.Errorf("crest: stage payload: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("crest: write payload: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("crest: close payload: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("crest: commit payload: %w", err)
	}
	return hash, nil
}

/*
================
Payload

The stored payload bytes for a content hash, for the /marks/
handler. The hash is server-generated hex; the name is validated
before it is ever joined to the directory.
================
*/
func (store *Store) Payload(hash string) ([]byte, bool) {
	if !validHash(hash) {
		return nil, false
	}
	raw, err := os.ReadFile(filepath.Join(store.dir, hash+".crb"))
	if err != nil || len(raw) != CrestPayloadBytes {
		return nil, false
	}
	return raw, true
}

/*
================
validHash

A stored payload's name: exactly one lowercase SHA-256 hex digest.
================
*/
func validHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

/*
================
Current

The standing crest of one guild or union, when one stands.
================
*/
func (store *Store) Current(kind string, id int64) (Ref, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	ref, ok := store.current[key(kind, id)]
	return ref, ok
}

/*
================
Resolve

The payload hash behind one native crest name's revision, for the
/marks/ handler: only the standing revision resolves; a replaced
crest's old revision is gone, as replacing art requires a new
revision.
================
*/
func (store *Store) Resolve(kind string, id int64, revision uint32) (string, bool) {
	ref, ok := store.Current(kind, id)
	if !ok || ref.Revision != revision {
		return "", false
	}
	return ref.Hash, true
}

/*
================
SetCurrent

Record a new standing crest and persist the index atomically.
================
*/
func (store *Store) SetCurrent(kind string, id int64, ref Ref) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	k := key(kind, id)
	store.current[k] = ref
	if ref.Revision > store.issued[k] {
		store.issued[k] = ref.Revision
	}
	return store.saveIndexLocked()
}

/*
================
RemoveCurrent

Drop the standing crest (a delete or a staff removal). The payload
file stays: content-addressed bytes are shared and harmless, and
only the standing pointer makes a crest visible.
================
*/
func (store *Store) RemoveCurrent(kind string, id int64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.current, key(kind, id))
	return store.saveIndexLocked()
}

/*
================
NextRevision

The revision a new standing crest takes: one past both the revision
the game record currently names (current, 0 after a delete) and the
highest this store ever issued for the crest, so no revision ever
names two different arts under its immutable URL. Pure: SetCurrent
records the issue when the change commits.
================
*/
func (store *Store) NextRevision(kind string, id int64, current uint32) uint32 {
	store.mu.Lock()
	defer store.mu.Unlock()
	highest := store.issued[key(kind, id)]
	if current > highest {
		highest = current
	}
	return highest + 1
}

/*
================
ServeCrest

The standing payload behind one native crest name, for the /marks/
handler: the payload bytes when the name's revision is the standing
one, its content hash (for the cache validator) beside them.
================
*/
func (store *Store) ServeCrest(kind string, id int64, revision uint32) ([]byte, string, bool) {
	hash, ok := store.Resolve(kind, id, revision)
	if !ok {
		return nil, "", false
	}
	payload, ok := store.Payload(hash)
	if !ok {
		return nil, "", false
	}
	return payload, hash, true
}

/*
================
saveIndexLocked

Rewrite the index file atomically (stage, then rename).
================
*/
func (store *Store) saveIndexLocked() error {
	raw, err := json.Marshal(indexFile{Current: store.current, Issued: store.issued})
	if err != nil {
		return fmt.Errorf("crest: encode index: %w", err)
	}
	tmp, err := os.CreateTemp(store.dir, ".crest-index-*")
	if err != nil {
		return fmt.Errorf("crest: stage index: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("crest: write index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("crest: close index: %w", err)
	}
	if err := os.Rename(tmpName, store.indexPath()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("crest: commit index: %w", err)
	}
	return nil
}

/*
================
AllowChange

Whether this guild's (or union's) crest may change at now under the
rate limit. A crest that never changed in this process may always
change: the limit guards the upload lane, not restarts.
================
*/
func (store *Store) AllowChange(kind string, id int64, now time.Time) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	last, ok := store.lastChange[key(kind, id)]
	return !ok || now.Sub(last) >= ChangeCooldown
}

/*
================
MarkChange

Record that this crest changed at now (a committed upload).
================
*/
func (store *Store) MarkChange(kind string, id int64, now time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.lastChange[key(kind, id)] = now
}
