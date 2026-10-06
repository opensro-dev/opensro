/*
===========================================================================

skilldata_test.go - skill reference admission and native timing normalization

===========================================================================
*/
package enterworld

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// syntheticSkillRow builds one 118-column tab row with poison values in
// every cell the loader must NOT read (so a shifted column index fails
// loudly), and the given values in the pinned learn-plane cells.
/*
================
syntheticSkillRow
================
*/
func syntheticSkillRow(cells map[int]string) string {
	fields := make([]string, 118)
	for i := range fields {
		// 900+i: a value no assertion below expects, unique per column.
		fields[i] = strconv.Itoa(900 + i)
	}
	fields[0] = "1"
	for index, value := range cells {
		fields[index] = value
	}
	row := fields[0]
	for _, field := range fields[1:] {
		row += "\t" + field
	}
	return row
}

// The learn plane rests on these column indices; they are pinned here
// against a synthetic shard whose every other cell is a poison value.
/*
================
TestTextdataSkillsReadsThePinnedColumns
================
*/
func TestTextdataSkillsReadsThePinnedColumns(t *testing.T) {
	dir := t.TempDir()
	// The index file names the shard, like the shipped skilldata.txt.
	if err := os.WriteFile(filepath.Join(dir, "skilldata.txt"), []byte("shard_a.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The indices are deliberately LITERAL (not the loader's constants):
	// this test must fail if a constant ever drifts off the pinned map
	// (1 id, 2 group, 7 level, 9 chain link, 12 casting time,
	// 13 action duration,
	// 14 reuse/cooldown, 34/35 req masteries, 36/37
	// their levels, 38/39 reqStr/Int, 40..42 prereq groups, 43..45 their
	// levels, 46 SP).
	shard := syntheticSkillRow(map[int]string{
		1: "4", 2: "175", 3: "SYN_ROOT_01", 7: "1", 9: "9001", 11: "50", 12: "175", 13: "1200", 14: "1350", 18: "33554432",
		34: "257", 35: "0",
		36: "27", 37: "0",
		38: "0", 39: "0",
		40: "174", 41: "0", 42: "0",
		43: "9", 44: "0", 45: "0",
		46: "117", 75: "1851946342",
	}) + "\n" + syntheticSkillRow(map[int]string{
		1: "9001", 2: "42", 3: "SYN_SUB_01", 7: "3", 9: "0",
		34: "0", 35: "514",
		36: "0", 37: "11",
		38: "21", 39: "22",
		40: "0", 41: "7", 42: "8",
		43: "0", 44: "5", 45: "6",
		46: "0",
	}) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "shard_a.txt"), []byte(shard), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := NewTextdataSkills(dir)
	if err := skills.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	row, ok := skills.SkillByID(4)
	if !ok {
		t.Fatal("row 4 did not load through the index")
	}
	if row.CoolTimeGroup != 2 {
		t.Fatalf("cool-time group = %d, want column-18 high byte 2", row.CoolTimeGroup)
	}
	if row.Group != 175 || row.Level != 1 || row.SPCost != 117 {
		t.Fatalf("row 4 = %+v, want grp 175 lvl 1 sp 117 (a column moved?)", row)
	}
	if !row.ActionCastingTimePinned || row.ActionCastingTimeMs != 225 ||
		!row.ActionDurationPinned || row.ActionDurationMs != 1200 ||
		!row.TimingPinned || row.CoolTimeMs != 1350 {
		t.Fatalf("row 4 action timing = cast %d/%v duration %d/%v reuse %d/%v, want 50+175+1200/1350 pinned",
			row.ActionCastingTimeMs, row.ActionCastingTimePinned,
			row.ActionDurationMs, row.ActionDurationPinned, row.CoolTimeMs, row.TimingPinned)
	}
	if lifecycleMs, pinned := row.ActionLifecycleMs(); !pinned || lifecycleMs != 1425 {
		t.Fatalf("row 4 action lifecycle = %d/%v, want 1425ms pinned", lifecycleMs, pinned)
	}
	if row.Masteries[0] != (SkillRequirement{ID: 257, Level: 27}) || row.Masteries[1] != (SkillRequirement{}) {
		t.Fatalf("row 4 masteries = %+v, want 257@27 + none", row.Masteries)
	}
	if row.Prerequisites[0] != (SkillRequirement{ID: 174, Level: 9}) {
		t.Fatalf("row 4 prerequisites = %+v, want 174@9 first", row.Prerequisites)
	}
	// Col 9 is the chain link, and being LINKED TO is what marks a row as
	// a chain sub-row - the pointer itself stays learnable.
	if row.ChainNext != 9001 || row.ChainSub {
		t.Fatalf("row 4 chain = next %d sub %v, want next 9001, not a sub-row", row.ChainNext, row.ChainSub)
	}
	if !row.VoluntaryCancelBlocked {
		t.Fatal("the encoded nbuf marker did not arm non-forced effect-stop protection")
	}

	row, ok = skills.SkillByID(9001)
	if !ok {
		t.Fatal("row 9001 did not load")
	}
	if row.Level != 3 || row.ReqStr != 21 || row.ReqInt != 22 {
		t.Fatalf("row 9001 = %+v, want lvl 3 reqStr 21 reqInt 22", row)
	}
	if row.ChainNext != 0 || !row.ChainSub {
		t.Fatalf("row 9001 chain = next %d sub %v, want a linked-to sub-row with no onward link", row.ChainNext, row.ChainSub)
	}
	if row.VoluntaryCancelBlocked {
		t.Fatal("a row without nbuf inherited another skill's cancellation protection")
	}
	if row.Masteries[1] != (SkillRequirement{ID: 514, Level: 11}) {
		t.Fatalf("row 9001 mastery slot 2 = %+v, want 514@11", row.Masteries[1])
	}
	if row.Prerequisites[1] != (SkillRequirement{ID: 7, Level: 5}) || row.Prerequisites[2] != (SkillRequirement{ID: 8, Level: 6}) {
		t.Fatalf("row 9001 prerequisites = %+v, want slots 2/3 filled", row.Prerequisites)
	}

	if _, ok := skills.SkillByID(999); ok {
		t.Fatal("an absent id must report ok=false so the caller refuses instead of learning free")
	}
	if got := skills.Len(); got != 2 {
		t.Fatalf("loaded %d rows, want 2", got)
	}

	// Col 3 is the codename, the version-stable key the creation seed
	// resolves by.
	byName, ok := skills.SkillByCodename("SYN_ROOT_01")
	if !ok || byName.ID != 4 {
		t.Fatalf("SkillByCodename(SYN_ROOT_01) = %+v/%v, want row 4 (col 3 moved?)", byName, ok)
	}
	if row.Codename != "SYN_SUB_01" {
		t.Fatalf("row 9001 codename = %q, want SYN_SUB_01", row.Codename)
	}
	if _, ok := skills.SkillByCodename("SYN_MISSING_01"); ok {
		t.Fatal("an absent codename must report ok=false so the seed refuses instead of seeding short")
	}
}

// A missing table remains fail-closed for direct lookups, while Load exposes
// the readiness failure to the production composition root.
/*
================
TestTextdataSkillsDegradesWhenAbsent
================
*/
func TestTextdataSkillsDegradesWhenAbsent(t *testing.T) {
	skills := NewTextdataSkills(filepath.Join(t.TempDir(), "missing-textdata"))

	if err := skills.Load(); err == nil {
		t.Fatal("Load accepted a missing skilldata projection")
	}
	if got := skills.Len(); got != 0 {
		t.Fatalf("loaded %d rows from a missing dir, want 0", got)
	}
	if _, ok := skills.SkillByID(1); ok {
		t.Fatal("a missing table must miss every id")
	}
}

/*
================
TestTextdataSkillsKeepsMalformedMultiImpactRowsOutOfCombatWithoutDroppingCatalogData
================
*/
func TestTextdataSkillsKeepsMalformedMultiImpactRowsOutOfCombatWithoutDroppingCatalogData(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skilldata.txt"), []byte("shard_a.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rowText := syntheticSkillRow(map[int]string{
		1:  "60000",
		2:  "600",
		3:  "SYN_MALFORMED_CM_01",
		7:  "1",
		12: "0",
		13: "1200",
		14: "1200",
		22: "1",
		50: "2",
		51: "3",
		69: "6386804", // little-endian "att"
		70: "5",
		71: "60",
		72: "0",
		73: "0",
		74: "60",
		75: "28003", // little-endian "cm"
		76: "1",     // retail only accepts kind 2
		77: "2",
	}) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "shard_a.txt"), []byte(rowText), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := NewTextdataSkills(dir)
	row, ok := skills.SkillByID(60000)
	if !ok || row.Codename != "SYN_MALFORMED_CM_01" {
		t.Fatalf("malformed-cm catalog row = %+v/%v, want retained row", row, ok)
	}
	if row.CombatPinned || row.Attack.Present || row.Attack.ImpactCount != 0 {
		t.Fatalf("malformed-cm combat row = %+v, want combat disabled", row)
	}
	byName, ok := skills.SkillByCodename("SYN_MALFORMED_CM_01")
	if !ok || byName.ID != row.ID {
		t.Fatalf("malformed-cm codename lookup = %+v/%v, want retained catalog row", byName, ok)
	}
}

// Canary against the REAL shipped textdata: if a media re-extraction ever
// shifts the requirement/cost columns, the learn gates would silently
// enforce garbage, so known shipped rows are asserted here (the values
// were read straight off skilldata_5000.txt).
/*
================
TestTextdataSkillsMatchesShippedRows
================
*/
func TestTextdataSkillsMatchesShippedRows(t *testing.T) {
	t.Parallel()
	skills := sharedShippedSkills(t)

	// SKILL_PUNCH_01: the requirement-free base row.
	punch, ok := skills.SkillByID(1)
	if !ok || punch.Group != 172 || punch.Level != 1 || punch.SPCost != 0 {
		t.Fatalf("shipped row 1 = %+v/%v, want grp 172 lvl 1 sp 0", punch, ok)
	}
	if punch.Masteries[0].ID != 0 || punch.Prerequisites[0].ID != 0 {
		t.Fatalf("shipped row 1 carries requirements: %+v", punch)
	}
	if punch.ChainNext != 0 || punch.ChainSub {
		t.Fatalf("shipped row 1 chain = %d/%v, want none (not a chain skill)", punch.ChainNext, punch.ChainSub)
	}
	if !punch.CombatPinned || !punch.TargetRequired ||
		punch.RequiredWeaponKinds != [2]uint8{1, 255} ||
		punch.Attack != (SkillAttack{
			Present:     true,
			Flags:       5,
			Percent:     150,
			Value5:      150,
			ImpactCount: 1,
		}) {
		t.Fatalf("shipped punch combat tail moved: %+v", punch)
	}

	sword, ok := skills.SkillByID(2)
	if !ok || !sword.CombatPinned || sword.Codename != "SKILL_CH_SWORD_BASE_01" ||
		!sword.ActionCastingTimePinned || sword.ActionCastingTimeMs != 0 ||
		!sword.ActionDurationPinned || sword.ActionDurationMs != 1200 ||
		!sword.TimingPinned || sword.CoolTimeMs != 1200 ||
		!sword.TargetRequired ||
		sword.RequiredWeaponKinds != [2]uint8{2, 3} ||
		sword.Attack != (SkillAttack{
			MasteryEnhancement: true, MasteryIDs: [2]uint32{257, 0},
			Present:     true,
			Flags:       5,
			Percent:     60,
			Value5:      60,
			ImpactCount: 2,
		}) {
		t.Fatalf("shipped sword base combat tail moved: %+v/%v", sword, ok)
	}

	// Monster attacks use the same native cm contract. Attack01 is a
	// one-impact action; Attack02 is the Mangyang's two-impact action. This
	// pins the shared loader so player and NPC combat cannot drift apart.
	for codename, want := range map[string]struct {
		impacts         uint8
		castingTime     uint32
		actionDuration  uint32
		actionLifecycle uint64
	}{
		"MSKILL_CH_MANGNYANG_ATTACK01":  {impacts: 1, castingTime: 0, actionDuration: 2400, actionLifecycle: 2400},
		"MSKILL_CH_MANGNYANG_ATTACK02":  {impacts: 2, castingTime: 0, actionDuration: 2500, actionLifecycle: 2500},
		"MSKILL_EU_MOVOI_CLON_ATTACK01": {impacts: 1, castingTime: 1077, actionDuration: 923, actionLifecycle: 2000},
		"MSKILL_EU_MOVOI_CLON_ATTACK02": {impacts: 1, castingTime: 1394, actionDuration: 606, actionLifecycle: 2000},
	} {
		row, ok := skills.SkillByCodename(codename)
		lifecycleMs, lifecyclePinned := row.ActionLifecycleMs()
		if !ok || !row.CombatPinned || row.Attack.ImpactCount != want.impacts ||
			!row.ActionCastingTimePinned || row.ActionCastingTimeMs != want.castingTime ||
			!row.ActionDurationPinned || row.ActionDurationMs != want.actionDuration ||
			!lifecyclePinned || lifecycleMs != want.actionLifecycle {
			t.Fatalf(
				"shipped %s action contract = %+v/%v, want %d impact(s), %d+%dms phases, %dms lifecycle",
				codename, row, ok, want.impacts, want.castingTime, want.actionDuration, want.actionLifecycle,
			)
		}
	}

	// SKILL_CH_SWORD_CHAIN_A_1S/2S/3S_01 (group 177 level 1): the chain
	// walk 6 -> 7 -> 8 -> 0, with only the ROOT learnable. Values read
	// straight off skilldata_5000.txt col 9.
	chain := map[uint32]struct {
		next uint32
		sub  bool
	}{
		6: {next: 7, sub: false},
		7: {next: 8, sub: true},
		8: {next: 0, sub: true},
	}
	for id, want := range chain {
		row, ok := skills.SkillByID(id)
		if !ok {
			t.Fatalf("shipped chain row %d missing", id)
		}
		if row.Group != 177 || row.Level != 1 {
			t.Fatalf("shipped row %d = grp %d lvl %d, want grp 177 lvl 1", id, row.Group, row.Level)
		}
		if row.ChainNext != want.next || row.ChainSub != want.sub {
			t.Fatalf("shipped row %d chain = next %d sub %v, want next %d sub %v (col 9 moved?)",
				id, row.ChainNext, row.ChainSub, want.next, want.sub)
		}
	}

	// SKILL_CH_SWORD_SMASH_B_01: every gate populated.
	smashB, ok := skills.SkillByID(4)
	if !ok {
		t.Fatal("shipped row 4 missing")
	}
	if smashB.Group != 175 || smashB.Level != 1 || smashB.SPCost != 117 {
		t.Fatalf("shipped row 4 = %+v, want grp 175 lvl 1 sp 117 (the columns moved?)", smashB)
	}
	if smashB.Masteries[0] != (SkillRequirement{ID: 257, Level: 27}) {
		t.Fatalf("shipped row 4 mastery = %+v, want 257@27", smashB.Masteries[0])
	}
	if smashB.Prerequisites[0] != (SkillRequirement{ID: 174, Level: 9}) {
		t.Fatalf("shipped row 4 prerequisite = %+v, want group 174@9", smashB.Prerequisites[0])
	}
	if smashB.ReqStr != 0 || smashB.ReqInt != 0 {
		t.Fatalf("shipped row 4 STR/INT = %d/%d, want 0/0 (all shipped rows carry 0)", smashB.ReqStr, smashB.ReqInt)
	}
}
