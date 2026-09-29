package enterworld

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"os"
	"path/filepath"
	"testing"
)

// The SP-cost cell is the THIRD column of a leveldata row. Everything
// about mastery-training authority rests on that index, so it is pinned
// here against a synthetic table with distinguishable columns.
func TestTextdataLevelsReadsTheSkillPointColumn(t *testing.T) {
	dir := t.TempDir()
	// level \t exp \t spCost \t ... - the exp column carries values that
	// would be obviously wrong if the loader read the wrong cell.
	table := "//header row is skipped\n" +
		"1\t118\t1\t0\t0\n" +
		"4\t1880\t2\t0\t0\n" +
		"6\t5640\t4\t0\t0\n" +
		"10\t23500\t9\t0\t0\n"
	if err := os.WriteFile(filepath.Join(dir, "leveldata.txt"), []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}

	levels := NewTextdataLevels(dir)
	for _, testCase := range []struct {
		level int64
		cost  int64
	}{{1, 1}, {4, 2}, {6, 4}, {10, 9}} {
		cost, ok := levels.SkillPointCost(testCase.level)
		if !ok {
			t.Fatalf("level %d has no cost row", testCase.level)
		}
		if cost != testCase.cost {
			t.Fatalf("level %d cost = %d, want %d (column index %d)", testCase.level, cost, testCase.cost, leveldataSkillPointColumn)
		}
	}
	if _, ok := levels.SkillPointCost(7); ok {
		t.Fatal("an absent level must report ok=false so the caller refuses instead of training free")
	}
	if got := levels.Len(); got != 4 {
		t.Fatalf("loaded %d rows, want 4", got)
	}
}

// The exp-requirement cell is the SECOND column (the 0x30D2 level walk's
// sub_7e0f20 row +0x08 u64). The synthetic table's columns carry values
// that cannot coincide, so a loader reading the wrong cell fails here.
func TestTextdataLevelsReadsTheExpColumn(t *testing.T) {
	dir := t.TempDir()
	table := "//header row is skipped\n" +
		"1\t118\t1\t0\t0\n" +
		"4\t1880\t2\t0\t0\n" +
		// A row past the u32 ceiling: the shipped table's level-140 exp
		// is 34900085783 and must survive the int64 path.
		"140\t34900085783\t100247\t0\t0\n"
	if err := os.WriteFile(filepath.Join(dir, "leveldata.txt"), []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}

	levels := NewTextdataLevels(dir)
	for _, testCase := range []struct {
		level int64
		exp   int64
	}{{1, 118}, {4, 1880}, {140, 34900085783}} {
		exp, ok := levels.ExpRequired(testCase.level)
		if !ok {
			t.Fatalf("level %d has no exp row", testCase.level)
		}
		if exp != testCase.exp {
			t.Fatalf("level %d exp = %d, want %d (column index %d)", testCase.level, exp, testCase.exp, leveldataExpColumn)
		}
	}
	if _, ok := levels.ExpRequired(7); ok {
		t.Fatal("an absent level must report ok=false so the exp grant refuses instead of walking blind")
	}
}

// CRefLevel +0x1c is GUST_Mob_Exp: the v1.188 monster-SEXP divisor and
// ordinary-death ceiling basis. Keep it independent from the EXP and SP-cost
// columns because both consumers otherwise produce plausible wrong values.
func TestTextdataLevelsReadsTheMonsterExpBasisColumn(t *testing.T) {
	dir := t.TempDir()
	table := "// level exp sp unused unused mobExpBasis\n" +
		"11\t34898\t12\t7001\t7002\t259\n" +
		"50\t10857676\t337\t8001\t8002\t2029\n"
	if err := os.WriteFile(filepath.Join(dir, "leveldata.txt"), []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}

	levels := NewTextdataLevels(dir)
	for _, testCase := range []struct {
		level int64
		basis int64
	}{{11, 259}, {50, 2029}} {
		basis, ok := levels.MonsterExpBasis(testCase.level)
		if !ok || basis != testCase.basis {
			t.Fatalf("level %d monster EXP basis = %d/%v, want %d (column index %d)",
				testCase.level, basis, ok, testCase.basis, leveldataMonsterExpBasisColumn)
		}
	}
}

// A missing table degrades to empty (the server still boots) and every
// lookup misses, which the training gate turns into a refusal.
func TestTextdataLevelsDegradesWhenAbsent(t *testing.T) {
	levels := NewTextdataLevels(filepath.Join(t.TempDir(), "missing-textdata"))

	if got := levels.Len(); got != 0 {
		t.Fatalf("loaded %d rows from a missing dir, want 0", got)
	}
	if _, ok := levels.SkillPointCost(2); ok {
		t.Fatal("a missing table must miss every level")
	}
	if _, ok := levels.ExpRequired(2); ok {
		t.Fatal("a missing table must miss every exp row too")
	}
	if _, ok := levels.MonsterExpBasis(2); ok {
		t.Fatal("a missing table must miss every monster-EXP-basis row too")
	}
}

// Canary against the REAL shipped textdata: if a media re-extraction ever
// shifts the SP-cost column, mastery training would silently reprice, so
// the known head of the curve is asserted here.
func TestTextdataLevelsMatchesShippedCurve(t *testing.T) {
	t.Parallel()
	dir := gamedatatest.TextdataDir(t)

	levels := NewTextdataLevels(dir)
	for _, testCase := range []struct {
		level int64
		cost  int64
	}{{1, 1}, {4, 2}, {6, 4}, {10, 9}} {
		cost, ok := levels.SkillPointCost(testCase.level)
		if !ok || cost != testCase.cost {
			t.Fatalf("shipped level %d cost = %d/%v, want %d (the SP column moved?)", testCase.level, cost, ok, testCase.cost)
		}
	}
	// The exp curve's head plus the level-cap boundary rows the level-up
	// core walks (a shifted exp column would silently re-curve every
	// level-up).
	for _, testCase := range []struct {
		level int64
		exp   int64
	}{{1, 118}, {2, 470}, {3, 1058}, {89, 265353867}, {90, 281672373}, {140, 34900085783}} {
		exp, ok := levels.ExpRequired(testCase.level)
		if !ok || exp != testCase.exp {
			t.Fatalf("shipped level %d exp = %d/%v, want %d (the exp column moved?)", testCase.level, exp, ok, testCase.exp)
		}
	}
	for _, testCase := range []struct {
		level int64
		basis int64
	}{{11, 259}, {50, 2029}, {90, 6949}} {
		basis, ok := levels.MonsterExpBasis(testCase.level)
		if !ok || basis != testCase.basis {
			t.Fatalf("shipped level %d monster EXP basis = %d/%v, want %d (the basis column moved?)",
				testCase.level, basis, ok, testCase.basis)
		}
	}
}
