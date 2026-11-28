/*
===========================================================================

gacha_v1696_test.go - the v1.696 Magic POP tables in the roll and exchange

SRO_MAGIC_POP_V1696 picks the later tables when the import staged them
and retires pool v2; the cap column gates the selected entry; the
exchange applies the row's [BIIV] spec (O plus, M options, V variance, D
durability) and still honours vouchers won from the v1.150 table.

===========================================================================
*/

package action

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	v1696BladeRef = 11470
	v1696StrID    = 101
	v1696IntID    = 102
)

/*
================
v1696Tables

Entry 1: the shipped gear spec (STR+3 INT+3, +3). Entry 2: a V override
behind the degree-10 cap column 91. Entry 3: a D override. Entry 4: the lose set.
The v1.150 table beside them carries a legacy lose pair (potion x7).
================
*/
func v1696Tables(t *testing.T, withV1696 bool) string {
	t.Helper()
	row := func(set, ref, weight, quantity, entry, specType, spec, level string) string {
		return "1\t" + set + "\t" + ref + "\t" + weight + "\t" + quantity + "\t" + entry + "\t0\t" + specType + "\t" + spec +
			"\t0\txxx\t0\txxx\t0\txxx\t" + level + "\t1\t1\t1\t1\n"
	}
	files := map[string]string{
		gacha.PrizeTableV1150: "1 1 11459 5000 1 1\n1 2 3630 10000 7 2\n",
		gacha.NpcMapV1150:     "1 9251 1 2\n",
	}
	if withV1696 {
		files[gacha.PrizeTableV1696] = row("1", "11459", "5000", "1", "1", "1", "[BIIV]<M:str,1,3><M:int,1,3><O:3>", "1") +
			row("1", "11460", "5000", "1", "2", "1", "[BIIV]<V:1023>", "91") +
			row("1", strconv.Itoa(v1696BladeRef), "5000", "1", "3", "1", "[BIIV]<D:60>", "1") +
			row("2", "3630", "10000", "1", "4", "0", "xxx", "1")
		files[gacha.NpcMapV1696] = "1\t9251\t1\t2\t3\t4\n"
		files[gacha.TreeTableV1696] = "1\t3\t1\t6\t2\t100\tUIIT_CTL_WK_CH_WEAPON\t101\tUIIT_CTL_WK_CH_WEAPON_SWORD\t15\n"
	}
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

/*
================
v1696Alchemy

The two degree-1 options the gear spec names, values 1..3.
================
*/
func v1696Alchemy() *alchemy.Catalog {
	params := [3]uint32{0x00010002, 0x00030000, 0}
	return &alchemy.Catalog{Magic: map[uint16]alchemy.Magic{
		v1696StrID: {ID: v1696StrID, Name: "MATTR_STR", Degree: 1, Tag: 0x737472, Params: params},
		v1696IntID: {ID: v1696IntID, Name: "MATTR_INT", Degree: 1, Tag: 0x696E74, Params: params},
	}}
}

/*
================
v1696Fixture

A runtime configured from v1696Tables, standing at the machine with a
ticket in slot 13. The returned payload rolls the given entry.
================
*/
func v1696Fixture(t *testing.T, withV1696 bool) (*Runtime, *enterworld.Character) {
	t.Helper()
	t.Setenv(EnvGachaPoolV2, "off")
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
	minimum := int64(55)
	items["GACHA_TEST_BLADE"] = &enterworld.ItemRef{RefObjID: v1696BladeRef, Codename: "GACHA_TEST_BLADE",
		TypeIDs: [4]int64{3, 1, 6, 2}, VarianceIntMin1c0: &minimum, MaxDurability: 69}
	rt, _ := newTestRuntime(c, gmItemSource{items})
	rt.Alchemy = v1696Alchemy()
	if err := rt.ConfigureGacha(v1696Tables(t, withV1696)); err != nil {
		t.Fatal(err)
	}
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{
		{ObjectID: 17, RefObjID: 9251, AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn},
		{ObjectID: 18, RefObjID: 9252, Codename: gachaOperatorCodename, AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn},
	}
	rt.Selected.Set(testDivision, c.Name, 17)
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: 900, Codename: gacha.CardCodename,
		TypeFlags: items[gacha.CardCodename].TypeFlags(), StackCount: 1}}
	return rt, c
}

// v1696Roll is the 0x7053 body for an entry with the ticket in slot 13.
func v1696Roll(entry uint32) []byte {
	return wire.NewWriter(9).U32(17).U32(entry).U8(13).Payload()
}

/*
================
v1696WinAndExchange

Wins the entry, then redeems the voucher at Gori; returns the granted row.
================
*/
func v1696WinAndExchange(t *testing.T, rt *Runtime, c *enterworld.Character, entry uint32, reward uint32) enterworld.InventoryRow {
	t.Helper()
	rt.GachaRoll = func() (uint32, error) { return 0, nil }
	if _, reason := rt.HandleGachaRoll(testDivision, c, v1696Roll(entry)); reason != "" {
		t.Fatal(reason)
	}
	rt.Selected.Set(testDivision, c.Name, 18)
	if _, reason := rt.HandleGachaExchange(testDivision, c, wire.NewWriter(4).U32(18).Payload()); reason != "" {
		t.Fatal(reason)
	}
	for _, row := range c.MissionInventory {
		if row.RefObjID == reward {
			return row
		}
	}
	t.Fatalf("reward %d not granted: %+v", reward, c.MissionInventory)
	return enterworld.InventoryRow{}
}

/*
================
TestMagicPopV1696FromEnv
================
*/
func TestMagicPopV1696FromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": true, "on": true, "1": true, "off": false, "0": false, "false": false, "NO": false} {
		t.Setenv(EnvMagicPopV1696, value)
		if got := MagicPopV1696FromEnv(); got != want {
			t.Fatalf("%q -> %v, want %v", value, got, want)
		}
	}
}

/*
================
TestConfigureGachaPicksTheTablesByFlag

On with the tables: v1.696, pool v2 retired even when its flag is on, the
pool published. On without them: the v1.150 machine. Off: the v1.150
machine even with the tables present.
================
*/
func TestConfigureGachaPicksTheTablesByFlag(t *testing.T) {
	t.Run("on", func(t *testing.T) {
		t.Setenv(EnvMagicPopV1696, "on")
		rt, _ := v1696Fixture(t, true)
		if !rt.GachaCatalog.V1696 || rt.GachaPoolV2 || rt.GachaPoolReference() == nil {
			t.Fatalf("v1696=%v poolV2=%v", rt.GachaCatalog.V1696, rt.GachaPoolV2)
		}
		t.Setenv(EnvGachaPoolV2, "on")
		if err := rt.ConfigureGacha(v1696Tables(t, true)); err != nil || rt.GachaPoolV2 {
			t.Fatalf("pool v2 not retired: err=%v active=%v", err, rt.GachaPoolV2)
		}
	})
	t.Run("on without tables", func(t *testing.T) {
		t.Setenv(EnvMagicPopV1696, "on")
		rt, _ := v1696Fixture(t, false)
		if rt.GachaCatalog.V1696 || rt.GachaPoolReference() != nil {
			t.Fatal("a v1.696 catalog without its tables")
		}
	})
	t.Run("on without tables keeps pool v2", func(t *testing.T) {
		// The fallback serves the v1.150 machine and pool v2 per its own
		// flag; the rules page reads this state (GachaRuleState).
		t.Setenv(EnvMagicPopV1696, "on")
		t.Setenv(EnvGachaPoolV2, "on")
		rt, _, _ := gachaPoolV2Fixture(t)
		v1696, poolV2 := rt.GachaRuleState()
		if v1696 || !poolV2 || rt.GachaPoolReference() != nil {
			t.Fatalf("fallback settled v1696=%v poolV2=%v", v1696, poolV2)
		}
	})
	t.Run("off", func(t *testing.T) {
		t.Setenv(EnvMagicPopV1696, "off")
		rt, _ := v1696Fixture(t, true)
		if rt.GachaCatalog.V1696 || rt.GachaPoolReference() != nil {
			t.Fatal("the v1.696 tables served while off")
		}
		if prize, ok := rt.GachaCatalog.PrizeForNpc(9251, 1); !ok || prize.ChancePer10000 != 5000 || len(prize.Spec.Tokens) != 0 {
			t.Fatalf("v1.150 entry 1 = %+v ok=%v", prize, ok)
		}
	})
}

/*
================
TestConfigureGachaRefusesAnUnresolvedSpecOption

The v1.188 row loader's M check: an option the magic table lacks at the
token's degree, or a value outside its range, fails construction.
================
*/
func TestConfigureGachaRefusesAnUnresolvedSpecOption(t *testing.T) {
	t.Setenv(EnvMagicPopV1696, "on")
	rt, _ := v1696Fixture(t, true)
	delete(rt.Alchemy.Magic, v1696IntID)
	if err := rt.ConfigureGacha(v1696Tables(t, true)); err == nil {
		t.Fatal("a spec naming a missing option was accepted")
	}
	rt.Alchemy = v1696Alchemy()
	option := rt.Alchemy.Magic[v1696StrID]
	option.Params = [3]uint32{0x00010002, 0, 0}
	rt.Alchemy.Magic[v1696StrID] = option
	if err := rt.ConfigureGacha(v1696Tables(t, true)); err == nil {
		t.Fatal("a spec value outside its option's range was accepted")
	}
}

/*
================
TestGachaV1696CapColumnGatesTheEntry

A degree-10 row (column 91) rolls at cap 90, the default cap
(gacha.degree10OpensAt). The column still gates a roll: below
it the entry is refused and the ticket kept.
================
*/
func TestGachaV1696CapColumnGatesTheEntry(t *testing.T) {
	t.Setenv(EnvMagicPopV1696, "on")
	rt, c := v1696Fixture(t, true)
	rt.GachaRoll = func() (uint32, error) { return 0, nil }
	if _, reason := rt.HandleGachaRoll(testDivision, c, v1696Roll(2)); reason != "" {
		t.Fatalf("cap 90 refused the degree-10 entry: %s", reason)
	}
	rt, c = v1696Fixture(t, true)
	rt.GachaRoll = func() (uint32, error) { return 0, nil }
	rt.GachaLevelCap = progression.LevelCap - 1
	frames, reason := rt.HandleGachaRoll(testDivision, c, v1696Roll(2))
	if reason == "" {
		t.Fatal("the degree-10 entry rolled below its cap column")
	}
	assertGachaRefusal(t, frames, gacha.OpResult, gachaRefusalNoEntry)
	if c.MissionInventory[0].RefObjID != 900 {
		t.Fatalf("the ticket was spent: %+v", c.MissionInventory)
	}
}

/*
================
TestGachaV1696ExchangeAppliesThePlusAndOptions

O fixes the plus and M writes exactly the listed options, id low and
value high.
================
*/
func TestGachaV1696ExchangeAppliesThePlusAndOptions(t *testing.T) {
	t.Setenv(EnvMagicPopV1696, "on")
	rt, c := v1696Fixture(t, true)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	granted := v1696WinAndExchange(t, rt, c, 1, 11459)
	want := []uint64{3<<32 | v1696StrID, 3<<32 | v1696IntID}
	if granted.Plus != 3 || !reflect.DeepEqual(granted.MagicOptions, want) {
		t.Fatalf("granted plus %d options %v, want +3 %v", granted.Plus, granted.MagicOptions, want)
	}
}

/*
================
TestGachaV1696ExchangeAppliesVarianceAndDurability

V fixes the variance word and durability follows its first field; D
alone clamps the durability to the row's range and leaves no variance.
================
*/
func TestGachaV1696ExchangeAppliesVarianceAndDurability(t *testing.T) {
	t.Setenv(EnvMagicPopV1696, "on")
	t.Setenv(progression.EnvLevelCap, "110")
	rt, c := v1696Fixture(t, true)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	sword, _ := rt.deps.ItemReferences().ItemRefByID(11460)
	// The fixture sword carries no durability row; give it the authored
	// range the V field derives from.
	minimum := int64(55)
	sword.VarianceIntMin1c0 = &minimum
	sword.MaxDurability = 69
	granted := v1696WinAndExchange(t, rt, c, 2, 11460)
	if granted.VarianceBits != "1023" || granted.Durability != int64(combat.DurabilityFromVariance(sword, 31)) {
		t.Fatalf("V row granted variance %s durability %d", granted.VarianceBits, granted.Durability)
	}
	rt, c = v1696Fixture(t, true)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	granted = v1696WinAndExchange(t, rt, c, 3, v1696BladeRef)
	if granted.Durability != 60 || (granted.VarianceBits != "0" && granted.VarianceBits != "") {
		t.Fatalf("D row granted variance %q durability %d, want none and 60", granted.VarianceBits, granted.Durability)
	}
}

/*
================
TestGachaV1696ExchangeRedeemsALegacyVoucher

A lose voucher written from the v1.150 table (potion x7) still pays
under the v1.696 tables.
================
*/
func TestGachaV1696ExchangeRedeemsALegacyVoucher(t *testing.T) {
	t.Setenv(EnvMagicPopV1696, "on")
	rt, c := v1696Fixture(t, true)
	lose := rt.GachaCatalog.LoseCard
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: lose.RefObjID, Codename: lose.Codename,
		TypeFlags: lose.TypeFlags, StackCount: 1, MagicOptions: []uint64{3630, 7}}}
	rt.Selected.Set(testDivision, c.Name, 18)
	if _, reason := rt.HandleGachaExchange(testDivision, c, wire.NewWriter(4).U32(18).Payload()); reason != "" {
		t.Fatal(reason)
	}
	if len(c.MissionInventory) != 1 || c.MissionInventory[0].RefObjID != 3630 || c.MissionInventory[0].StackCount != 7 {
		t.Fatalf("legacy voucher paid %+v", c.MissionInventory)
	}
}

/*
================
TestGachaV1696ExchangeRefusesAnUnresolvedOption

Without the magic table the M tokens cannot be written, so the exchange
refuses and the voucher stays.
================
*/
func TestGachaV1696ExchangeRefusesAnUnresolvedOption(t *testing.T) {
	t.Setenv(EnvMagicPopV1696, "on")
	rt, c := v1696Fixture(t, true)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	rt.GachaRoll = func() (uint32, error) { return 0, nil }
	if _, reason := rt.HandleGachaRoll(testDivision, c, v1696Roll(1)); reason != "" {
		t.Fatal(reason)
	}
	rt.Alchemy = nil
	rt.Selected.Set(testDivision, c.Name, 18)
	frames, reason := rt.HandleGachaExchange(testDivision, c, wire.NewWriter(4).U32(18).Payload())
	if reason == "" {
		t.Fatal("an unresolved option was exchanged")
	}
	assertGachaRefusal(t, frames, gacha.OpExchangeResult, gachaRefusalFailed)
	if !rt.GachaCatalog.IsResultCard(c.MissionInventory[0].RefObjID) {
		t.Fatalf("the voucher was spent: %+v", c.MissionInventory)
	}
}

/*
================
TestGachaV1696PoolPublishesTheOptionIdsTheExchangeWrites

The pool's option lines carry the magicoption row id the exchange stores
in each option word's low half, so the browser's tooltip names the same
option the server pays.
================
*/
func TestGachaV1696PoolPublishesTheOptionIdsTheExchangeWrites(t *testing.T) {
	t.Setenv(EnvMagicPopV1696, "on")
	rt, c := v1696Fixture(t, true)
	pool := rt.GachaPoolReference()
	if pool == nil {
		t.Fatal("the v1.696 machine published no pool")
	}
	var options []enterworld.GachaPoolOption
	for _, entry := range pool.Entries {
		if entry.Entry == 1 {
			options = entry.Options
		}
	}
	if len(options) != 2 || options[0].ParamID != v1696StrID || options[1].ParamID != v1696IntID {
		t.Fatalf("published options %+v", options)
	}
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	granted := v1696WinAndExchange(t, rt, c, 1, 11459)
	if len(granted.MagicOptions) != len(options) {
		t.Fatalf("exchange wrote %v options, pool published %+v", granted.MagicOptions, options)
	}
	for i, option := range options {
		if wrote := uint16(granted.MagicOptions[i] & 0xffff); wrote != option.ParamID {
			t.Fatalf("option %d published id %d, the exchange wrote %d", i, option.ParamID, wrote)
		}
	}
}

/*
================
TestGachaV1696ZeroDurabilityPrizeKeepsZero

An avatar attach (flags 3.1.13.4) carries Dur_U 0: its prize gets
durability 0, not DurabilityFromVariance's floor of one, whatever the
spec or the roll draws.
================
*/
func TestGachaV1696ZeroDurabilityPrizeKeepsZero(t *testing.T) {
	rt, _ := v1696Fixture(t, true)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	attach := &enterworld.ItemRef{
		RefObjID: 11499,
		Codename: "ITEM_EVENT_AVATAR_ATTACH",
		TypeIDs:  [4]int64{3, 1, 13, 4},
	}
	for name, text := range map[string]string{
		"variance":   "[BIIV]<V:1023>",
		"durability": "[BIIV]<D:5>",
		"rolled":     "",
	} {
		spec := gacha.Spec{}
		if text != "" {
			parsed, err := gacha.ParseSpec(text)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			spec = parsed
		}
		item, err := rt.prepareGachaRewardItem(attach, 1, spec)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if item.Durability != 0 {
			t.Fatalf("%s: durability %d, want 0", name, item.Durability)
		}
	}
}

/*
================
TestV1696LosePrizesWithoutAnEffectAreRefused

The owner ships the v1.696 lose set as native (2026-11-28), effects or
not. Until a prize's effect lands, its use must be refused with nothing
spent, never reach another family: the skill point scroll (3/3/3/12), the
alchemy catalyst (3/3/11/17), the summon flowers (3/3/3/20) and the Hwan
look items (3/3/13/13, Param1 6).
================
*/
func TestV1696LosePrizesWithoutAnEffectAreRefused(t *testing.T) {
	for _, tc := range []struct {
		codename string
		typeIDs  [4]int64
		param1   float64
	}{
		{"ITEM_EVENT_SKILLPOINT_SCROLL", [4]int64{3, 3, 3, 12}, 0},
		{"ITEM_EVENT_ARCHEMY_REINFORCE_RATIO_100", [4]int64{3, 3, 11, 17}, 0},
		{"ITEM_ETC_SUMMON_FLOWER_01", [4]int64{3, 3, 3, 20}, 0},
		{"ITEM_ETC_WHAN_LOOK_CHANGE_C_PANDA", [4]int64{3, 3, 13, 13}, 6},
	} {
		ref := &enterworld.ItemRef{Codename: tc.codename, TypeIDs: tc.typeIDs,
			NativeFields: enterworld.NewNativeFields(map[string]float64{"itemParam1_29c": tc.param1})}
		if got := admittedItemUseFamily(ref); got != itemUseUnsupported {
			t.Errorf("%s: family %d, want unsupported", tc.codename, got)
		}
	}
}
