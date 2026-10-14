/*
===========================================================================

snapshot.go - a read-only copy of the committed authority for readers
that must never touch the live file

Port-only: the staff economy view reads its aggregates from this copy
(platform/economy), never from the live database per request. BackupTo
holds the store's lock for the whole copy, which would hold off every door
and stall the tick on a full database; a snapshot instead copies through
its own connection, which only reads the source. In WAL mode that
connection reads one consistent committed generation while doors keep
committing (the WAL only grows until the copy ends), so a snapshot costs
disk and CPU, never a door.

===========================================================================
*/
package store

import (
	"fmt"
	"os"
)

/*
================
SnapshotTo

Writes the committed authority to path through a separate connection,
replacing any earlier copy only once the new one is complete (VACUUM INTO
a sibling, then rename). Safe to call from any goroutine but
never from inside a door.
================
*/
func (s *Store) SnapshotTo(path string) error {
	s.mu.RLock()
	source, open := s.dbPath, s.db != nil
	s.mu.RUnlock()
	if !open {
		return fmt.Errorf("snapshot: no database open")
	}
	db, err := connectDB(source)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	defer db.Close()
	// The connection runs VACUUM INTO alone, which reads the source and
	// writes only the target (query_only would refuse that target write).
	partial := path + ".partial"
	if err := os.Remove(partial); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := db.Exec("VACUUM INTO ?", partial); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("snapshot: %w", err)
	}
	if err := os.Rename(partial, path); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("snapshot: %w", err)
	}
	return nil
}
