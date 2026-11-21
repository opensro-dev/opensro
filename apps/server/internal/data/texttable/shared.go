/*
===========================================================================

shared.go - one immutable parse of the same shipped files per process

Every loader over the same textdata files shares one parse: test binaries
build many loaders over one directory, and re-parsing the retail tables
for each cost most of their time and memory. The key names every source
file by path, size and stamp, and by its bytes while the file is fresh
enough to have been rewritten within one timestamp tick, so a staged
rewrite is never served the previous parse. Keep the shared value
derived and read-only;
a loader that must change rows copies them first, and a loader that moves
to bounded residency releases its entry so a long-lived process does not
pin it.

===========================================================================
*/
package texttable

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

/*
================
SharedParses
================
*/
type SharedParses[T any] struct {
	mu      sync.Mutex
	entries map[string]*T
}

/*
================
SharedParses.Get

The parse published for key, running parse once per key.
================
*/
func (s *SharedParses[T]) Get(key string, parse func() *T) *T {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.entries[key]; ok {
		return v
	}
	v := parse()
	if s.entries == nil {
		s.entries = map[string]*T{}
	}
	s.entries[key] = v
	return v
}

/*
================
SharedParses.Release

Forgets one parse; loaders already holding it keep reading it.
================
*/
func (s *SharedParses[T]) Release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
}

// fingerprintRacyWindow is how fresh a file's modification time must be
// for the fingerprint to also name its bytes (see Fingerprint). Two
// seconds matches git's racy-clean rule of thumb: only a file written
// moments ago can have been rewritten inside one timestamp tick, and
// hashing the shipped tables on every loader cost the action suite a
// third of its runtime.
const fingerprintRacyWindow = 2 * time.Second

/*
================
Fingerprint

Identifies the bytes a parse of paths reads: the absolute path, the
size and the modification time, plus a SHA-256 of the contents while
the file is racy - modified within fingerprintRacyWindow of now, or
later (clock skew). Path, size and time alone cannot name the bytes of
a file that fresh: a same-size rewrite inside one timestamp tick of a
coarse filesystem keeps all three (a test staging a link condition in
place read the previous parse back). A settled file names itself by its
stamp - the accepted racy-clean limit, git's rule: a rewrite backdated
to an old identical stamp still collides, and nothing stages that way.
The same bytes under a newer stamp fingerprint apart and reparse once.
================
*/
func Fingerprint(paths ...string) string {
	now := time.Now()
	var b strings.Builder
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		b.WriteString(abs)
		info, err := os.Stat(path)
		if err != nil {
			b.WriteString("|absent")
			b.WriteByte('\n')
			continue
		}
		b.WriteByte('|')
		b.WriteString(strconv.FormatInt(info.Size(), 10))
		b.WriteByte('|')
		b.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
		if !info.ModTime().Before(now.Add(-fingerprintRacyWindow)) {
			if sum, _, err := hashFile(path); err == nil {
				b.WriteByte('|')
				b.WriteString(hex.EncodeToString(sum))
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

/*
================
hashFile

The SHA-256 and size of one file, streamed so a large table is never
held whole just to name it.
================
*/
func hashFile(path string) ([]byte, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return nil, 0, err
	}
	return hash.Sum(nil), size, nil
}
