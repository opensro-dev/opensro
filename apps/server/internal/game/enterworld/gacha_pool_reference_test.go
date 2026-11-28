/*
===========================================================================

gacha_pool_reference_test.go - the published v1.696 Magic POP pool

The pool rides the public references under "gachaPool" only when a
source supplies it, and every prize must be a published item row.

===========================================================================
*/
package enterworld

import (
	"net/http/httptest"
	"strings"
	"testing"
)

/*
================
TestGachaPoolPublishedOnlyWithItsItems
================
*/
func TestGachaPoolPublishedOnlyWithItsItems(t *testing.T) {
	pool := &GachaPoolReference{Npc: 9251, VisibleSet: 1, LevelCap: 90,
		Entries: []GachaPoolEntry{{Entry: 1, RefObjID: 4140, Quantity: 1, MinLevelCap: 1, Plus: 3,
			Options: []GachaPoolOption{{Name: "str", Value: 3}}}},
		Tree: []GachaPoolBranch{{TypeID: [4]uint8{3, 1, 6, 2}, Group: 100, GroupLabel: "UIIT_CTL_WK_CH_WEAPON",
			Sub: 101, SubLabel: "UIIT_CTL_WK_CH_WEAPON_SWORD", Degrees: 15}}}
	refs, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{},
		StaticItems: []RefItemRow{{RefObjID: 4140}}, GachaPool: pool})
	if err != nil {
		t.Fatalf("a published pool refused: %v", err)
	}
	recorder := httptest.NewRecorder()
	refs.ServeHTTP(recorder, httptest.NewRequest("GET", refs.Path, nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `"gachaPool":{"npc":9251,"visibleSet":1,"levelCap":90,"entries":[{"entry":1,"refObjId":4140`) {
		t.Fatalf("the pool is missing from the references: status %d", recorder.Code)
	}
	if _, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{}, GachaPool: pool}); err == nil {
		t.Fatal("a pool naming an unpublished item was accepted")
	}
	refs, err = NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{}})
	if err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	refs.ServeHTTP(recorder, httptest.NewRequest("GET", refs.Path, nil))
	if strings.Contains(recorder.Body.String(), "gachaPool") {
		t.Fatal("a pool key without a pool")
	}
}

/*
================
TestGachaPoolHoldsTheReaderLimits

The server refuses the shape the browser's decodeGachaPool would throw
on: entry, option and label bounds, the tree's row cap and unique entry
ids. A pool the server publishes is one the window can decode.
================
*/
func TestGachaPoolHoldsTheReaderLimits(t *testing.T) {
	valid := func() *GachaPoolReference {
		return &GachaPoolReference{Npc: 9251, VisibleSet: 1, LevelCap: 90,
			Entries: []GachaPoolEntry{{Entry: 1, RefObjID: 4140, Quantity: 1, MinLevelCap: 1, Plus: 3,
				Options: []GachaPoolOption{{Name: "str", Value: 3, ParamID: 11}}}},
			Tree: []GachaPoolBranch{{TypeID: [4]uint8{3, 1, 6, 2}, Group: 100, GroupLabel: "UIIT_A",
				Sub: 101, SubLabel: "UIIT_B", Degrees: 15}}}
	}
	itemIDs := map[uint32]bool{4140: true}
	if err := validateGachaPool(valid(), itemIDs); err != nil {
		t.Fatalf("a valid pool refused: %v", err)
	}
	for name, breakIt := range map[string]func(*GachaPoolReference){
		"no machine":           func(p *GachaPoolReference) { p.Npc = 0 },
		"cap 0":                func(p *GachaPoolReference) { p.LevelCap = 0 },
		"cap past a byte":      func(p *GachaPoolReference) { p.LevelCap = 256 },
		"empty group label":    func(p *GachaPoolReference) { p.Tree[0].GroupLabel = "" },
		"empty sub label":      func(p *GachaPoolReference) { p.Tree[0].SubLabel = "" },
		"group label past 128": func(p *GachaPoolReference) { p.Tree[0].GroupLabel = strings.Repeat("A", gachaPoolMaxLabel+1) },
		"sub label past 128":   func(p *GachaPoolReference) { p.Tree[0].SubLabel = strings.Repeat("A", gachaPoolMaxLabel+1) },
		"tree past 4096 rows": func(p *GachaPoolReference) {
			p.Tree = make([]GachaPoolBranch, gachaPoolMaxTreeRows+1)
			for i := range p.Tree {
				p.Tree[i] = GachaPoolBranch{GroupLabel: "UIIT_A", SubLabel: "UIIT_B"}
			}
		},
		"no entries":            func(p *GachaPoolReference) { p.Entries = nil },
		"zero entry id":         func(p *GachaPoolReference) { p.Entries[0].Entry = 0 },
		"repeated entry id":     func(p *GachaPoolReference) { p.Entries = append(p.Entries, p.Entries[0]) },
		"zero reward":           func(p *GachaPoolReference) { p.Entries[0].RefObjID = 0 },
		"unpublished reward":    func(p *GachaPoolReference) { p.Entries[0].RefObjID = 4141 },
		"zero quantity":         func(p *GachaPoolReference) { p.Entries[0].Quantity = 0 },
		"level cap past a byte": func(p *GachaPoolReference) { p.Entries[0].MinLevelCap = 256 },
		"six options": func(p *GachaPoolReference) {
			p.Entries[0].Options = []GachaPoolOption{{Name: "str"}, {Name: "int"}, {Name: "dex"},
				{Name: "hp"}, {Name: "mp"}, {Name: "hr"}}
		},
		"empty option name":  func(p *GachaPoolReference) { p.Entries[0].Options[0].Name = "" },
		"option name past 4": func(p *GachaPoolReference) { p.Entries[0].Options[0].Name = "toolong" },
		"option name symbol": func(p *GachaPoolReference) { p.Entries[0].Options[0].Name = "s-r" },
		"negative value":     func(p *GachaPoolReference) { p.Entries[0].Options[0].Value = -1 },
		"value past a dword": func(p *GachaPoolReference) { p.Entries[0].Options[0].Value = 0x100000000 },
	} {
		pool := valid()
		breakIt(pool)
		if err := validateGachaPool(pool, itemIDs); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
