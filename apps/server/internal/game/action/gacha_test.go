package action

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

func gachaFixture(t *testing.T) (*Runtime, *enterworld.Character, []byte) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	for i, name := range []string{gacha.CardCodename, gacha.WinCardCodename, gacha.LoseCardCodename} {
		variant := int64(2)
		if i == 0 {
			variant = 1
		}
		items[name] = &enterworld.ItemRef{RefObjID: uint32(900 + i), Codename: name, TypeIDs: [4]int64{3, 3, 14, variant}}
	}
	items[gacha.CardCodename].ParamDescriptions = [20]string{gacha.LoseCardCodename, gacha.WinCardCodename}
	rt, _ := newTestRuntime(c, gmItemSource{items})
	dir := t.TempDir()
	for name, value := range map[string]string{"gachaitemset.txt": "1 1 11459 5000 1 1\n1 2 11460 10000 1 2\n", "gachanpcmap.txt": "1 9251 1 2\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.ConfigureGacha(dir); err != nil {
		t.Fatal(err)
	}
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 9251, AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}}
	rt.Selected.Set(testDivision, c.Name, 17)
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: 900, Codename: gacha.CardCodename, TypeFlags: items[gacha.CardCodename].TypeFlags(), StackCount: 1}}
	return rt, c, wire.NewWriter(9).U32(17).U32(1).U8(13).Payload()
}

func TestGachaCommitResultOrderAndReplay(t *testing.T) {
	for _, roll := range []uint32{0, 4999, 5000, 9999} {
		t.Run(string(rune('A'+roll%26)), func(t *testing.T) {
			rt, c, p := gachaFixture(t)
			draws := 0
			rt.GachaRoll = func() (uint32, error) { draws++; return roll, nil }
			frames, reason := rt.HandleGachaRoll(testDivision, c, p)
			if reason != "" {
				t.Fatal(reason)
			}
			assertOpcodes(t, frames, 0x3645, 0xb053)
			win := roll < 5000
			want := uint32(902)
			code := byte(0)
			if win {
				want = 901
				code = 1
			}
			if c.MissionInventory[0].RefObjID != want || !reflect.DeepEqual(frames[1].Payload, []byte{1, code}) {
				t.Fatalf("wrong result: %+v %+v", c.MissionInventory, frames)
			}
			reward := uint64(11460)
			wantDraws := 2
			if win {
				reward = 11459
				wantDraws = 1
			}
			if draws != wantDraws {
				t.Fatalf("draws=%d want %d", draws, wantDraws)
			}
			if !reflect.DeepEqual(c.MissionInventory[0].MagicOptions, []uint64{reward, 1}) {
				t.Fatal("reward lost")
			}
			before := c.Snapshot()
			again, reason := rt.HandleGachaRoll(testDivision, c, p)
			if reason == "" || len(again) != 0 || draws != wantDraws || !reflect.DeepEqual(c.Snapshot(), before) {
				t.Fatal("replayed ticket mutated state")
			}
		})
	}
}

func TestGachaAdmissionFailuresPreserveTicket(t *testing.T) {
	for _, kind := range []string{"catalog", "random missing", "random failure", "random range", "selection", "outside scope", "disabled NPC", "entry", "slot", "ticket", "delete pending", "truncated", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			rt, c, p := gachaFixture(t)
			rt.GachaRoll = func() (uint32, error) { return 0, nil }
			switch kind {
			case "catalog":
				rt.GachaCatalog = nil
			case "random missing":
				rt.GachaRoll = nil
			case "random failure":
				rt.GachaRoll = func() (uint32, error) { return 0, errors.New("entropy unavailable") }
			case "random range":
				rt.GachaRoll = func() (uint32, error) { return 10000, nil }
			case "selection":
				rt.Selected.Set(testDivision, c.Name, 18)
			case "outside scope":
				rt.NpcRoster[0].Spawn.RegionID = 0
			case "disabled NPC":
				rt.NpcSpawn.Enabled = false
			case "entry":
				p[4] = 99
			case "slot":
				p[8] = 1
			case "ticket":
				c.MissionInventory[0].RefObjID = 999
			case "delete pending":
				c.DeletePending = true
			case "truncated":
				p = p[:8]
			case "trailing":
				p = append(p, 0)
			}
			before := c.Snapshot()
			frames, reason := rt.HandleGachaRoll(testDivision, c, p)
			if reason == "" || len(frames) != 0 || !reflect.DeepEqual(c.Snapshot(), before) {
				t.Fatalf("refusal changed state: %s %+v", reason, frames)
			}
		})
	}
}

func TestGachaOpeningUsesWorldAdmission(t *testing.T) {
	rt, c, _ := gachaFixture(t)
	p := wire.NewWriter(8).U32(17).U32(0x10000).Payload()
	frames, reason := rt.HandleGachaNpcAction(testDivision, c, p)
	if reason != "" {
		t.Fatal(reason)
	}
	assertOpcodes(t, frames, 0xb338)
	rt.NpcRoster[0].Spawn.RegionID = 0
	if frames, reason = rt.HandleGachaNpcAction(testDivision, c, p); reason == "" || len(frames) != 0 {
		t.Fatal("out-of-scope machine admitted")
	}
}

func TestGachaRewardsAreAdmittedToWorldReferences(t *testing.T) {
	rt, _, _ := gachaFixture(t)
	counts := map[string]int{}
	for _, name := range rt.StaticRefItemCodenames() {
		counts[name]++
	}
	for _, name := range rt.GachaCatalog.RewardCodenames() {
		if counts[name] != 1 {
			t.Fatalf("reward %s admitted %d times", name, counts[name])
		}
	}
}

func TestGachaFailedWasteDrawPreservesTicket(t *testing.T) {
	for _, bad := range []bool{false, true} {
		rt, c, p := gachaFixture(t)
		calls := 0
		rt.GachaRoll = func() (uint32, error) {
			calls++
			if calls == 1 {
				return 5000, nil
			}
			if bad {
				return 10000, nil
			}
			return 0, errors.New("waste entropy unavailable")
		}
		before := c.Snapshot()
		frames, reason := rt.HandleGachaRoll(testDivision, c, p)
		if reason == "" || len(frames) != 0 || calls != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatalf("failed waste draw committed: %v %s", frames, reason)
		}
	}
}
