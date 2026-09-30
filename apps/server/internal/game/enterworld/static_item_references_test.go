/*
===========================================================================

static_item_references_test.go - the static item rows leave the login blob

A login used to resend every drop, alchemy and Magic Pop item row (BUG-035:
4.6 MB per entry). They are published once in the reference file; the
login carries only rows the file lacks. The browser merges the two and
rejects a repeated id, so the lists must be disjoint and together complete.

===========================================================================
*/
package enterworld

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http/httptest"
	"sort"
	"testing"
)

// oneSkillCatalogue is the smallest skill source a reference file accepts.
type oneSkillCatalogue struct{}

func (oneSkillCatalogue) SkillByID(uint32) (SkillRow, bool) { return SkillRow{}, false }

func (oneSkillCatalogue) SpawnSkillRows() []SpawnSkillRow { return []SpawnSkillRow{{}} }

/*
================
staticReferenceFixture

A character carrying one static item (the blade) and one that only a login
can name (the heavy armour), with the spear and the gold heaps static.
================
*/
func staticReferenceFixture() (*Deps, *Character) {
	character := chinaSpearman()
	items := testItems()
	for slot, codename := range []string{"ITEM_CH_BLADE_01_A", "ITEM_CH_M_HEAVY_01_LA_A"} {
		ref := items[codename]
		character.MissionInventory = append(character.MissionInventory, InventoryRow{
			Slot: int64(13 + slot), RefObjID: ref.RefObjID, Codename: codename, TypeFlags: ref.TypeFlags(), StackCount: 1,
		})
	}
	deps := testDeps(character)
	deps.StaticRefItemCodenames = func() []string {
		return []string{"ITEM_CH_BLADE_01_A", "ITEM_CH_SPEAR_01_A_DEF"}
	}
	return deps, character
}

func refItemIDs(rows []RefItemRow) []uint32 {
	ids := make([]uint32, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.RefObjID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

/*
================
TestPublishedStaticItemsLeaveTheLogin
================
*/
func TestPublishedStaticItemsLeaveTheLogin(t *testing.T) {
	deps, character := staticReferenceFixture()
	detached := refItemIDs(buildRefItemSnapshot(deps, DefaultDivisionID, character))

	static := StaticRefItemRows(deps)
	refs, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{}, StaticItems: static})
	if err != nil {
		t.Fatal(err)
	}
	deps.BrowserReferences = refs
	login := buildRefItemSnapshot(deps, DefaultDivisionID, character)
	for _, row := range login {
		if refs.itemIDs[row.RefObjID] {
			t.Fatalf("login repeats published item %d", row.RefObjID)
		}
	}
	if got := refItemIDs(append(append([]RefItemRow{}, static...), login...)); !equalIDs(got, detached) {
		t.Fatalf("published + login = %v, want the detached snapshot %v", got, detached)
	}
	if ids := refItemIDs(login); !equalIDs(ids, []uint32{5049}) {
		t.Fatalf("login rows = %v, want only the character-specific armour", ids)
	}

	// The served file carries the rows and names its contract.
	w := httptest.NewRecorder()
	q := httptest.NewRequest("GET", refs.Path, nil)
	q.Header.Set("Accept-Encoding", "gzip")
	refs.ServeHTTP(w, q)
	reader, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var served struct {
		ReferencesVersion int
		RefItemSnapshot   []RefItemRow
	}
	if err = json.Unmarshal(data, &served); err != nil {
		t.Fatal(err)
	}
	if served.ReferencesVersion != 2 || !equalIDs(refItemIDs(served.RefItemSnapshot), refItemIDs(static)) {
		t.Fatalf("served contract %d rows %v", served.ReferencesVersion, refItemIDs(served.RefItemSnapshot))
	}
}

/*
================
TestReferenceFileRefusesRepeatedItems
================
*/
func TestReferenceFileRefusesRepeatedItems(t *testing.T) {
	row := RefItemRow{RefObjID: 7, Codename: "ITEM_X"}
	_, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{}, StaticItems: []RefItemRow{row, row}})
	if err == nil {
		t.Fatal("repeated published item accepted")
	}
}

func equalIDs(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// manyCommands is an item command source past the browser's row limit.
type manyCommands int

func (n manyCommands) ItemCommandReferences() []ItemCommandReference {
	return make([]ItemCommandReference, int(n))
}

/*
================
TestReferenceFileRefusesTablesTheBrowserRefuses
================
*/
func TestReferenceFileRefusesTablesTheBrowserRefuses(t *testing.T) {
	_, err := NewBrowserReferences(BrowserReferenceSources{
		Skills:       oneSkillCatalogue{},
		ItemCommands: manyCommands(maxPublicReferenceRows + 1),
	})
	if err == nil {
		t.Fatal("an item command table past the browser's row limit was published")
	}
}
