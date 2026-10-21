/*
===========================================================================

shared.go - one immutable parse of the same shipped files per process

Every loader over the same textdata files shares one parse: test binaries
build many loaders over one directory, and re-parsing the retail tables
for each cost most of their time and memory. The key fingerprints every
source file (path, size, modification time), so a changed or re-extracted
projection is parsed afresh. Keep the shared value derived and read-only;
a loader that must change rows copies them first, and a loader that moves
to bounded residency releases its entry so a long-lived process does not
pin it.

===========================================================================
*/
package texttable

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

/*
================
Fingerprint

Identifies the exact bytes a parse of paths reads.
================
*/
func Fingerprint(paths ...string) string {
	var b strings.Builder
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		b.WriteString(abs)
		if info, err := os.Stat(path); err == nil {
			b.WriteByte('|')
			b.WriteString(strconv.FormatInt(info.Size(), 10))
			b.WriteByte('|')
			b.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
		} else {
			b.WriteString("|absent")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
