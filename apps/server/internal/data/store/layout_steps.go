/*
===========================================================================

layout_steps.go - the table layouts, their classes, and the boot that
creates a newer layout's empty tables

Every table layout past preMallLayoutVersion has one step here: the
tables it added, the statement that creates them, and its class (#3094,
part 5 of #3082). Two writers apply the steps: the offline upgrade
(authority_upgrade.go) runs every step a store lacks, and the boot runs
the additive ones inside the read window, so a layout that only adds
empty tables ships without a maintenance release. The boot never alters
an existing table or row: a step that would is not additive and stays
the offline upgrade's.

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
)

/*
================
layoutStep

One table layout's step. A step is additive when its statement only
creates tables (and their indexes) that no earlier layout has, all of
them empty: no existing table gains a column, an index, a constraint or
a row. Only an additive step runs at boot (layout_steps_test.go checks
each statement against a fresh database); any other step is created by
the offline upgrade alone. When in doubt a step is not additive: that
costs a maintenance release, never saved data.
================
*/
type layoutStep struct {
	layout   int
	additive bool
	// why is the reason for the class, in one line.
	why    string
	tables []string
	schema string
}

// layoutSteps holds every layout from preFortressLayoutVersion to
// CurrentLayoutVersion in ascending order; ascending is creation order.
// A new layout adds its step here, classified.
var layoutSteps = []layoutStep{
	{5, true, "empty mall currency and warehouse tables", []string{"mall_accounts", "account_storage"}, mallAccountsSchema + accountStorageSchema},
	{6, true, "empty fortress and union tables", []string{"fortresses", "fortress_requests", "fortress_structures", "alliances"}, fortressSchema + allianceSchema},
	{7, true, "empty guild-war table", []string{"guild_wars"}, guildWarSchema},
	{8, true, "empty fortress production table", []string{"fortress_item_forges"}, fortressItemForgeSchema},
	{9, true, "empty unique-kill table", []string{"unique_kills"}, uniqueKillsSchema},
	{10, true, "empty economy ledger", []string{"economy_ledger"}, economyLedgerSchema},
	{11, true, "empty name-reservation table", []string{"name_reservations"}, nameReservationsSchema},
	{12, true, "empty silk-history table", []string{"silk_history"}, silkHistorySchema},
}

/*
================
pendingLayoutSteps

The steps a store at layout from lacks, after the residue guard: a table
of such a step may already exist only empty. An empty one is
construction residue (a database a newer binary created, then stamped
down), and its CREATE ... IF NOT EXISTS is a no-op. Rows are the
mislabeling the guard exists to catch: an older layout cannot hold data
its binary never wrote.
================
*/
func pendingLayoutSteps(db *sql.DB, steps []layoutStep, from int) ([]layoutStep, error) {
	var pending []layoutStep
	for _, step := range steps {
		if step.layout <= from {
			continue
		}
		pending = append(pending, step)
		for _, table := range step.tables {
			var existing int
			if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?", table).Scan(&existing); err != nil {
				return nil, err
			}
			if existing == 0 {
				continue
			}
			var rows int
			if err := db.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&rows); err != nil {
				return nil, err
			}
			if rows != 0 {
				return nil, fmt.Errorf("layout %d unexpectedly contains %s", from, table)
			}
		}
	}
	return pending, nil
}

/*
================
bootLayoutWindow

Whether the boot creates a newer layout's tables on a store at layout
stored: only inside the read window (stored at least current-headroom),
and only when every step from stored to current is additive. A store
already at or past current, or below the window, is loadDB's to accept
or refuse; a non-additive step inside the window refuses here, naming
it. headroom 0 creates nothing, which keeps today's exact boot.
================
*/
func bootLayoutWindow(steps []layoutStep, stored, current, headroom int) (bool, error) {
	if stored >= current || stored < current-headroom {
		return false, nil
	}
	for _, step := range steps {
		if step.layout <= stored || step.layout > current {
			continue
		}
		if !step.additive {
			return false, fmt.Errorf("database layout version %d, binary layout %d: layout %d is not additive (%s): %w; preserve the authority; it is upgraded offline with sro-authority-upgrade", stored, current, step.layout, step.why, errIncompatibleSchema)
		}
	}
	return true, nil
}

/*
================
createBootLayoutTables

The boot's layout step, before loadDB on the same handle. A store whose
schema lies inside the read window and whose layout is older than this
binary's, by additive steps within the window, gains the missing empty
tables and its layout stamp in one transaction. The stamp only goes up
(stampVersionTx), and an older binary inside its own window reads the
store as before, ignoring the tables (#3091). Everything else is left
untouched for loadDB to judge.
================
*/
func createBootLayoutTables(db *sql.DB, steps []layoutStep, headroom int) error {
	version, err := readMetaInt(db, metaKeySchemaVersion, 0)
	if err != nil {
		return err
	}
	if version < CurrentVersion || version > CurrentVersion+headroom {
		return nil
	}
	layout, err := readMetaInt(db, metaKeyLayoutVersion, 0)
	if err != nil {
		return err
	}
	inside, err := bootLayoutWindow(steps, layout, CurrentLayoutVersion, headroom)
	if err != nil || !inside {
		return err
	}
	pending, err := pendingLayoutSteps(db, steps, layout)
	if err != nil {
		return fmt.Errorf("boot layout step: %w: %w", err, errIncompatibleSchema)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var created []string
	for _, step := range pending {
		if step.layout > CurrentLayoutVersion {
			break
		}
		if _, err := tx.Exec(step.schema); err != nil {
			return fmt.Errorf("boot layout step %d: %w", step.layout, err)
		}
		created = append(created, step.tables...)
	}
	if err := stampVersionTx(tx, metaKeyLayoutVersion, CurrentLayoutVersion); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Infof("store: layout %d -> %d at boot: created the empty tables %s", layout, CurrentLayoutVersion, strings.Join(created, ", "))
	return nil
}
