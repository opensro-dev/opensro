/*
===========================================================================

hwan_chain_test.go - the Army / Military Test chains' tools, rolls and pay

Runs over the shipped data when the build has the cap-110 quest tables (it
skips otherwise). The tools are driven through the same entry points the item
lane uses; the rolls are injected, so no test depends on chance.

===========================================================================
*/
package quest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// hwanChainCharacter is a Chinese character of the given level with the named quests done.
func hwanChainCharacter(t *testing.T, defs *Definitions, level int64, done ...string) *enterworld.Character {
	t.Helper()
	c := hwanCharacter("CHAR_CH_MAN_ADVENTURER", level)
	for _, code := range done {
		def, ok := defs.ByCodename(code)
		if !ok {
			t.Fatalf("%s is not loaded", code)
		}
		c.CompletedQuestIds = append(c.CompletedQuestIds, def.RefID)
	}
	return c
}

func hwanItemCount(c *enterworld.Character, code string) uint32 { return captureItemCount(c, code) }

func notifiedWith(frames []wire.Frame, symbol string) bool {
	want := questNotification(symbol)
	for _, frame := range frames {
		if frame.Opcode == want.Opcode && string(frame.Payload) == string(want.Payload) {
			return true
		}
	}
	return false
}

func TestHwanChainsAreGatedByLevelAndPredecessor(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	low := hwanChainCharacter(t, defs, 94)
	if _, err := rt.StartQuest(low, "QNO_CH_HWAN_1_1"); err == nil {
		t.Fatal("level 94 took the 95 chain")
	}
	c := hwanChainCharacter(t, defs, 95)
	if _, err := rt.StartQuest(c, "QNO_CH_HWAN_1_2"); err == nil {
		t.Fatal("1_2 was taken before the trial")
	}
	if _, err := rt.StartQuest(c, "QNO_EU_HWAN_1_1"); err == nil {
		t.Fatal("a Chinese character took the European trial")
	}
	if _, err := rt.StartQuest(c, "QNO_CH_HWAN_1_1"); err != nil {
		t.Fatal(err)
	}
	// Chain 2 needs level 100 and the whole of chain 1.
	ninety := hwanChainCharacter(t, defs, 99, "QNO_CH_HWAN_1_6")
	if _, err := rt.StartQuest(ninety, "QNO_CH_HWAN_2_1"); err == nil {
		t.Fatal("level 99 took the 100 chain")
	}
	chainOne := hwanChainCharacter(t, defs, 100)
	if _, err := rt.StartQuest(chainOne, "QNO_CH_HWAN_2_1"); err == nil {
		t.Fatal("the 100 chain started without the 95 chain")
	}
	ready := hwanChainCharacter(t, defs, 100, "QNO_CH_HWAN_1_6")
	if _, err := rt.StartQuest(ready, "QNO_CH_HWAN_2_1"); err != nil {
		t.Fatal(err)
	}
}

func TestSoulRingingBellSummonsTheGhostOnlyAtTheCemetery(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	c := hwanChainCharacter(t, defs, 95, "QNO_CH_HWAN_1_1", "QNO_CH_HWAN_1_2", "QNO_CH_HWAN_1_3")
	if _, err := rt.StartQuest(c, "QNO_CH_HWAN_1_3_1"); err != nil {
		t.Fatal(err)
	}
	if hwanItemCount(c, hwanPurifiedBell) != 1 {
		t.Fatalf("acceptance left %d purified bells", hwanItemCount(c, hwanPurifiedBell))
	}
	var summoned []simulation.QuestMonsterSpawn
	rt.SpawnQuestMonster = func(_ *enterworld.Character, request simulation.QuestMonsterSpawn) bool {
		summoned = append(summoned, request)
		return true
	}
	elsewhere := simulation.Spawn{RegionID: hwanBellRegion, X: 200, Y: 230, Z: 200}
	frames, used := rt.BeginItemUse(c, hwanPurifiedBell, elsewhere, 0)
	if used || len(summoned) != 0 || !notifiedWith(frames, "SN_TALK_QNO_CH_HWAN_1_3_1_10") {
		t.Fatalf("the bell worked far from the cemetery: used=%v frames=%v", used, frames)
	}
	here := hwanBellPoint()
	if _, used = rt.BeginItemUse(c, hwanPurifiedBell, here, 0); !used || len(summoned) != 1 {
		t.Fatalf("the bell did not summon at the cemetery: used=%v", used)
	}
	if got := summoned[0]; got.Codename != hwanGhost || got.RadiusMin != 90 || got.RadiusSpan != 10 {
		t.Fatalf("summoned %+v, want %s on the 90 to 100 ring", got, hwanGhost)
	}
	// Holding the shell, the bell is refused.
	rows, _, err := rt.PlanInventory(c, nil, itemLead(hwanShell))
	if err != nil {
		t.Fatal(err)
	}
	c.MissionInventory = rows
	if _, used = rt.BeginItemUse(c, hwanPurifiedBell, here, 0); used {
		t.Fatal("the bell worked with the shell already held")
	}
}

func TestMountainEssenceWorksOnlyOnAYeoha(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	c := hwanChainCharacter(t, defs, 100, "QNO_CH_HWAN_2_1", "QNO_CH_HWAN_2_2")
	if _, err := rt.StartQuest(c, "QNO_CH_HWAN_2_3"); err != nil {
		t.Fatal(err)
	}
	at := simulation.Spawn{RegionID: 23716, X: 500, Y: 0, Z: 500}
	rt.SelectedMonster = func(*enterworld.Character) (string, simulation.Spawn, bool) { return "", simulation.Spawn{}, false }
	if frames, used := rt.BeginItemUse(c, hwanMountainEssence, at, 0); used || !notifiedWith(frames, noSelectedTarget) {
		t.Fatalf("no target: used=%v frames=%v", used, frames)
	}
	rt.SelectedMonster = func(*enterworld.Character) (string, simulation.Spawn, bool) { return "MOB_CH_TIGER", at, true }
	if frames, used := rt.BeginItemUse(c, hwanMountainEssence, at, 0); used || !notifiedWith(frames, "SN_TALK_QNO_CH_HWAN_2_3_09") {
		t.Fatalf("wrong target: used=%v frames=%v", used, frames)
	}
	far := simulation.Spawn{RegionID: 23716, X: 1500, Y: 0, Z: 1500}
	rt.SelectedMonster = func(*enterworld.Character) (string, simulation.Spawn, bool) { return "MOB_CH_YEOHA", far, true }
	if _, used := rt.BeginItemUse(c, hwanMountainEssence, at, 0); used {
		t.Fatal("the essence worked on a Yeoha beyond 300")
	}
	rt.SelectedMonster = func(*enterworld.Character) (string, simulation.Spawn, bool) { return "MOB_CH_YEOHA_CLON", at, true }
	if _, used := rt.BeginItemUse(c, hwanMountainEssence, at, 0); !used || hwanItemCount(c, hwanBranchOfLife) != 1 {
		t.Fatalf("a Decayed Yeoha gave %d Branches of Life", hwanItemCount(c, hwanBranchOfLife))
	}
	if _, used := rt.BeginItemUse(c, hwanMountainEssence, at, 0); used {
		t.Fatal("a second Branch of Life was made")
	}
}

func TestSunGodRelicDigFollowsItsKnob(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	dig := func(knobs HwanKnobs, roll uint32) (relics uint32, notices []wire.Frame) {
		rt.Hwan = knobs
		c := hwanChainCharacter(t, defs, 100, "QNO_CH_HWAN_2_1", "QNO_CH_HWAN_2_2", "QNO_CH_HWAN_2_3")
		if _, err := rt.StartQuest(c, "QNO_CH_HWAN_2_4"); err != nil {
			t.Fatal(err)
		}
		rt.CaptureRoll = func() (uint32, error) { return roll, nil }
		elsewhere := simulation.Spawn{RegionID: 25954, X: 100, Y: 180, Z: 100}
		if frames, started := rt.BeginItemUse(c, hwanRadiantCrystal, elsewhere, 0); started || !notifiedWith(frames, "SN_TALK_QNO_CH_HWAN_2_4_09") {
			t.Fatalf("the crystal worked far from the gate: started=%v", started)
		}
		if _, started := rt.BeginItemUse(c, hwanRadiantCrystal, hwanDigPoint(), 0); !started {
			t.Fatal("the dig did not start at the gate")
		}
		var frames []wire.Frame
		for second := int64(1); second <= hwanDigSeconds; second++ {
			frames = append(frames, rt.AdvanceItemUse(c, second*questSecondMs)...)
		}
		return hwanItemCount(c, hwanSunRelic), frames
	}
	// Original: rand() mod 101 <= 10 wins.
	if relics, frames := dig(HwanKnobs{}, 10); relics != 1 || !notifiedWith(frames, "SN_TALK_QNO_CH_HWAN_2_4_08") {
		t.Errorf("roll 10 gave %d relics", relics)
	}
	if relics, frames := dig(HwanKnobs{}, 11); relics != 0 || !notifiedWith(frames, "SN_TALK_QNO_CH_HWAN_2_4_10") {
		t.Errorf("roll 11 gave %d relics", relics)
	}
	// Knob: 50 percent over 100 outcomes.
	half := HwanKnobs{SunRelicPercent: 50, SunRelicSet: true}
	if relics, _ := dig(half, 49); relics != 1 {
		t.Errorf("50%% knob, roll 49 gave %d relics", relics)
	}
	if relics, _ := dig(half, 50); relics != 0 {
		t.Errorf("50%% knob, roll 50 gave %d relics", relics)
	}
}

func TestNiyaTrapRollsNothingFlutterOrNormal(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	setup := func() *enterworld.Character {
		c := hwanChainCharacter(t, defs, 95, "QNO_CH_HWAN_1_1", "QNO_CH_HWAN_1_2", "QNO_CH_HWAN_1_3", "QNO_CH_HWAN_1_3_1", "QNO_CH_HWAN_1_4")
		if _, err := rt.StartQuest(c, "QNO_CH_HWAN_1_4_1"); err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := setup()
	if got := hwanItemCount(c, hwanSpiritTrap); got != hwanTrapSupply {
		t.Fatalf("acceptance gave %d traps, want %d", got, hwanTrapSupply)
	}
	if _, ok := rt.CanPlaceTrap(c, hwanNiyaSkill); !ok {
		t.Fatal("the trap was refused while the trap quest runs")
	}
	if _, ok := rt.CanPlaceTrap(hwanChainCharacter(t, defs, 95), hwanNiyaSkill); ok {
		t.Fatal("the trap was admitted without its quest")
	}
	retired := 0
	retire := func() bool { retired++; return true }
	try := func(roll uint32) ([]wire.Frame, bool) {
		rt.CaptureRoll = func() (uint32, error) { return roll, nil }
		return rt.CaptureQuestTrap(c, hwanNiyaSkill, hwanNiyaGeneral, retire)
	}
	if frames, ok := try(50); ok || retired != 0 || !notifiedWith(frames, "SN_TALK_QNO_CH_HWAN_1_4_1_09") {
		t.Fatalf("roll 50 (nothing): ok=%v retired=%d", ok, retired)
	}
	if frames, ok := try(5); !ok || retired != 1 || hwanItemCount(c, hwanFlutterSpirit) != 1 || !notifiedWith(frames, "SN_TALK_QNO_CH_HWAN_1_4_1_14") {
		t.Fatalf("roll 5 (flutter): ok=%v retired=%d", ok, retired)
	}
	if frames, ok := try(1); !ok || retired != 2 || hwanItemCount(c, hwanNormalSpirit) != 1 || !notifiedWith(frames, "SN_TALK_QNO_CH_HWAN_1_4_1_08") {
		t.Fatalf("roll 1 (normal): ok=%v retired=%d", ok, retired)
	}
	if _, ok := rt.CaptureQuestTrap(c, hwanNiyaSkill, "MOB_CH_TIGER", retire); ok || retired != 2 {
		t.Fatal("a trap closed on a monster that is not a Niya General")
	}
	// The knob replaces the split: a certain Normal Spirit.
	rt.Hwan = HwanKnobs{NiyaRates: [3]uint8{0, 0, 100}, NiyaRatesSet: true}
	c = setup()
	if _, ok := try(99); !ok || hwanItemCount(c, hwanNormalSpirit) != 1 {
		t.Fatal("0,0,100 did not give the Normal Spirit")
	}
	rt.Hwan = HwanKnobs{}
}

func TestTrapQuestPaysTenGoldPerFlutterSpiritAndTakesThem(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	c := hwanChainCharacter(t, defs, 95, "QNO_CH_HWAN_1_1", "QNO_CH_HWAN_1_2", "QNO_CH_HWAN_1_3", "QNO_CH_HWAN_1_3_1", "QNO_CH_HWAN_1_4")
	if _, err := rt.StartQuest(c, "QNO_CH_HWAN_1_4_1"); err != nil {
		t.Fatal(err)
	}
	rows, _, err := rt.PlanInventory(c, nil, itemLeads(hwanNormalSpirit, 1, hwanFlutterSpirit, 7))
	if err != nil {
		t.Fatal(err)
	}
	c.MissionInventory = rows
	rt.applyInventoryChange(c)
	gold := int64(1000)
	c.Gold = &gold
	if _, err := rt.AdvanceNpcQuest(c, "QNO_CH_HWAN_1_4_1", "NPC_CH_SHAMAN"); err != nil {
		t.Fatal(err)
	}
	if *c.Gold != 1000+7*hwanFlutterGold {
		t.Fatalf("gold %d, want %d", *c.Gold, 1000+7*hwanFlutterGold)
	}
	for _, code := range []string{hwanNormalSpirit, hwanFlutterSpirit, hwanSpiritTrap} {
		if hwanItemCount(c, code) != 0 {
			t.Errorf("%s was left in the bag", code)
		}
	}
}

func TestSoulFragmentDeliveryRaisesTheBlueBerserkLevel(t *testing.T) {
	rt, defs := shippedHwanRuntime(t)
	rt.HwanLevelFrames = func(*enterworld.Character) []wire.Frame { return nil }
	for chain, want := range map[int]uint8{1: 1, 2: 2} {
		done := []string{"QNO_CH_HWAN_1_1", "QNO_CH_HWAN_1_2", "QNO_CH_HWAN_1_3", "QNO_CH_HWAN_1_3_1", "QNO_CH_HWAN_1_4", "QNO_CH_HWAN_1_4_1", "QNO_CH_HWAN_1_5"}
		level, last := int64(95), "QNO_CH_HWAN_1_6"
		if chain == 2 {
			done = append(done, "QNO_CH_HWAN_1_6", "QNO_CH_HWAN_2_1", "QNO_CH_HWAN_2_2", "QNO_CH_HWAN_2_3", "QNO_CH_HWAN_2_4", "QNO_CH_HWAN_2_5")
			level, last = 100, "QNO_CH_HWAN_2_6"
		}
		c := hwanChainCharacter(t, defs, level, done...)
		if _, err := rt.StartQuest(c, last); err != nil {
			t.Fatal(err)
		}
		if _, err := rt.AdvanceNpcQuest(c, last, "NPC_CH_SHAMAN"); err == nil {
			t.Fatal("the fragment was taken by the wrong NPC")
		}
		if _, err := rt.AdvanceNpcQuest(c, last, "NPC_CH_GENARAL"); err != nil {
			t.Fatal(err)
		}
		if rt.HwanLevel(c) != want {
			t.Errorf("%s gave Blue Berserk level %d, want %d", last, rt.HwanLevel(c), want)
		}
	}
}

// itemLead lists one item for the inventory planner.
func itemLead(code string) []inventory.ItemAmount {
	return []inventory.ItemAmount{{Codename: code, Count: 1}}
}

// itemLeads lists item and count pairs for the inventory planner.
func itemLeads(pairs ...any) []inventory.ItemAmount {
	var out []inventory.ItemAmount
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, inventory.ItemAmount{Codename: pairs[i].(string), Count: uint32(pairs[i+1].(int))})
	}
	return out
}

// The Niya trap's skill compiles to the quest-trap shape the item lane places: a 60 s
// trap, radius 20, aimed at the Niya General's reference id.
func TestNiyaTrapSkillCompilesToAQuestTrap(t *testing.T) {
	_, _ = shippedHwanRuntime(t)
	dir := gamedatatest.TextdataDir(t)
	skill, ok := enterworld.NewTextdataSkills(dir).SkillByCodename(hwanNiyaSkill)
	if !ok {
		t.Fatalf("%s is not in the skill table", hwanNiyaSkill)
	}
	trap := skill.CastGate.QuestTrap
	if !trap.Present || trap.DurationMs != 60000 || trap.Radius != 20 {
		t.Fatalf("trap = %+v, want a present 60 s radius 20 program", trap)
	}
	ref, found := enterworld.NewTextdataItems(dir).ItemRefByCodename(hwanSpiritTrap)
	monster, _ := enterworld.NewTextdataItems(dir).ItemRefByCodename(hwanNormalSpirit)
	if !found || monster == nil || ref == nil {
		t.Fatal("trap items missing")
	}
	if trap.Targets[0] != 3809 {
		t.Fatalf("targets %v, want the Niya General's id 3809 first", trap.Targets)
	}
}

// shippedTextSymbols collects the textquest and textuisystem keys that have English text. The
// tables are read raw (UTF-16, tab columns, English in column 8): the server's own row reader
// skips lines with quotes, which the client's dialog text does not.
func shippedTextSymbols(t *testing.T) map[string]bool {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	keys := map[string]bool{}
	for _, name := range []string{"textquest.txt", "textuisystem.txt"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(raw) < 2 {
			t.Fatalf("%s: %v", name, err)
		}
		words := make([]uint16, 0, len(raw)/2)
		for i := 2; i+1 < len(raw); i += 2 {
			words = append(words, uint16(raw[i])|uint16(raw[i+1])<<8)
		}
		for _, line := range strings.Split(string(utf16.Decode(words)), "\r\n") {
			row := strings.Split(line, "\t")
			if len(row) > 8 && strings.TrimSpace(row[8]) != "" {
				keys[strings.TrimSpace(row[1])] = true
			}
		}
	}
	return keys
}

// Every line a Blue Berserk quest, supply or arena can speak has English text in the
// shipped tables: a wrong number in a symbol would otherwise show an empty dialog.
func TestEverySymbolTheHwanQuestsSpeakHasText(t *testing.T) {
	_, defs := shippedHwanRuntime(t)
	text := shippedTextSymbols(t)
	check := func(owner, symbol string) {
		if symbol != "" && !text[symbol] {
			t.Errorf("%s speaks %s, which has no English text", owner, symbol)
		}
	}
	for _, def := range defs.All() {
		if !strings.Contains(def.Codename, "HWAN") && !strings.Contains(def.Codename, "SD_HW") {
			continue
		}
		for _, symbol := range []string{def.OfferPromptSymbol, def.AcceptResponseSymbol, def.DenyResponseSymbol, def.NotAchievedSymbol,
			def.CompletePromptSymbol, def.InventoryFullSymbol, def.AchievedNowSymbol, def.AcceptNoticeSymbol, def.CompleteNoticeSymbol,
			def.OfferAcceptRowSymbol, def.TitleSymbol} {
			check(def.Codename, symbol)
		}
	}
	for _, supply := range hwanSupplies {
		for _, symbol := range []string{supply.title, supply.prompt, supply.success, supply.full} {
			check(supply.quest, symbol)
		}
	}
	for _, arena := range hwanArenas {
		talk := hwanTalk(arena.quest())
		spoken := []string{"09", "10"}
		if arena.kind == hwanTrial {
			spoken = []string{"08", "09", "10", "11"}
			if arena.chain == 2 {
				spoken = []string{"11", "12", "13", "14"}
			}
			spoken = append(spoken, "04", "05")
		} else {
			spoken = append(spoken, "03", "04", "11")
		}
		for _, step := range spoken {
			check(arena.quest(), talk+step)
		}
	}
	for level := 1; level <= 4; level++ {
		for _, race := range []string{"CH", "EU"} {
			check("title", "UIO_SECOND_HWAN_"+race+"_NAME_LEVEL_"+string(rune('0'+level)))
		}
	}
	check("scroll refusal", "UIIT_MSG_HWANITEM_CANNOT_USE")
	check("tool refusal", cannotUseQuestTool)
	check("no target", noSelectedTarget)
}
