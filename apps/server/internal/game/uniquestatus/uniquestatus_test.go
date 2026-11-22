/*
===========================================================================

uniquestatus_test.go - the Unique Status feed's states and pacing

===========================================================================
*/
package uniquestatus

import (
	"encoding/binary"
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
feedWorld

The feed's two owners as a test drives them: the monster state's rows
and the kill record behind one sequence.
================
*/
type feedWorld struct {
	states   []simulation.UniqueState
	kills    []domain.UniqueKill
	killPage int
	killErr  error
}

func (w *feedWorld) sources() Sources {
	return Sources{
		States: func() []simulation.UniqueState { return w.states },
		KillsAfter: func(afterSeq int64) ([]domain.UniqueKill, error) {
			if w.killErr != nil {
				return nil, w.killErr
			}
			var out []domain.UniqueKill
			for _, kill := range w.kills {
				if kill.Seq > afterSeq {
					out = append(out, kill)
				}
			}
			if w.killPage > 0 && len(out) > w.killPage {
				out = out[:w.killPage]
			}
			return out, nil
		},
		KillPage:   w.killPage,
		UniqueName: func(refObjID uint32) string { return "" },
	}
}

// readRow decodes one wire row at body[at:], returning the row and the
// offset just past it.
func readRow(t *testing.T, body []byte, at int) (wire.UniqueStatusRow, int) {
	t.Helper()
	var row wire.UniqueStatusRow
	row.RefObjID = binary.LittleEndian.Uint32(body[at:])
	at += 4
	nameLen := int(body[at])
	at++
	row.Name = string(body[at : at+nameLen])
	at += nameLen
	row.Level = body[at]
	at++
	row.RegionID = binary.LittleEndian.Uint16(body[at:])
	at += 2
	row.Alive = body[at] == 1
	at++
	row.DiedAtMs = int64(binary.LittleEndian.Uint64(body[at:]))
	at += 8
	killerLen := int(body[at])
	at++
	row.Killer = string(body[at : at+killerLen])
	return row, at + killerLen
}

func readSnapshot(t *testing.T, payload []byte) []wire.UniqueStatusRow {
	t.Helper()
	count := int(binary.LittleEndian.Uint16(payload[8:]))
	rows := make([]wire.UniqueStatusRow, 0, count)
	at := 10
	for i := 0; i < count; i++ {
		row, next := readRow(t, payload, at)
		rows = append(rows, row)
		at = next
	}
	if at != len(payload) {
		t.Fatalf("snapshot has %d trailing bytes", len(payload)-at)
	}
	return rows
}

func readUpdate(t *testing.T, payload []byte) wire.UniqueStatusRow {
	t.Helper()
	row, at := readRow(t, payload, 8)
	if at != len(payload) {
		t.Fatalf("update has %d trailing bytes", len(payload)-at)
	}
	return row
}

/*
================
TestEnabled
================
*/
func TestEnabled(t *testing.T) {
	get := func(value string) func(string) string {
		return func(string) string { return value }
	}
	for _, value := range []string{"on", "1", "true", "TRUE", " on "} {
		if !Enabled(get(value)) {
			t.Fatalf("Enabled(%q) = false", value)
		}
	}
	for _, value := range []string{"off", "", "yes", "0"} {
		if Enabled(get(value)) {
			t.Fatalf("Enabled(%q) = true", value)
		}
	}
}

/*
================
TestTickSnapshotsAndUpdates

The feed's life: a first snapshot with no updates, a kill's update with
its killer and time, a late session's snapshot, a respawn's update, and
a returning session's fresh snapshot, all through the poll pacing.
================
*/
func TestTickSnapshotsAndUpdates(t *testing.T) {
	world := &feedWorld{states: []simulation.UniqueState{
		{RefObjID: 1954, Codename: "MOB_CH_TIGERGIRL", Level: 20, RegionID: 25001, Alive: false},
		{RefObjID: 2000, Codename: "MOB_RM_ROC", Level: 90, RegionID: 25002, Alive: true},
	}}
	feed := New(world.sources())

	snapshots, updates := feed.Tick(0, []string{"s1"})
	if len(updates) != 0 {
		t.Fatalf("first pass sent %d updates", len(updates))
	}
	rows := readSnapshot(t, snapshots["s1"])
	if len(rows) != 2 || rows[0].RefObjID != 1954 || rows[1].RefObjID != 2000 {
		t.Fatalf("snapshot rows = %+v", rows)
	}
	if rows[0].Name != "MOB_CH_TIGERGIRL" || rows[0].Alive || rows[0].DiedAtMs != 0 {
		t.Fatalf("never-killed row = %+v", rows[0])
	}

	// Inside the poll interval nothing moves.
	snapshots, updates = feed.Tick(500, []string{"s1"})
	if snapshots != nil || updates != nil {
		t.Fatal("a paced pass returned frames")
	}

	// Roc dies to Lune.
	world.kills = []domain.UniqueKill{{Seq: 1, AtMs: 900, RefObjID: 2000, KillerName: "Lune"}}
	world.states[1].Alive = false
	snapshots, updates = feed.Tick(1000, []string{"s1"})
	if len(snapshots) != 0 {
		t.Fatalf("a known session got %d snapshots", len(snapshots))
	}
	if len(updates) != 1 {
		t.Fatalf("the kill sent %d updates", len(updates))
	}
	if row := readUpdate(t, updates[0]); row.RefObjID != 2000 || row.Alive || row.DiedAtMs != 900 || row.Killer != "Lune" {
		t.Fatalf("kill update = %+v", row)
	}

	// A new session's snapshot carries the kill; no update repeats.
	snapshots, updates = feed.Tick(2000, []string{"s1", "s2"})
	if len(updates) != 0 {
		t.Fatalf("an unchanged pass sent %d updates", len(updates))
	}
	rows = readSnapshot(t, snapshots["s2"])
	if len(rows) != 2 || rows[1].DiedAtMs != 900 || rows[1].Killer != "Lune" {
		t.Fatalf("late snapshot rows = %+v", rows)
	}
	if _, ok := snapshots["s1"]; ok {
		t.Fatal("s1 was re-snapshotted")
	}

	// Roc returns: the update flips it alive and keeps the last kill.
	world.states[1].Alive = true
	_, updates = feed.Tick(3000, []string{"s1", "s2"})
	if len(updates) != 1 {
		t.Fatalf("the respawn sent %d updates", len(updates))
	}
	if row := readUpdate(t, updates[0]); row.RefObjID != 2000 || !row.Alive || row.DiedAtMs != 900 || row.Killer != "Lune" {
		t.Fatalf("respawn update = %+v", row)
	}

	// s1 leaves and returns: owed a fresh snapshot.
	feed.Tick(4000, []string{"s2"})
	snapshots, _ = feed.Tick(5000, []string{"s1", "s2"})
	if _, ok := snapshots["s1"]; !ok {
		t.Fatal("the returning session got no snapshot")
	}
}

/*
================
TestTickKillPaging

The last-kill map is built page by page, however small the page.
================
*/
func TestTickKillPaging(t *testing.T) {
	world := &feedWorld{
		states:   []simulation.UniqueState{{RefObjID: 1954, Codename: "MOB_CH_TIGERGIRL", Level: 20, Alive: false}},
		killPage: 1,
		kills: []domain.UniqueKill{
			{Seq: 1, AtMs: 100, RefObjID: 1954, KillerName: "First"},
			{Seq: 2, AtMs: 200, RefObjID: 1954, KillerName: "Second"},
			{Seq: 3, AtMs: 300, RefObjID: 1954, KillerName: "Third"},
		},
	}
	feed := New(world.sources())
	snapshots, _ := feed.Tick(0, []string{"s1"})
	rows := readSnapshot(t, snapshots["s1"])
	if len(rows) != 1 || rows[0].DiedAtMs != 300 || rows[0].Killer != "Third" {
		t.Fatalf("paged snapshot rows = %+v", rows)
	}
}

/*
================
TestTickKillReadError

A failed kill read never fabricates or absorbs a change: snapshots
still go out from the states, and the pending update fires on the
first clean pass after the read recovers.
================
*/
func TestTickKillReadError(t *testing.T) {
	world := &feedWorld{states: []simulation.UniqueState{
		{RefObjID: 1954, Codename: "MOB_CH_TIGERGIRL", Level: 20, Alive: true},
	}}
	feed := New(world.sources())
	feed.Tick(0, []string{"s1"})

	world.killErr = errors.New("store unavailable")
	world.states[0].Alive = false
	snapshots, updates := feed.Tick(1000, []string{"s1", "s2"})
	if len(updates) != 0 {
		t.Fatalf("the failed read sent %d updates", len(updates))
	}
	if _, ok := snapshots["s2"]; !ok {
		t.Fatal("the failed read withheld a snapshot")
	}

	world.killErr = nil
	_, updates = feed.Tick(2000, []string{"s1", "s2"})
	if len(updates) != 1 {
		t.Fatalf("the recovered read sent %d updates", len(updates))
	}
}
