/*
===========================================================================

db.go - the SQLite authority store: open, layout version, quarantine

===========================================================================
*/

package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
	"opensro.online/server/internal/domain"
)

// The persistence engine uses SQLite in WAL mode, one row per entity,
// and one transaction per committed operation. The store contract is unchanged:
// one commit door, an operation is on disk wholly applied or wholly
// absent, boot fails closed, runtime write failures fail open and loud -
// only the mechanism moved from "marshal the whole world + atomic rename"
// (O(world) CPU and I/O plus an fsync per gameplay op) to "upsert the
// touched rows in a transaction" (O(delta)).
//
// Durability posture: synchronous=NORMAL under WAL makes every commit
// durable against process death (process killers, crashes, supervisor
// restarts); power-loss durability is bounded
// by WAL checkpoints (boot, clean shutdown, SQLite's auto-checkpoint).
// SRO_AUTHORITY_SYNC_FULL=1 opts into synchronous=FULL for full
// power-loss durability at ~1 fsync per commit.
//
// Engine choice rationale: single-writer WAL SQLite
// sustains 10k-50k write tx/s on NVMe - orders of magnitude above a
// single-shard gateway's op rate; the store's commit door is already the
// single writer, so SQLite's one-writer constraint costs nothing. The
// reference emulators (TrinityCore/MaNGOS) persist to MySQL every 90s-15m
// per character, i.e. WEAKER durability than this door; retail vSRO needs
// networked MSSQL because its topology is many PROCESSES sharing one DB,
// which this single-process gateway does not have. MongoDB was
// disqualified: multi-document transactions (the drop/pickup atomicity
// contract) require a replica set - a standalone mongod refuses them.
// If the deployment later needs multiple gateway processes sharing one
// authority, the plain SQL schema can move to a network database without
// changing domain ownership.
const (
	// DBFileName is the authority database.
	DBFileName = "state.db"
	// DBBakFileName is the previous-generation copy (VACUUM INTO at boot,
	// once per successful main-file load, following one-generation retention).
	DBBakFileName = DBFileName + ".bak"

	// EnvSyncFull opts into PRAGMA synchronous=FULL (power-loss-durable
	// commits at ~1 fsync each).
	EnvSyncFull = "SRO_AUTHORITY_SYNC_FULL"
)

// dbSchema creates a fresh current-layout database. Existing databases must
// already match CurrentLayoutVersion exactly and are never altered at boot.
const mallAccountsSchema = `
CREATE TABLE IF NOT EXISTS mall_accounts (
  account_id TEXT PRIMARY KEY,
  silk INTEGER NOT NULL CHECK (silk BETWEEN 0 AND 4294967295),
  gift_silk INTEGER NOT NULL CHECK (gift_silk BETWEEN 0 AND 4294967295),
  points INTEGER NOT NULL CHECK (points BETWEEN 0 AND 4294967295)
) WITHOUT ROWID;
`

const dbSchema = mallAccountsSchema + accountStorageSchema + fortressSchema + allianceSchema + `
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS characters (
  division   TEXT    NOT NULL,
  id         INTEGER NOT NULL,
  name_lower TEXT    NOT NULL,
  record     TEXT    NOT NULL,
  PRIMARY KEY (division, id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_characters_name
  ON characters (division, name_lower);
CREATE TABLE IF NOT EXISTS deleted_characters (
  division TEXT    NOT NULL,
  seq      INTEGER NOT NULL,
  record   TEXT    NOT NULL,
  PRIMARY KEY (division, seq)
);
CREATE TABLE IF NOT EXISTS ground_items (
  division TEXT    NOT NULL,
  gid      INTEGER NOT NULL,
  record   TEXT    NOT NULL,
  PRIMARY KEY (division, gid)
);
CREATE TABLE IF NOT EXISTS next_char_id (
  division TEXT PRIMARY KEY,
  next_id  INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS next_guild_id (
  division TEXT PRIMARY KEY,
  next_id  INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS memos (
  division TEXT    NOT NULL,
  char_id  INTEGER NOT NULL,
  seq      INTEGER NOT NULL,
  record   TEXT    NOT NULL,
  PRIMARY KEY (division, char_id, seq)
);
CREATE TABLE IF NOT EXISTS guilds (
  division TEXT    NOT NULL,
  guild_id INTEGER NOT NULL,
  record   TEXT    NOT NULL,
  PRIMARY KEY (division, guild_id)
);
CREATE TABLE IF NOT EXISTS guild_members (
  division TEXT    NOT NULL,
  guild_id INTEGER NOT NULL,
  char_id  INTEGER NOT NULL,
  record   TEXT    NOT NULL,
  seq      INTEGER NOT NULL,
  PRIMARY KEY (division, guild_id, char_id)
);
CREATE TABLE IF NOT EXISTS training_camps (
  division TEXT    NOT NULL,
  camp_id  INTEGER NOT NULL,
  record   TEXT    NOT NULL,
  PRIMARY KEY (division, camp_id)
);
CREATE TABLE IF NOT EXISTS training_camp_members (
  division TEXT    NOT NULL,
  camp_id  INTEGER NOT NULL,
  char_id  INTEGER NOT NULL,
  record   TEXT    NOT NULL,
  seq      INTEGER NOT NULL,
  PRIMARY KEY (division, camp_id, char_id)
);
`

// metaKey* are the meta table's fixed keys.
const (
	metaKeySchemaVersion = "schemaVersion"
	metaKeyLayoutVersion = "layoutVersion"
	metaKeyGidCounter    = "gidCounter"
	metaKeyUpdatedAtMs   = "updatedAtMs"
)

// CurrentLayoutVersion identifies the only physical table layout accepted by
// this binary. It is independent from CurrentVersion, which identifies the
// JSON character-record schema. Adding a table bumps this value and requires an
// reviewed offline upgrade before deployment. sro-authority-upgrade preserves
// layout-4 records while adding the empty mall currency and warehouse tables
// for layout 5, and the empty fortress and union tables for layout 6
// (authority_upgrade.go); the release receiver runs it.
const CurrentLayoutVersion = 6

/*
==================
connectDB

connectDB opens a single-connection handle without changing durable
database settings. Startup uses it to validate an existing database before
adopting or configuring it.
==================
*/
func connectDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("PRAGMA busy_timeout=5000: %w", err)
	}
	return db, nil
}

// configureDB applies the runtime durability settings after a database has
// proven current and internally coherent.
/*
================
configureDB
================
*/
func configureDB(db *sql.DB) error {
	pragmas := []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=" + sqliteSynchronous()}
	if !flushToMedia() {
		// Without media flushes the journal switch must not run under the
		// connection's default FULL level, which syncs the rollback journal.
		pragmas[0], pragmas[1] = pragmas[1], pragmas[0]
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return nil
}

// openDB creates a runtime-configured handle for a new database or an
// already-validated one.
/*
================
openDB
================
*/
func openDB(path string) (*sql.DB, error) {
	db, err := connectDB(path)
	if err != nil {
		return nil, err
	}
	if err := configureDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// ensureSchema applies the idempotent schema.
/*
================
ensureSchema
================
*/
func ensureSchema(db *sql.DB) error {
	_, err := db.Exec(dbSchema)
	return err
}

// quickCheck runs SQLite's integrity probe; any answer but "ok" is
// corruption.
/*
================
quickCheck
================
*/
func quickCheck(db *sql.DB) error {
	var verdict string
	if err := db.QueryRow("PRAGMA quick_check(1)").Scan(&verdict); err != nil {
		return err
	}
	if verdict != "ok" {
		return fmt.Errorf("quick_check: %s", verdict)
	}
	return nil
}

/*
==================
dbSidecars

dbSidecars are the WAL-mode companion files that must move with the
database on quarantine: leaving a stale -wal beside a restored main
file would replay old frames into the wrong generation.
==================
*/
func dbSidecars(path string) []string {
	return []string{path + "-wal", path + "-shm"}
}

// quarantineDB renames the database AND its sidecars aside.
/*
================
quarantineDB
================
*/
func quarantineDB(path string, now time.Time) (string, error) {
	dest, err := quarantine(path, now)
	if err != nil {
		return "", err
	}
	for _, sidecar := range dbSidecars(path) {
		if _, statErr := os.Stat(sidecar); statErr == nil {
			if err := os.Rename(sidecar, dest+filepath.Ext(sidecar)); err != nil {
				return "", fmt.Errorf("quarantining database sidecar %s: %w", sidecar, err)
			}
		}
	}
	return dest, nil
}

// removeDBSidecars deletes stale sidecars (used before restoring a bak
// copy into place: the bak is a compact checkpointed image with no WAL).
/*
================
removeDBSidecars
================
*/
func removeDBSidecars(path string) {
	for _, sidecar := range dbSidecars(path) {
		os.Remove(sidecar)
	}
}

/*
==================
refreshDBBak

refreshDBBak writes the one-generation previous copy with VACUUM INTO
(a transactionally consistent, checkpointed, compact image). Once per
boot, only after the MAIN file proved good - never while running.
==================
*/
func refreshDBBak(db *sql.DB, bakPath string) error {
	if err := os.Remove(bakPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err := db.Exec("VACUUM INTO ?", bakPath)
	return err
}

// loadedDB is the in-memory result of a validated current-schema read.
/*
================
loadedDB

Validated database records adopted together during startup.
================
*/
type loadedDB struct {
	characters   map[string][]*domain.Character
	deleted      map[string][]json.RawMessage
	ground       map[string][]domain.GroundItemRecord
	mailboxes    map[string]map[int64][]domain.LetterRecord
	guilds       map[string]map[int64]domain.GuildRecord
	guildMembers map[string]map[int64][]domain.GuildMemberRecord
	camps        map[string]map[int64]domain.TrainingCampRecord
	campMembers  map[string]map[int64][]domain.TrainingCampMemberRecord
	meta         Meta
	version      int
}

// loadDB accepts only the exact current character and layout versions. It
// validates without rewriting the database.
