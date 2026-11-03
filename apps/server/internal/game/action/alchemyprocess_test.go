/*
===========================================================================

alchemyprocess_test.go - alchemy compound and dissolve through the action handler

The published-dissolution case reads the shipped tables through
clientdata.RequireGameData, so a clean checkout skips it and the full check
runs it.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"reflect"
	"sort"
	"sync"
	"testing"
)

/*
================
TestAlchemyPublishedDissolveCommitsOnceThroughHandler

Two concurrent dissolutions of one published weapon commit exactly once.
================
*/
func TestAlchemyPublishedDissolveCommitsOnceThroughHandler(t *testing.T) {
	// The shipped tables, as every licensed test reads them: a clean
	// checkout skips, the full check (SRO_REQUIRE_GAME_DATA=1) fails.
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	catalog, err := alchemy.LoadCatalog(dir, enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	rt, c := alchemyRuntime()
	rt.Alchemy = catalog
	var weapon alchemy.Reference
	for _, ref := range catalog.Items {
		if ref.Flags&0x7fe == 0x32c && ref.Degree() == 1 && (weapon.ID == 0 || ref.ID < weapon.ID) {
			weapon = ref
		}
	}
	if weapon.ID == 0 {
		t.Fatal("missing published weapon")
	}
	rondo := catalog.Items["ITEM_ETC_ARCHEMY_RONDO_02"]
	c.MissionInventory = []enterworld.InventoryRow{
		{Slot: 13, RefObjID: weapon.ID, Codename: weapon.Name, TypeFlags: weapon.Flags, StackCount: 1},
		{Slot: 14, RefObjID: rondo.ID, Codename: rondo.Name, TypeFlags: rondo.Flags, StackCount: 5000},
	}
	results := make(chan []wire.Frame, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- rt.HandleAlchemyProcess(testDivision, c, alchemy.OpDissolve, []byte{2, 14, 13})
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for frames := range results {
		last := frames[len(frames)-1]
		if last.Opcode != alchemy.OpDissolveResult {
			t.Fatal(frames)
		}
		if reflect.DeepEqual(last.Payload, []byte{1}) {
			wins++
			if frames[0].Opcode != 0x3645 || !reflect.DeepEqual(frames[0].Payload, []byte{13, 8, 0, 0}) {
				t.Fatal("equipment removal must precede rewards", frames)
			}
			grants := 0
			for _, f := range frames {
				if f.Opcode == 0xb06d {
					grants++
				}
			}
			if grants == 0 {
				t.Fatal("success without rewards")
			}
		}
	}
	if wins != 1 {
		t.Fatal("double consumption", wins)
	}
	for _, row := range c.MissionInventory {
		if row.RefObjID == weapon.ID {
			t.Fatal("equipment survived", row)
		}
		if row.RefObjID == rondo.ID && row.StackCount != int64(5000-(weapon.Price/20000+1)) {
			t.Fatal("Rondo spent more than once", row)
		}
	}
}

/*
================
TestAlchemyCompoundConcurrentCommitAddsNewRowsOnce
================
*/
func TestAlchemyCompoundConcurrentCommitAddsNewRowsOnce(t *testing.T) {
	rt, c := alchemyRuntime()
	rt.Alchemy.Items = map[string]alchemy.Reference{
		"part":                      {ID: 10, Name: "part", Flags: 0x25ec, Stack: 250, Price: 1000, Params: [5]uint32{1, 1, 1, 1}, Descriptions: [5]string{"e", "w", "f", "a"}},
		"ITEM_ETC_ARCHEMY_RONDO_01": {ID: 11, Name: "ITEM_ETC_ARCHEMY_RONDO_01", Flags: 0x35ec, Stack: 5000},
	}
	for k, name := range []string{"e", "w", "f", "a"} {
		rt.Alchemy.Items[name] = alchemy.Reference{ID: uint32(20 + k), Name: name, Flags: 0x2dec, Stack: 5000}
	}
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: 10, Codename: "part", TypeFlags: 0x25ec, StackCount: 1}, {Slot: 14, RefObjID: 11, Codename: "ITEM_ETC_ARCHEMY_RONDO_01", TypeFlags: 0x35ec, StackCount: 2}}
	results := make(chan []wire.Frame, 2)
	var wg sync.WaitGroup
	for k := 0; k < 2; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- rt.HandleAlchemyProcess(testDivision, c, alchemy.OpCompound, []byte{2, 1, 1, 0, 0, 0, 1, 13})
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for frames := range results {
		last := frames[len(frames)-1]
		if last.Opcode == alchemy.OpCompoundResult && last.Payload[0] == 1 {
			wins++
		}
	}
	if wins != 1 || len(c.MissionInventory) != 4 {
		t.Fatal(wins, c.MissionInventory)
	}
	for _, row := range c.MissionInventory {
		if row.RefObjID < 20 || row.RefObjID > 23 || row.StackCount != 1 {
			t.Fatal(c.MissionInventory)
		}
	}
	before := c.Snapshot()
	rt.HandleAlchemyProcess(testDivision, c, alchemy.OpCompound, []byte{1})
	if len(c.MissionInventory) != len(before.MissionInventory) {
		t.Fatal("cancel rolled back committed rows")
	}
}

/*
================
TestAlchemyPartsLoopThroughHandler

The shipped tables, the whole parts loop through the action handler: a
monster part breaks into its four elements (compound mode 1 with the
void rondo), then a degree-eight-or-nine tablet fuses its four required
elements into its stone (compound mode 2). This is the loop the
community vote of 2026-10-24 brought back.
================
*/
func TestAlchemyPartsLoopThroughHandler(t *testing.T) {
	// As above: a clean checkout skips, the full check runs it.
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	catalog, err := alchemy.LoadCatalog(dir, enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	rt, c := alchemyRuntime()
	rt.Alchemy = catalog

	names := make([]string, 0, len(catalog.Items))
	for name := range catalog.Items {
		names = append(names, name)
	}
	sort.Strings(names)

	// Step 1: pick a published material whose four authored outputs
	// all resolve to published elements.
	var material alchemy.Reference
	for _, name := range names {
		ref := catalog.Items[name]
		if ref.Flags&0xfffe != 0x25ec {
			continue
		}
		ok := true
		for k := 0; k < 4; k++ {
			product, known := catalog.Items[ref.Descriptions[k]]
			if !known || product.Flags&0xfffe != 0x2dec || ref.Params[k] == 0 {
				ok = false
				break
			}
		}
		if ok {
			material = ref
			break
		}
	}
	if material.ID == 0 {
		t.Fatal("no published material breaks into four elements")
	}
	rondo := catalog.Items["ITEM_ETC_ARCHEMY_RONDO_01"]
	if rondo.ID == 0 {
		t.Fatal("missing ITEM_ETC_ARCHEMY_RONDO_01")
	}
	c.MissionInventory = []enterworld.InventoryRow{
		{Slot: 13, RefObjID: material.ID, Codename: material.Name, TypeFlags: material.Flags, StackCount: 10},
		{Slot: 14, RefObjID: rondo.ID, Codename: rondo.Name, TypeFlags: rondo.Flags, StackCount: 5000},
	}
	frames := rt.HandleAlchemyProcess(testDivision, c, alchemy.OpCompound, []byte{2, 1, 10, 0, 0, 0, 1, 13})
	last := frames[len(frames)-1]
	if last.Opcode != alchemy.OpCompoundResult || last.Payload[0] != 1 {
		t.Fatal("material break refused", last)
	}
	rows := map[string]int64{}
	for _, row := range c.MissionInventory {
		rows[row.Codename] = row.StackCount
	}
	for k := 0; k < 4; k++ {
		if rows[material.Descriptions[k]] != int64(material.Params[k])*10 {
			t.Fatal("element missing", material.Descriptions[k], rows)
		}
	}
	if rows[material.Name] != 0 {
		t.Fatal("material survived", rows)
	}
	if want := int64(5000 - (material.Price/1000+1)*10); rows[rondo.Name] != want {
		t.Fatal("rondo residue", rows[rondo.Name], want)
	}

	// Step 2: pick a degree-eight-or-nine tablet whose four required
	// elements and stone product are all published.
	var tablet alchemy.Reference
	for _, name := range names {
		ref := catalog.Items[name]
		if ref.Flags&0xfffe != 0x1dec || (ref.Degree() != 8 && ref.Degree() != 9) {
			continue
		}
		ok := true
		for k := 0; k < 4; k++ {
			element, known := catalog.Items[ref.Descriptions[k]]
			if !known || element.Flags&0xfffe != 0x2dec || ref.Params[k] == 0 {
				ok = false
				break
			}
		}
		stone, known := catalog.Items[ref.Descriptions[4]]
		if !ok || !known || (stone.Flags&0xfffe != 0x0dec && stone.Flags&0xfffe != 0x15ec && stone.Flags&0xfffe != 0x3dec) {
			continue
		}
		tablet = ref
		break
	}
	if tablet.ID == 0 {
		t.Fatal("no degree-eight-or-nine tablet fuses into a stone")
	}
	c.MissionInventory = []enterworld.InventoryRow{
		{Slot: 13, RefObjID: tablet.ID, Codename: tablet.Name, TypeFlags: tablet.Flags, StackCount: 1},
	}
	for k := 0; k < 4; k++ {
		element := catalog.Items[tablet.Descriptions[k]]
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: int64(14 + k), RefObjID: element.ID, Codename: element.Name,
			TypeFlags: element.Flags, StackCount: int64(tablet.Params[k]),
		})
	}
	frames = rt.HandleAlchemyProcess(testDivision, c, alchemy.OpCompound, []byte{2, 2, 1, 0, 0, 0, 5, 13, 14, 15, 16, 17})
	last = frames[len(frames)-1]
	if last.Opcode != alchemy.OpCompoundResult || last.Payload[0] != 1 {
		t.Fatal("tablet fuse refused", tablet.Name, last)
	}
	rows = map[string]int64{}
	for _, row := range c.MissionInventory {
		rows[row.Codename] = row.StackCount
	}
	if rows[tablet.Descriptions[4]] != 1 {
		t.Fatal("stone missing", tablet.Descriptions[4], rows)
	}
	if rows[tablet.Name] != 0 {
		t.Fatal("tablet survived", rows)
	}
	for k := 0; k < 4; k++ {
		if rows[tablet.Descriptions[k]] != 0 {
			t.Fatal("element survived", tablet.Descriptions[k], rows)
		}
	}
}
