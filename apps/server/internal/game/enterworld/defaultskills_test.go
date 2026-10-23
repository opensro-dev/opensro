package enterworld

import (
	"strings"
	"testing"
)

// fakeSeedSkills is a SkillDataSource + SkillCodenameSource over a fixed
// row set (keyed by codename; ids resolve through the same rows).
type fakeSeedSkills map[string]SkillRow

func (f fakeSeedSkills) SkillByCodename(codename string) (SkillRow, bool) {
	row, ok := f[codename]
	return row, ok
}

func (f fakeSeedSkills) SkillByID(id uint32) (SkillRow, bool) {
	for _, row := range f {
		if row.ID == id {
			return row, true
		}
	}
	return SkillRow{}, false
}

// idOnlySkills implements ONLY SkillByID - the seeder must refuse it
// rather than silently seed nothing.
type idOnlySkills struct{}

func (idOnlySkills) SkillByID(id uint32) (SkillRow, bool) { return SkillRow{}, false }

// fakeChSeed is the Chinese seed as the shipped table carries it.
func fakeChSeed() fakeSeedSkills {
	return fakeSeedSkills{
		"SKILL_PUNCH_01":         {ID: 1, Codename: "SKILL_PUNCH_01", Group: 172, Level: 1},
		"SKILL_CH_SWORD_BASE_01": {ID: 2, Codename: "SKILL_CH_SWORD_BASE_01", Group: 173, Level: 1},
		"SKILL_CH_SPEAR_BASE_01": {ID: 40, Codename: "SKILL_CH_SPEAR_BASE_01", Group: 195, Level: 1},
		"SKILL_CH_BOW_BASE_01":   {ID: 70, Codename: "SKILL_CH_BOW_BASE_01", Group: 217, Level: 1},
	}
}

// The creation seed against the REAL shipped table: the codenames must
// resolve to exactly the v1.150-native id set (lane6 notes 3.3), every
// row a level-1 / 0-SP / non-chain-sub ladder root. The two races'
// expected id sets are structurally different (4 rows vs 10, disjoint
// except the shared punch), so a race swap CANNOT pass this test by
// coincidence.
func TestDefaultSkillRowsResolveTheShippedSeed(t *testing.T) {
	t.Parallel()
	skills := sharedShippedSkills(t)

	expect := map[string][]uint32{
		RaceKeyChina:  {1, 2, 40, 70},
		RaceKeyEurope: {1, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970},
	}
	for raceKey, wantIDs := range expect {
		rows, err := DefaultSkillRows(skills, raceKey)
		if err != nil {
			t.Fatalf("%s seed failed to resolve: %v", raceKey, err)
		}
		if len(rows) != len(wantIDs) {
			t.Fatalf("%s seed = %d rows, want %d", raceKey, len(rows), len(wantIDs))
		}
		for i, row := range rows {
			if row.ID != wantIDs[i] {
				t.Fatalf("%s seed[%d] = id %d (%s), want id %d", raceKey, i, row.ID, row.Codename, wantIDs[i])
			}
			if row.Level != 1 || row.SPCost != 0 || row.ChainSub {
				t.Fatalf("%s seed row %s = lvl %d sp %d chainSub %v, want a level-1 0-SP root", raceKey, row.Codename, row.Level, row.SPCost, row.ChainSub)
			}
		}
	}

	// The race-swap witness: beyond sharing punch, the two sets must not
	// overlap at all - same-number-different-rules can never satisfy
	// both branches above.
	chSet := map[uint32]bool{}
	for _, id := range expect[RaceKeyChina] {
		chSet[id] = true
	}
	for _, id := range expect[RaceKeyEurope][1:] {
		if chSet[id] {
			t.Fatalf("expected id sets overlap at %d beyond punch - the test inputs can coincide", id)
		}
	}
}

// FAIL LOUD half 1: a codename the table does not carry refuses the WHOLE
// seed and names the codename - never a silently short list.
func TestDefaultSkillRowsRefuseAMissingCodename(t *testing.T) {
	broken := fakeChSeed()
	delete(broken, "SKILL_CH_SPEAR_BASE_01")

	rows, err := DefaultSkillRows(broken, RaceKeyChina)
	if err == nil {
		t.Fatalf("a missing codename resolved to %d rows instead of refusing", len(rows))
	}
	if !strings.Contains(err.Error(), "SKILL_CH_SPEAR_BASE_01") {
		t.Fatalf("the refusal does not name the missing codename: %v", err)
	}
}

// FAIL LOUD half 2: a codename that resolves to the wrong KIND of row
// (the cross-version contamination shape: chain sub, wrong level, priced)
// refuses rather than seeding it.
func TestDefaultSkillRowsRefuseANonRootRow(t *testing.T) {
	for name, mutate := range map[string]func(row SkillRow) SkillRow{
		"wrongLevel": func(row SkillRow) SkillRow { row.Level = 7; return row },
		"pricedRow":  func(row SkillRow) SkillRow { row.SPCost = 468; return row },
		"chainSub":   func(row SkillRow) SkillRow { row.ChainSub = true; return row },
	} {
		t.Run(name, func(t *testing.T) {
			broken := fakeChSeed()
			broken["SKILL_CH_BOW_BASE_01"] = mutate(broken["SKILL_CH_BOW_BASE_01"])

			rows, err := DefaultSkillRows(broken, RaceKeyChina)
			if err == nil {
				t.Fatalf("a non-root row seeded (%d rows) instead of refusing", len(rows))
			}
			if !strings.Contains(err.Error(), "SKILL_CH_BOW_BASE_01") {
				t.Fatalf("the refusal does not name the offending codename: %v", err)
			}
		})
	}
}

// The seeder's idempotence contract: only MISSING ids come back, in seed
// order, so a character who already 0-SP-learned some bases gets no
// duplicate and a fresh character gets the whole racial set.
func TestDefaultSkillSeederAddsOnlyMissingIDs(t *testing.T) {
	seeder := DefaultSkillSeeder(fakeChSeed())

	fresh, err := seeder(RaceKeyChina, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) != 4 || fresh[0] != 1 || fresh[1] != 2 || fresh[2] != 40 || fresh[3] != 70 {
		t.Fatalf("fresh seed = %v, want [1 2 40 70]", fresh)
	}

	partial, err := seeder(RaceKeyChina, []uint32{40, 1, 31337})
	if err != nil {
		t.Fatal(err)
	}
	if len(partial) != 2 || partial[0] != 2 || partial[1] != 70 {
		t.Fatalf("partial backfill = %v, want [2 70] (learned bases must not duplicate)", partial)
	}

	again, err := seeder(RaceKeyChina, append([]uint32{31337}, fresh...))
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("re-running the seeder added %v - the backfill is not idempotent", again)
	}
}

// A source without codename lookup must refuse every call - seeding
// NOTHING silently would be the short-list failure wearing a different
// face.
func TestDefaultSkillSeederRequiresCodenameLookup(t *testing.T) {
	seeder := DefaultSkillSeeder(idOnlySkills{})
	if _, err := seeder(RaceKeyChina, nil); err == nil {
		t.Fatal("an id-only source seeded instead of refusing")
	}
}

func TestDefaultSkillSeederRefusesTypedNilSource(t *testing.T) {
	var skills *TextdataSkills
	seeder := DefaultSkillSeeder(skills)
	if _, err := seeder(RaceKeyChina, nil); err == nil {
		t.Fatal("typed-nil skilldata source was accepted")
	}
}
