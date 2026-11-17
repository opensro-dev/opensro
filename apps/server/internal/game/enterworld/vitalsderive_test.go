package enterworld

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/domain/charactervitals"
)

// The published retail worked examples are the ground truth for the
// derivation (lane6 notes 2.3: period observations of LIVE retail that
// reproduce to the digit). Every case here is a real observed character,
// not a synthetic input - a formula change that still passes these is by
// definition observationally equivalent to retail.
func TestDerivedVitalsReproduceTheRetailObservations(t *testing.T) {
	cases := []struct {
		name        string
		level, stat int64
		want        int64
	}{
		// Fresh character: the vendor _AddNewChar INSERT (STR/INT 20,
		// HP/MP 200) - the formula at level 1.
		{"creation level 1 stat 20", 1, 20, 200},
		// Level 1->2 with no points spent: observed 214 HP - the case
		// that arithmetically proves the auto +1 STR per level (STR 20
		// would read 204).
		{"level 2 STR 21 (auto-growth witness)", 2, 21, 214},
		// The same character after +3 manual points into STR.
		{"level 2 STR 24", 2, 24, 244},
		// S13's level-43 character: 1.02^42*1730 = 3974.23 -> 3974 and
		// 1.02^42*790 = 1814.82 -> 1814. BOTH fractions are below .5, so
		// they also witness trunc against ceil-like drift.
		{"level 43 STR 173 HP", 43, 173, 3974},
		{"level 43 INT 79 MP", 43, 79, 1814},
	}
	for _, tc := range cases {
		if got := derivedVitalMax(tc.level, tc.stat); got != tc.want {
			t.Errorf("%s: derived %d, want the retail-observed %d", tc.name, got, tc.want)
		}
	}
}

// trunc, NOT round: 1.02^1 * 33 * 10 = 336.6 truncates to 336 where
// rounding would say 337. The retail fixtures above cannot catch a
// round() substitution alone (their fractions all fall below .5), so this
// input is chosen to be unable to coincide.
func TestDerivedVitalsTruncateNotRound(t *testing.T) {
	if got := derivedVitalMax(2, 33); got != 336 {
		t.Fatalf("derived %d, want 336 (336.6 must truncate; 337 means round crept in)", got)
	}
}

// The user-visible payoff arithmetic: one +STR at level 2 moves the
// maximum 214 -> 224 (1.02 * 220 = 224.4). The progression plane re-encodes
// the 0x343C block after the spend, so this delta is exactly what the
// player's gauge shows.
func TestDerivedVitalsMoveOnAStatPoint(t *testing.T) {
	before := derivedVitalMax(2, 21)
	after := derivedVitalMax(2, 22)
	if before != 214 || after != 224 {
		t.Fatalf("stat point moved the max %d -> %d, want 214 -> 224", before, after)
	}
}

// THE CROSS-PIN (COORD seq-31 mandate): the creation seed (LANE-A,
// BaseVitals) and the growth formula (LANE-C) encode the SAME retail fact
// in two places - a fresh character's 200/200 is the formula at level 1
// with the creation stats. This test converts that coordination agreement
// into something the compiler enforces forever: if either constant or the
// formula drifts, this fails.
func TestCreationVitalsCrossPin(t *testing.T) {
	if got := derivedVitalMax(1, BaseStat); got != BaseVitals {
		t.Fatalf("formula(level 1, BaseStat %d) = %d, want BaseVitals %d - the creation seed and the derivation have diverged", BaseStat, got, BaseVitals)
	}
	bare := &Character{Name: "fresh"}
	if got := DerivedMaxHP(bare); got != BaseVitals {
		t.Fatalf("fresh-record MaxHP = %d, want BaseVitals %d", got, BaseVitals)
	}
	if got := DerivedMaxMP(bare); got != BaseVitals {
		t.Fatalf("fresh-record MaxMP = %d, want BaseVitals %d", got, BaseVitals)
	}
}

// Character-level readers: absent fields take the same fallbacks the
// rest of the plane uses (level 1, creation-base 20/20), so a bare record
// derives exactly the retail creation vitals.
func TestDerivedMaxFallbacksMatchTheCreationShape(t *testing.T) {
	bare := &Character{Name: "bare"}
	if got := DerivedMaxHP(bare); got != 200 {
		t.Fatalf("bare-record MaxHP = %d, want the creation 200", got)
	}
	if got := DerivedMaxMP(bare); got != 200 {
		t.Fatalf("bare-record MaxMP = %d, want the creation 200", got)
	}
	if got := DerivedMaxHP(nil); got != 200 {
		t.Fatalf("nil-character MaxHP = %d, want the creation 200", got)
	}
}

// Character maxima are derived from the authoritative level and stats.
func TestDerivedMaxUsesAuthoritativeStats(t *testing.T) {
	c := &Character{
		Name:     "derivedMaxima",
		Level:    i64(2),
		Strength: i64(21), Intellect: i64(21),
	}
	if got := DerivedMaxHP(c); got != 214 {
		t.Fatalf("MaxHP = %d, want 214", got)
	}
	if got := DerivedMaxMP(c); got != 214 {
		t.Fatalf("MaxMP = %d, want 214", got)
	}
}

// Boundary discipline: the stat input clamps at the native u16 word (the
// same value the 0x343C block shows), the level clamps into [1, 140], and
// the result never leaves [1, 0x7fffffff].
func TestDerivedVitalsBoundaries(t *testing.T) {
	over := &Character{
		Name:     "over",
		Level:    i64(9000),
		Strength: i64(charactervitals.StatWordMax + 500),
	}
	// Level clamps to 140, stat to 0xffff: 1.02^139 * 655350 = 10277729.
	if got := DerivedMaxHP(over); got != 10277729 {
		t.Fatalf("clamped ceiling MaxHP = %d, want 10277729", got)
	}
	if got := derivedVitalMax(1, 0); got != 1 {
		t.Fatalf("zero stat derived %d, want the floor 1 (a 0 max empties the HUD gauge)", got)
	}
	negativeLevel := &Character{Name: "negLevel", Level: i64(-5)}
	if got := DerivedMaxHP(negativeLevel); got != 200 {
		t.Fatalf("negative level MaxHP = %d, want 200 (level floors at 1)", got)
	}
}

// The browser response presents derived maxima without adding them to the
// persisted Character aggregate.
func TestBootstrapResultPresentsDerivedMaxima(t *testing.T) {
	result := &BootstrapResult{
		NativeResult: 1,
		Character:    (&Character{Name: "snap"}).Snapshot(),
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal bootstrap result: %v", err)
	}
	var response struct {
		Character struct {
			MaxHP int64 `json:"maxHp"`
			MaxMP int64 `json:"maxMp"`
		} `json:"character"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("unmarshal bootstrap result: %v", err)
	}
	if response.Character.MaxHP != 200 || response.Character.MaxMP != 200 {
		t.Fatalf(
			"presented maxima = %d/%d, want 200/200",
			response.Character.MaxHP,
			response.Character.MaxMP,
		)
	}
}
