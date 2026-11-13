/*
===========================================================================

layout_steps_test.go - an additive layout step is created at boot, and
only that

Part 5 of #3082 (#3094). StoreReadHeadroom is 0 until part 4, so the
boot tests open with the package-only wider window (Options.readHeadroom)
on a store as the layout-before binary left it: the current file with
the current layout's tables dropped and its layout stamped one lower.
The class tests run every additive step's statement against an empty
and a full database, so a step that touches anything but its own new
tables cannot be marked additive.

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

/*
================
olderLayoutStore

A store as the binary one layout back leaves it: a character, then the
tables of every layout above layout dropped and the layout stamped
down. Returns the character's name.
================
*/
func olderLayoutStore(t *testing.T, dir string, clock *testClock, layout int) string {
	t.Helper()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
		t.Fatal(err)
	}
	s.Close()
	for _, step := range layoutSteps {
		if step.layout <= layout {
			continue
		}
		for _, table := range step.tables {
			execAuthority(t, dir, "DROP TABLE "+table)
		}
	}
	rewriteDatabaseMeta(t, dir, metaKeyLayoutVersion, layout)
	return c.Name
}

/*
================
databaseContents

Every schema object's statement and every table's rows, by name, from
an open handle: what "no existing table or row is touched" compares.
================
*/
func databaseContents(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	contents := map[string]string{}
	objects, err := db.Query("SELECT type, name, coalesce(sql, '') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for objects.Next() {
		var kind, name, statement string
		if err := objects.Scan(&kind, &name, &statement); err != nil {
			t.Fatal(err)
		}
		contents[kind+" "+name] = statement
		if kind == "table" {
			tables = append(tables, name)
		}
	}
	if err := objects.Err(); err != nil {
		t.Fatal(err)
	}
	if err := objects.Close(); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		contents["rows "+table] = tableRows(t, db, table)
	}
	return contents
}

/*
================
tableRows

One table's rows, each printed and the list sorted, so the comparison
does not depend on the scan order.
================
*/
func tableRows(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query("SELECT * FROM " + table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var printed []string
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		printed = append(printed, fmt.Sprintf("%v", values))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(printed)
	return strings.Join(printed, "\n")
}

/*
================
closedContents

databaseContents of the closed store's file, the handle closed again
before the caller opens the store.
================
*/
func closedContents(t *testing.T, dir string) map[string]string {
	t.Helper()
	db, err := connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	return databaseContents(t, db)
}

/*
================
connectTest

A validation handle on the closed store's file, closed with the test.
================
*/
func connectTest(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

//============================================================================

/*
================
TestBootCreatesAnAdditiveLayoutsEmptyTables

A store at layout L boots on the layout L+1 binary inside its window:
the new table exists empty, the layout stamp is L+1, and every object
and row the store held is unchanged. The binary of layout L, with its
own window, still opens the result.
================
*/
func TestBootCreatesAnAdditiveLayoutsEmptyTables(t *testing.T) {
	t.Parallel()
	older := CurrentLayoutVersion - 1
	current := layoutSteps[len(layoutSteps)-1]

	// The boot, end to end.
	dir, clock := t.TempDir(), newTestClock()
	name := olderLayoutStore(t, dir, clock, older)
	s, err := openWithHeadroom(t, dir, clock, 1)
	if err != nil {
		t.Fatalf("the layout %d binary refused a layout %d store: %v", CurrentLayoutVersion, older, err)
	}
	storedCharacter(t, s, name)
	s.Close()
	if got := metaStamp(t, dir, metaKeyLayoutVersion); got != strconv.Itoa(CurrentLayoutVersion) {
		t.Fatalf("layout stamp = %s, want %d", got, CurrentLayoutVersion)
	}
	for _, table := range current.tables {
		if got := rowRecord(t, dir, "SELECT count(*) FROM "+table); got != "0" {
			t.Fatalf("%s holds %s rows after the boot created it", table, got)
		}
	}

	// The step alone, on a second such store: nothing but the new
	// tables and the stamp moves.
	dir = t.TempDir()
	olderLayoutStore(t, dir, clock, older)
	db := connectTest(t, dir)
	before := databaseContents(t, db)
	if err := createBootLayoutTables(db, layoutSteps, 1); err != nil {
		t.Fatal(err)
	}
	after := databaseContents(t, db)
	before["rows meta"], after["rows meta"] = "", ""
	added := map[string]bool{}
	for key := range after {
		if _, held := before[key]; !held {
			added[key] = true
			continue
		}
		if after[key] != before[key] {
			t.Errorf("%s changed:\n%s\nwas:\n%s", key, after[key], before[key])
		}
	}
	if len(after)-len(added) != len(before) {
		t.Fatalf("the boot step removed objects: %d before, %d after with %d added", len(before), len(after), len(added))
	}
	for _, table := range current.tables {
		if !added["table "+table] || after["rows "+table] != "" {
			t.Fatalf("%s: added %v, rows %q", table, slices.Sorted(maps.Keys(added)), after["rows "+table])
		}
	}
	if got, err := readMetaInt(db, metaKeyLayoutVersion, 0); err != nil || got != CurrentLayoutVersion {
		t.Fatalf("layout stamp = %d (%v)", got, err)
	}

	// The layout L binary's view of the result: inside its window.
	if _, err := loadDB(db, CurrentVersion, older, 1); err != nil {
		t.Fatalf("the layout %d binary cannot open what the boot left: %v", older, err)
	}
}

/*
================
TestBootLayoutOutsideTheWindowRefuses

Today's window (headroom 0) creates nothing and refuses an older layout
as before; a one-wide window refuses a store two layouts back. Both
leave the file as it was.
================
*/
func TestBootLayoutOutsideTheWindowRefuses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		layout   int
		headroom int
	}{
		{"today's window", CurrentLayoutVersion - 1, 0},
		{"two layouts back, one-wide window", CurrentLayoutVersion - 2, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir, clock := t.TempDir(), newTestClock()
			olderLayoutStore(t, dir, clock, test.layout)
			before := closedContents(t, dir)
			if _, err := openWithHeadroom(t, dir, clock, test.headroom); !isVersionMismatch(err) {
				t.Fatalf("open = %v, want an incompatible-schema refusal", err)
			}
			if after := closedContents(t, dir); !maps.Equal(before, after) {
				t.Fatal("a refused store was changed")
			}
		})
	}
}

/*
================
TestANonAdditiveLayoutStepRefusesAtBoot

A layout step that alters an existing table is not additive: inside the
window the boot refuses it, naming the step, writes nothing, and the
store stays the offline upgrade's.
================
*/
func TestANonAdditiveLayoutStepRefusesAtBoot(t *testing.T) {
	t.Parallel()

	steps := []layoutStep{
		{11, true, "a test", []string{"eleven"}, "CREATE TABLE IF NOT EXISTS eleven (id INTEGER);"},
		{12, true, "a test", []string{"twelve"}, "CREATE TABLE IF NOT EXISTS twelve (id INTEGER);"},
		{13, false, "adds a column to characters", nil, "ALTER TABLE characters ADD COLUMN probe INTEGER;"},
	}
	for _, test := range []struct {
		name                      string
		stored, current, headroom int
		inside                    bool
		refused                   bool
	}{
		{"additive step inside the window", 11, 12, 1, true, false},
		{"non-additive step inside the window", 12, 13, 1, false, true},
		{"non-additive step behind an additive one", 11, 13, 2, false, true},
		{"below the window", 11, 13, 1, false, false},
		{"store at the current layout", 13, 13, 1, false, false},
	} {
		inside, err := bootLayoutWindow(steps, test.stored, test.current, test.headroom)
		if inside != test.inside || isVersionMismatch(err) != test.refused {
			t.Fatalf("%s: inside %v, err %v", test.name, inside, err)
		}
		if test.refused && !strings.Contains(err.Error(), "layout 13 is not additive") {
			t.Fatalf("%s: the refusal does not name the step: %v", test.name, err)
		}
	}

	// The boot's own step on a real store, its current layout's step
	// reclassified.
	altered := slices.Clone(layoutSteps)
	last := &altered[len(altered)-1]
	last.additive, last.why = false, "a test: alters an existing table"
	dir, clock := t.TempDir(), newTestClock()
	olderLayoutStore(t, dir, clock, CurrentLayoutVersion-1)
	db := connectTest(t, dir)
	before := databaseContents(t, db)
	if err := createBootLayoutTables(db, altered, 1); !isVersionMismatch(err) {
		t.Fatalf("the boot step = %v, want an incompatible-schema refusal", err)
	}
	if after := databaseContents(t, db); !maps.Equal(before, after) {
		t.Fatal("a refused layout step changed the store")
	}
}

/*
================
TestBootRefusesANewTableThatHoldsRows

A table of a newer layout that already holds rows in an older-layout
store is a mislabeled store, not construction residue: the boot refuses
without quarantining it, and the rows and the stamp stay.
================
*/
func TestBootRefusesANewTableThatHoldsRows(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	s.Close()
	execAuthority(t, dir, "INSERT INTO silk_history (account_id, at_ms, kind, silk, gift_silk, points) VALUES ('account-a', 1, 1, 10, 0, 0)")
	rewriteDatabaseMeta(t, dir, metaKeyLayoutVersion, CurrentLayoutVersion-1)
	if _, err := openWithHeadroom(t, dir, clock, 1); !isVersionMismatch(err) || !strings.Contains(err.Error(), "silk_history") {
		t.Fatalf("open = %v, want an incompatible-schema refusal naming silk_history", err)
	}
	if got := rowRecord(t, dir, "SELECT count(*) FROM silk_history"); got != "1" {
		t.Fatalf("silk_history holds %s rows", got)
	}
	if got := metaStamp(t, dir, metaKeyLayoutVersion); got != strconv.Itoa(CurrentLayoutVersion-1) {
		t.Fatalf("layout stamp = %s", got)
	}
}

/*
================
TestAdditiveLayoutStepsOnlyCreateTheirOwnTables

The class, machine-checked: an additive step's statement, run on an
empty database, creates exactly its tables (and their indexes), empty,
as a fresh authority's dbSchema writes them; run on a full current
database, it changes nothing. A step that alters, fills or drops any
other table fails one of the two.
================
*/
func TestAdditiveLayoutStepsOnlyCreateTheirOwnTables(t *testing.T) {
	t.Parallel()
	full := openScratch(t)
	if err := ensureSchema(full); err != nil {
		t.Fatal(err)
	}
	fresh := databaseContents(t, full)

	checked := 0
	for _, step := range layoutSteps {
		if !step.additive {
			continue
		}
		checked++
		empty := openScratch(t)
		if _, err := empty.Exec(step.schema); err != nil {
			t.Fatalf("layout %d on an empty database: %v", step.layout, err)
		}
		var objects []string
		owners, err := empty.Query("SELECT type, name, tbl_name FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'")
		if err != nil {
			t.Fatal(err)
		}
		for owners.Next() {
			var kind, name, table string
			if err := owners.Scan(&kind, &name, &table); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(step.tables, table) {
				t.Errorf("layout %d creates %s %s on %s, not one of its tables %v", step.layout, kind, name, table, step.tables)
			}
			objects = append(objects, kind+" "+name)
		}
		if err := owners.Err(); err != nil {
			t.Fatal(err)
		}
		if err := owners.Close(); err != nil {
			t.Fatal(err)
		}
		created := databaseContents(t, empty)
		for _, object := range objects {
			if created[object] != fresh[object] {
				t.Errorf("layout %d writes %s as\n%s\nbut a fresh authority as\n%s", step.layout, object, created[object], fresh[object])
			}
		}
		for _, table := range step.tables {
			if _, ok := created["table "+table]; !ok {
				t.Errorf("layout %d does not create %s", step.layout, table)
			}
			if rows := created["rows "+table]; rows != "" {
				t.Errorf("layout %d fills %s: %s", step.layout, table, rows)
			}
		}
		if _, err := full.Exec(step.schema); err != nil {
			t.Fatalf("layout %d on a current database: %v", step.layout, err)
		}
		if again := databaseContents(t, full); !maps.Equal(again, fresh) {
			t.Errorf("layout %d changes a current database", step.layout)
		}
	}
	if checked == 0 {
		t.Fatal("no additive layout step was checked")
	}
}

/*
================
openScratch

An empty database file of the test's own.
================
*/
func openScratch(t *testing.T) *sql.DB {
	t.Helper()
	db, err := connectDB(filepath.Join(t.TempDir(), DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
