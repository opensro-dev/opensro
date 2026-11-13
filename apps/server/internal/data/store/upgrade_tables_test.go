/*
===========================================================================

upgrade_tables_test.go - the offline upgrade's two tables and the
quarantine's sidecar names

storeSteps' layout windows and layout_steps.go's steps replace
a switch and two hand-written comparison chains, so these tests pin what
the chains guaranteed: each window is sane, and every layout from the
first addition to the current one adds its tables exactly once and in
order. The quarantine tests pin SQLite's sidecar pairing and
generation-based retention.

===========================================================================
*/
package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

/*
================
TestEveryStoreStepHasALayoutWindow

Every listed version's window lies inside the layouts the upgrade
knows (store_steps_test.go holds that every version is listed), and the
low ends never fall back: no schema accepts a layout below an earlier
schema's lowest. The current schema is the exception: its wide window
lets the role repair run on a current store at any older layout.
================
*/
func TestEveryStoreStepHasALayoutWindow(t *testing.T) {
	floor := 0
	for version := UpgradeFromVersion; version < CurrentVersion; version++ {
		step, ok := storeSteps[version]
		if !ok {
			continue
		}
		if step.minLayout < preMallLayoutVersion || step.minLayout > step.maxLayout || step.maxLayout > CurrentLayoutVersion {
			t.Fatalf("schema %d window %d..%d", version, step.minLayout, step.maxLayout)
		}
		if step.minLayout < floor {
			t.Fatalf("schema %d accepts layout %d, below an earlier schema's floor %d", version, step.minLayout, floor)
		}
		floor = step.minLayout
	}
	if step := storeSteps[CurrentVersion]; step.maxLayout != CurrentLayoutVersion {
		t.Fatalf("the current schema's window ends at %d, not the current layout %d", step.maxLayout, CurrentLayoutVersion)
	}
}

/*
================
TestUpgradeSourceLayoutRefusesOutsideTheWindow

The windows the code's own history records: schemas 33-37 ran at layout
11 and 39-42 at layout 12, so a lower stamp is a mislabeled store.
================
*/
func TestUpgradeSourceLayoutRefusesOutsideTheWindow(t *testing.T) {
	cases := []struct {
		schema, layout int
		ok             bool
	}{
		{UpgradeFromVersion, preMallLayoutVersion, true},
		{UpgradeFromVersion, preFortressLayoutVersion, false},
		{preCompanionVersion, preGuildWarLayoutVersion, false},
		{preTradeRewardVersion, preItemForgeLayoutVersion, true},
		{preTradeRewardVersion, preUniqueKillLayoutVersion, false},
		{preHalloweenVersion, preNameReservationLayoutVersion, false},
		{preHalloweenVersion, preSilkHistoryLayoutVersion, true},
		{preWantedDecayVersion, preNameReservationLayoutVersion, false},
		{preSookArenaAccountVersion, preSilkHistoryLayoutVersion, false},
		{preSookArenaAccountVersion, CurrentLayoutVersion, true},
		{CurrentVersion, preGuildWarLayoutVersion, true},
		{UpgradeFromVersion - 1, preMallLayoutVersion, false},
		{CurrentVersion + 1, CurrentLayoutVersion, false},
	}
	for _, c := range cases {
		got, err := upgradeSourceLayout(c.schema, c.layout)
		if c.ok != (err == nil) {
			t.Fatalf("schema %d layout %d: err %v, want ok=%v", c.schema, c.layout, err, c.ok)
		}
		if c.ok && got != c.layout {
			t.Fatalf("schema %d layout %d validated at %d", c.schema, c.layout, got)
		}
	}
}

/*
================
TestLayoutStepsRunInOrderToTheCurrentLayout
================
*/
func TestLayoutStepsRunInOrderToTheCurrentLayout(t *testing.T) {
	tables := map[string]bool{}
	for i, step := range layoutSteps {
		if want := preFortressLayoutVersion + i; step.layout != want {
			t.Fatalf("step %d is layout %d, want %d", i, step.layout, want)
		}
		if step.schema == "" || strings.TrimSpace(step.why) == "" {
			t.Fatalf("layout %d has no statement or no reason", step.layout)
		}
		if step.additive && len(step.tables) == 0 {
			t.Fatalf("layout %d is additive but adds no table", step.layout)
		}
		for _, table := range step.tables {
			if tables[table] {
				t.Fatalf("table %s added twice", table)
			}
			tables[table] = true
			if !strings.Contains(step.schema, "CREATE TABLE IF NOT EXISTS "+table+" ") {
				t.Fatalf("layout %d lists %s but its statement does not create it", step.layout, table)
			}
		}
	}
	if last := layoutSteps[len(layoutSteps)-1].layout; last != CurrentLayoutVersion {
		t.Fatalf("steps end at layout %d, current is %d", last, CurrentLayoutVersion)
	}
}

/*
================
TestQuarantineKeepsSQLiteSidecarNames

A quarantined database's WAL must sit at <dest>-wal, the only name
SQLite pairs with <dest>.
================
*/
func TestQuarantineKeepsSQLiteSidecarNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DBFileName)
	for _, name := range []string{path, path + dbSidecarWAL, path + dbSidecarSHM} {
		if err := os.WriteFile(name, []byte(filepath.Base(name)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dest, err := quarantineDB(path, time.UnixMilli(1_700_000_000_000))
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", dbSidecarWAL, dbSidecarSHM} {
		if got, err := os.ReadFile(dest + suffix); err != nil || string(got) != DBFileName+suffix {
			t.Fatalf("%s holds %q (err %v)", dest+suffix, got, err)
		}
	}
}

/*
================
TestPruneQuarantinesCountsGenerations

Sidecars belong to their database's generation: five quarantines with
sidecars (and one older release's ".db-wal" name) keep the newest three
generations whole.
================
*/
func TestPruneQuarantinesCountsGenerations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DBFileName)
	for i := 0; i < 5; i++ {
		for _, name := range []string{path, path + dbSidecarWAL, path + dbSidecarSHM} {
			if err := os.WriteFile(name, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := quarantineDB(path, time.UnixMilli(1_700_000_000_000+int64(i))); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			legacy := path + corruptSuffix + "1700000000000.db-wal"
			if err := os.WriteFile(legacy, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	pruneQuarantines(path)
	matches, err := filepath.Glob(path + corruptSuffix + "*")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(matches)
	var got []string
	for _, match := range matches {
		got = append(got, strings.TrimPrefix(filepath.Base(match), DBFileName+corruptSuffix))
	}
	want := []string{
		"1700000000002", "1700000000002-shm", "1700000000002-wal",
		"1700000000003", "1700000000003-shm", "1700000000003-wal",
		"1700000000004", "1700000000004-shm", "1700000000004-wal",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("kept %v, want %v", got, want)
	}
}

/*
================
TestRetiredFieldSkipKeepsTheRecord

A record without a retired key comes back as the same bytes; one with an
empty retired key loses it; a populated one is refused.
================
*/
func TestRetiredFieldSkipKeepsTheRecord(t *testing.T) {
	plain := []byte(`{"name":"a","accountId":"x"}`)
	if got, err := stripRetiredCharacterFields(plain); err != nil || string(got) != string(plain) {
		t.Fatalf("plain record %q (err %v)", got, err)
	}
	empty := []byte(`{"name":"a","dressSetKeys":[]}`)
	if got, err := stripRetiredCharacterFields(empty); err != nil || strings.Contains(string(got), "dressSetKeys") {
		t.Fatalf("empty retired key kept: %q (err %v)", got, err)
	}
	if _, err := stripRetiredCharacterFields([]byte(`{"weaponSetKeys":["k"]}`)); err == nil {
		t.Fatal("a populated retired key must fail the load")
	}
	inValue := []byte(`{"name":"\"dressSetKeys\""}`)
	if got, err := stripRetiredCharacterFields(inValue); err != nil || string(got) != string(inValue) {
		t.Fatalf("a value naming the key %q (err %v)", got, err)
	}
}
