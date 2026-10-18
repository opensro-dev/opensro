/*
===========================================================================

writer.go - atomic file writes, stray temps and quarantine pruning

===========================================================================
*/

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"opensro.online/server/internal/platform/privatepath"
)

/*
==================
tmpPattern

tmpPattern is the CreateTemp pattern for a target file name; strays are
matched by the same prefix at open (a reaped mid-write can only strand
one of these, never tear the target).
==================
*/
func tmpPattern(base string) string {
	return base + ".tmp-*"
}

/*
==================
writeFileAtomic

writeFileAtomic writes payload via temp file + fsync + rename + parent
directory sync. The file fsync runs before close so even a power loss
cannot publish an empty rename target; the DIRECTORY sync afterwards
makes the rename itself (the publish) durable - without it a power
loss can keep the bytes but lose the entry. A plain process kill needs
only the rename ordering.
==================
*/
func writeFileAtomic(path string, payload []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, authorityDirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, tmpPattern(filepath.Base(path)))
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := syncFile(tmp); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// Windows cannot fsync a directory; syncDir is a documented no-op
	// there (dirsync_windows.go), so the write never fails on a platform
	// that cannot do it.
	return syncDir(dir)
}

/*
==================
Dir

Returns the authority directory path owned by this store.
==================
*/
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

/*
==================
WriteFileAtomic

WriteFileAtomic exposes the durable file writer to sibling maintenance
tools. The store itself reaches the unexported name through its failpoint
seam.
==================
*/
func WriteFileAtomic(path string, payload []byte) error {
	return writeFileAtomic(path, payload)
}

/*
==================
WriteNewFileAtomic

WriteNewFileAtomic durably publishes a private file only when the target
does not exist. The hard-link publish is atomic and no-clobber on Windows
and Unix, closing the check-then-rename race for credential provisioning.
==================
*/
func WriteNewFileAtomic(path string, payload []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, authorityDirMode); err != nil {
		return err
	}
	if err := privatepath.ProtectDirectory(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, tmpPattern(filepath.Base(path)))
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if _, err := tmp.Write(payload); err != nil {
		cleanup()
		return err
	}
	if err := syncFile(tmp); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := privatepath.ProtectFile(tmpPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Link(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Remove(tmpPath); err != nil {
		return err
	}
	return syncDir(dir)
}

/*
==================
cleanStrayTemps

Removes temp files a reaped writer stranded beside the target. Returns what
it removed so the caller can log it.
==================
*/
func cleanStrayTemps(path string) []string {
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), tmpPattern(filepath.Base(path))))
	if err != nil {
		return nil
	}
	removed := make([]string, 0, len(matches))
	for _, stray := range matches {
		if os.Remove(stray) == nil {
			removed = append(removed, stray)
		}
	}
	return removed
}

// corruptSuffix marks quarantined files: <base>.corrupt-<unix ms>.
const corruptSuffix = ".corrupt-"

/*
==================
quarantine

Renames a bad file aside, preserving its bytes for forensics, and prunes
old quarantines beyond keepQuarantines.
==================
*/
func quarantine(path string, now time.Time) (string, error) {
	dest := fmt.Sprintf("%s%s%d", path, corruptSuffix, now.UnixMilli())
	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	pruneQuarantines(path)
	return dest, nil
}

// keepQuarantines caps the quarantine debris surface (retention
// contract: newest 3 kept; older ones are sweep-eligible after triage).
const keepQuarantines = 3

/*
==================
pruneQuarantines

pruneQuarantines deletes all but the newest keepQuarantines quarantine
files for the target. Names embed unix milliseconds at fixed magnitude,
so lexicographic order is chronological for this codebase's lifetime.
==================
*/
func pruneQuarantines(path string) {
	matches, err := filepath.Glob(path + corruptSuffix + "*")
	if err != nil || len(matches) <= keepQuarantines {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-keepQuarantines] {
		os.Remove(old)
	}
}
