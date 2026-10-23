package monster

import "testing"

func TestSummonDamageGateAndBothNativeSelectors(t *testing.T) {
	a := []SummonSkill{{HPPercent: 80}, {HPPercent: 60}, {HPPercent: 40}, {HPPercent: 0}}
	i := Instance{Ref: MonsterRef{Codename: "MOB_CH_TIGERWOMAN", MaxHP: 1000}, CurrentHP: 900, DamageSinceSummon: 99}
	if _, ok := SelectSummon(i, a, 0); ok {
		t.Fatal("summoned before 10% damage")
	}
	i.DamageSinceSummon = 100
	for _, c := range []struct {
		hp    uint32
		index int
	}{{900, 0}, {800, 0}, {799, 1}, {600, 1}, {599, 2}, {400, 2}, {399, 3}, {200, 3}, {1, 3}} {
		i.CurrentHP = c.hp
		index, ok := SelectSummon(i, a, 0)
		if !ok || index != c.index {
			t.Fatalf("A hp %d = %d/%v", c.hp, index, ok)
		}
	}
	i.CurrentHP = 0
	if SummonDue(i) {
		t.Fatal("corpse summoned")
	}
	i.CurrentHP = 900
	i.SummonActionUntilMs = 1
	if SummonDue(i) {
		t.Fatal("accepted action summoned twice")
	}
	i.SummonActionUntilMs = 0
	i.Ref.Codename = "MOB_KK_ISYUTARU"
	b := make([]SummonSkill, 7)
	for _, c := range []struct {
		hp     uint32
		lo, hi int
	}{{900, 3, 3}, {799, 0, 4}, {599, 1, 5}, {399, 2, 6}, {1, 2, 6}} {
		i.CurrentHP = c.hp
		for _, roll := range []float64{0, .499999, .5, .99999} {
			want := c.lo
			if uint32(roll*32768)%1000 >= 500 {
				want = c.hi
			}
			index, ok := SelectSummon(i, b, roll)
			if !ok || index != want {
				t.Fatalf("B hp %d roll %f = %d/%v want %d", c.hp, roll, index, ok, want)
			}
		}
	}
}

func TestSummonAFirstAuthoredBandAndMissingBand(t *testing.T) {
	i := Instance{Ref: MonsterRef{Codename: "MOB_EU_KERBEROS", MaxHP: 1000}, CurrentHP: 1, DamageSinceSummon: 100}
	rows := []SummonSkill{{HPPercent: 80}, {HPPercent: 60}, {HPPercent: 40}, {HPPercent: 20}, {HPPercent: 0}, {HPPercent: 20}}
	if index, ok := SelectSummon(i, rows, 0); !ok || index != 3 {
		t.Fatalf("first row per band not preserved: %d/%v", index, ok)
	}
	if _, ok := SelectSummon(i, rows[:3], 0); ok {
		t.Fatal("missing low-health band manufactured a wave")
	}
	i.Ref.Codename = "UNKNOWN"
	if SummonDue(i) {
		t.Fatal("unbound policy guessed")
	}
}

func TestSummonDefaultTacticsNormalChampionAndOriginalFallback(t *testing.T) {
	// One normal and one champion row; the sight adds the body radius.
	table := SummonTacticsTable{"MOB_CH_MANGNYANG": {{{SightRange: 115}}, {{SightRange: 130}}}}
	ref := MonsterRef{Codename: "MOB_CH_MANGNYANG", BodyRadius: 6}
	draws := 0
	random := func() float64 { draws++; return 0 }
	if radius, ok := table.SummonedSightRange(ref, 0, random); !ok || radius != 121 {
		t.Fatalf("normal sight=%v/%v", radius, ok)
	}
	if radius, ok := table.SummonedSightRange(ref, 6, random); !ok || radius != 136 {
		t.Fatalf("champion sight=%v/%v", radius, ok)
	}
	if draws != 0 {
		t.Fatal("singleton tactics consumed extra entropy")
	}
	ref.OriginalCodename = ref.Codename
	ref.Codename = "UNPUBLISHED_VARIANT"
	if radius, ok := table.SummonedSightRange(ref, 6, random); !ok || radius != 136 {
		t.Fatal("original reference fallback lost")
	}
	ref.OriginalCodename = "UNKNOWN"
	if _, ok := table.SummonedSightRange(ref, 0, random); ok {
		t.Fatal("invented missing native tactics")
	}
}
