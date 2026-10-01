/*
===========================================================================

abnormal_test.go - abnormal rolls: bits, element levels and grades

===========================================================================
*/

package abnormal

import (
	"testing"

	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
scriptedRandom

================
*/
type scriptedRandom struct {
	calls  []string
	chance map[uint32]bool
	rand   int32
}

/*
================
Chance

================
*/
func (r *scriptedRandom) Chance(key uint32, chance int32) bool {
	r.calls = append(r.calls, "chance")
	if chance <= 0 {
		return false
	}
	return r.chance[key]
}

/*
================
Rand

================
*/
func (r *scriptedRandom) Rand() int32 {
	r.calls = append(r.calls, "rand")
	return r.rand
}

/*
================
fakeOwner

================
*/
type fakeOwner struct {
	alive, player, monster bool
	hp, maxHP, maxMP       uint32
	params                 map[uint16]float32
	block                  *Block
	now                    int64
	sourceGone, sourceDead bool
	roll                   bool
	motions                [][3]float32
	cancels                []bool
	stops, detonations     int
	ai                     [][3]uint32
	hits                   []uint32
	credited               []bool
	drains                 []int32
}

/*
================
Alive

================
*/
func (o *fakeOwner) Alive() bool { return o.alive }

/*
================
IsPlayer

================
*/
func (o *fakeOwner) IsPlayer() bool { return o.player }

/*
================
IsMonster

================
*/
func (o *fakeOwner) IsMonster() bool { return o.monster }

/*
================
CurrentHP

================
*/
func (o *fakeOwner) CurrentHP() uint32 { return o.hp }

/*
================
MaxHP

================
*/
func (o *fakeOwner) MaxHP() uint32 { return o.maxHP }

/*
================
MaxMP

================
*/
func (o *fakeOwner) MaxMP() uint32 { return o.maxMP }

/*
================
Param

================
*/
func (o *fakeOwner) Param(id uint16) float32 {
	v, err := o.block.Evaluate(id, paramkeeper.Definition{Maximum: 9999999}, o.params[id])
	if err != nil {
		panic(err)
	}
	return v
}

/*
================
SourceExists

================
*/
func (o *fakeOwner) SourceExists(uint32) bool { return !o.sourceGone }

/*
================
SourceDead

================
*/
func (o *fakeOwner) SourceDead(uint32) bool { return o.sourceDead }

/*
================
Roll

================
*/
func (o *fakeOwner) Roll(uint32, int32) bool { return o.roll }

/*
================
Now

================
*/
func (o *fakeOwner) Now() int64 { return o.now }

/*
================
ParamsChanged

================
*/
func (o *fakeOwner) ParamsChanged(bool) {}

/*
================
SetMotion

================
*/
func (o *fakeOwner) SetMotion(s, n uint8, d float32) {
	o.motions = append(o.motions, [3]float32{float32(s), float32(n), d})
}

/*
================
CancelActions

================
*/
func (o *fakeOwner) CancelActions(all bool) { o.cancels = append(o.cancels, all) }

/*
================
StopMove

================
*/
func (o *fakeOwner) StopMove() { o.stops++ }

/*
================
AIEvent

================
*/
func (o *fakeOwner) AIEvent(e, k uint8, s uint32) {
	o.ai = append(o.ai, [3]uint32{uint32(e), uint32(k), s})
}

/*
================
ConsumeResources

================
*/
func (o *fakeOwner) ConsumeResources(_, mp int32, _ uint8) { o.drains = append(o.drains, mp) }

/*
================
Detonate

================
*/
func (o *fakeOwner) Detonate(Slot) { o.detonations++ }

/*
================
Hit

================
*/
func (o *fakeOwner) Hit(_ uint32, credited bool, damage uint32, _ uint8, _ Status) {
	o.hits = append(o.hits, damage)
	o.credited = append(o.credited, credited)
}

/*
================
params

================
*/
func params(tag uint32, args ...uint32) *SkillParams {
	p := &SkillParams{}
	i, ok := SourceIndex(tag)
	if !ok {
		panic("unknown tag")
	}
	p.Params[i].Present = true
	copy(p.Params[i].Args[:], args)
	return p
}

/*
================
TestBitsSwapBurnAndShock

================
*/
func TestBitsSwapBurnAndShock(t *testing.T) {
	if Burn.Bit() != 8 || ElectricShock.Bit() != 4 || Stun.Bit() != 0x4000 || TimeBomb.Bit() != 0x1000000 {
		t.Fatal("g_adwAbnormalStatusBit mapping lost")
	}
	if Freeze.Category() != 1 || Burn.Category() != 1 || Bleeding.Category() != 2 || Dark.Category() != 2 {
		t.Fatal("category mismatch with 5AA450")
	}
}

/*
================
TestElementRollLevelAndDuration

Element statuses roll first, then scale their level by the target resist
and take a rank-multiplied duration (590680 / 410B40).
================
*/
func TestElementRollLevelAndDuration(t *testing.T) {
	random := &scriptedRandom{chance: map[uint32]bool{0x04000000: true}}
	in := RollInput{Params: params(0x6275, 10, 30, 5), TargetResist: [6]float32{3: 20}, TargetFlat: [6]float32{3: 2}}
	records := Roll(in, random)
	if len(records) != 1 {
		t.Fatalf("records %v", records)
	}
	r := records[0]
	if r.Status != Burn || r.Level != 6 || r.DurationMs != 6*750 || r.Rate24 != 14 || r.PeriodMs != 2000 || r.Scale20 != 1 || r.Chance != 30 {
		t.Fatalf("burn record %+v", r)
	}
	random.chance[0x04000000] = false
	if len(Roll(in, random)) != 0 {
		t.Fatal("failed roll admitted burn")
	}
	in.TargetResist[3] = 100
	random.chance[0x04000000] = true
	if len(Roll(in, random)) != 0 {
		t.Fatal("full resist admitted burn")
	}
}

/*
================
TestGradeRollScalesByLevelGap

Statuses 6+ scale duration by 2.5 % and chance by 5 % per level gap with
50 % and 10 % floors; disease's bonus raises the rank and the chance.
================
*/
func TestGradeRollScalesByLevelGap(t *testing.T) {
	random := &scriptedRandom{chance: map[uint32]bool{0x0f000000: true}}
	in := RollInput{Params: params(0x7374, 4000, 60, 3), TargetLevel: 40}
	records := Roll(in, random)
	if len(records) != 1 || records[0].Status != Stun || records[0].Grade != 3 || records[0].DurationMs != 3000 {
		t.Fatalf("stun %+v", records)
	}
	in.TargetLevel = 90
	if r := Roll(in, random); r[0].DurationMs != 2000 {
		t.Fatalf("duration floor %+v", r)
	}
	in.TargetBonus = 5
	in.TargetLevel = 40
	// Disease's bonus raises the rank by one, closing the ten-level gap.
	if r := Roll(in, random); r[0].DurationMs != 4000 {
		t.Fatalf("bonus rank %+v", r)
	}
}

/*
================
TestSlowGradeFromCasterLevel

Slow and stun without an authored grade take the caster level / 10.
================
*/
func TestSlowGradeFromCasterLevel(t *testing.T) {
	random := &scriptedRandom{chance: map[uint32]bool{0x09000000: true}}
	r := Roll(RollInput{Params: params(0x736c, 5000, 50, 0), CasterLevel: 47}, random)
	if len(r) != 1 || r[0].Grade != 4 {
		t.Fatalf("slow grade %+v", r)
	}
}

/*
================
TestWallContextSuppressesStunAndMagicalStatusRolls

590680's sixth argument points to the Force wall parameters. Ordinary hit
flags and the true type-two block branch are independent of that context.
================
*/
func TestWallContextSuppressesStunAndMagicalStatusRolls(t *testing.T) {
	for _, mask := range []uint32{0, 4, 8, 12} {
		for _, stun := range []bool{false, true} {
			random := &scriptedRandom{chance: map[uint32]bool{0x04000000: true, 0x0f000000: true}}
			p := params(0x6275, 10, 30, 5)
			if stun {
				i, _ := SourceIndex(0x7374)
				p.Params[i] = Param{Present: true, Args: [6]uint32{1000, 50, 1}}
			}
			want := 1
			if mask&8 != 0 && !stun {
				want = 0
			}
			got := Roll(RollInput{Params: p, WallMask: &mask}, random)
			if len(got) != want {
				t.Fatalf("wall %x stun %v: %+v", mask, stun, got)
			}
			for _, record := range got {
				if record.Status == Stun {
					t.Fatal("wall accepted stun")
				}
			}
			if stun && len(Roll(RollInput{Params: p}, random)) != 2 {
				t.Fatal("unwalled stun was suppressed")
			}
			if len(Roll(RollInput{Params: p, SkipGroup: true}, random)) != 0 {
				t.Fatal("blocked impact rolled statuses")
			}
		}
	}
}

/*
================
TestTimeBombDrawsRandBeforeRoll

The time bomb draws rand() before its probability roll and picks one of
the nonzero duration bytes.
================
*/
func TestTimeBombDrawsRandBeforeRoll(t *testing.T) {
	random := &scriptedRandom{chance: map[uint32]bool{0x19000000: true}, rand: 4}
	p := params(0x7462, 0x0000050a, 50, 2, 300)
	p.TrapDamageGetv = true
	r := Roll(RollInput{Params: p, CasterModifier: func(key uint32) (uint32, bool) { return 115, key == KeyTrapDamage }}, random)
	if len(r) != 1 || r[0].DurationMs != 10000 || r[0].Damage1C != 415 {
		t.Fatalf("time bomb %+v", r)
	}
	if len(random.calls) != 2 || random.calls[0] != "rand" || random.calls[1] != "chance" {
		t.Fatalf("call order %v", random.calls)
	}
}

/*
================
TestApplyReplacementRule

4A4270: an active slot accepts only a strictly stronger level/grade; the
refreshed flag is set even when the source then fails to resolve.
================
*/
func TestApplyReplacementRule(t *testing.T) {
	var b Block
	o := &fakeOwner{alive: true, monster: true, block: &b, hp: 100, maxHP: 100}
	if !b.Apply(o, Record{Status: Stun, Grade: 3, DurationMs: 1000}, 0) {
		t.Fatal("first stun refused")
	}
	if b.Apply(o, Record{Status: Stun, Grade: 3, DurationMs: 5000}, 10) {
		t.Fatal("equal grade extended the stun")
	}
	o.sourceGone = true
	if b.Apply(o, Record{Status: Stun, Grade: 4}, 20) || !b.Slots[Stun].Refreshed {
		t.Fatal("refreshed flag must be set before the source lookup")
	}
	o.sourceGone = false
	if !b.Apply(o, Record{Status: Stun, Grade: 4, DurationMs: 1000}, 30) || b.Slots[Stun].StartedAt != 30 {
		t.Fatal("stronger stun refused")
	}
	if b.Apply(o, Record{Status: Stun}, 40) {
		t.Fatal("zero grade admitted")
	}
	o.alive = false
	if b.Apply(o, Record{Status: Freeze, Level: 9}, 50) {
		t.Fatal("dead owner admitted a status")
	}
}

/*
================
TestUpdateExpiresAfterDuration

4A4390 expires strictly after the duration and republishes the mask.
================
*/
func TestUpdateExpiresAfterDuration(t *testing.T) {
	var b Block
	o := &fakeOwner{alive: true, monster: true, block: &b}
	b.Apply(o, Record{Status: Sleep, Grade: 1, DurationMs: 1000}, 0)
	if b.Mask != 0x40 || len(o.motions) != 1 || o.motions[0][0] != 0x13 {
		t.Fatalf("sleep start %+v %v", b.Mask, o.motions)
	}
	o.now = 1000
	if res := b.Update(o, 1000); res.Changed || !b.Has(Sleep) {
		t.Fatal("sleep ended at the exact deadline")
	}
	if res := b.Update(o, 1001); !res.Changed || res.Mask != 0 || b.Has(Sleep) || o.motions[1][0] != 0 {
		t.Fatalf("sleep did not expire %+v", res)
	}
}

/*
================
TestBurnTicks

Burn ticks immediately, then every strictly-more-than 2000 ms; its damage
divides the table value by the magical-parry factor.
================
*/
func TestBurnTicks(t *testing.T) {
	var b Block
	o := &fakeOwner{alive: true, monster: true, block: &b, hp: 1000, params: map[uint16]float32{8: 50}}
	b.Apply(o, Record{Status: Burn, Level: 4, DurationMs: 3000, Rate24: 14, Scale20: 1}, 5000)
	o.now = 5000
	b.Update(o, 5000)
	o.now = 7000
	b.Update(o, 7000)
	o.now = 7001
	b.Update(o, 7001)
	if len(o.hits) != 2 || o.hits[0] != 9 || !o.credited[0] {
		t.Fatalf("burn hits %v", o.hits)
	}
}

/*
================
TestDamageOverTimeCredit

Poison never kills and never credits a dead source; bleeding keeps credit
for a dead source only on a lethal tick.
================
*/
func TestDamageOverTimeCredit(t *testing.T) {
	var b Block
	o := &fakeOwner{alive: true, monster: true, block: &b, hp: 30, sourceDead: true}
	// Native GetTickCount is large, so the zero +68 makes the first tick due.
	const start = 1_000_000
	b.Apply(o, Record{Status: Poison, Level: 1, DurationMs: 9000, Param38: 100}, start)
	o.now = start + 1
	b.Update(o, start+1)
	if len(o.hits) != 1 || o.hits[0] != 29 || o.credited[0] {
		t.Fatalf("poison %v %v", o.hits, o.credited)
	}
	var bleed Block
	o2 := &fakeOwner{alive: true, monster: true, block: &bleed, hp: 30, sourceDead: true}
	bleed.Apply(o2, Record{Status: Bleeding, Grade: 1, DurationMs: 9000, PeriodMs: 2000, Param38: 40}, start)
	o2.now = start + 1
	bleed.Update(o2, start+1)
	if len(o2.hits) != 1 || !o2.credited[0] {
		t.Fatalf("lethal bleeding lost credit %v", o2.credited)
	}
}

/*
================
TestFrostbiteAndSlowShareMovement

Frostbite owns the movement factors; slow applies only without it and
re-applies once frostbite has ended (4A4A90 event 1 with owner 1).
================
*/
func TestFrostbiteAndSlowShareMovement(t *testing.T) {
	var b Block
	o := &fakeOwner{alive: true, monster: true, block: &b, params: map[uint16]float32{0x17: 20, 0x18: 60, 0x8c: 100}}
	b.Apply(o, Record{Status: Frostbite, Level: 2, DurationMs: 500}, 0)
	if v := o.Param(0x18); v != 30 || o.Param(0x8c) != 200 {
		t.Fatalf("frostbite %v", v)
	}
	b.Apply(o, Record{Status: Slow, Grade: 1, DurationMs: 5000}, 0)
	if o.Param(0x18) != 30 {
		t.Fatal("slow overrode frostbite")
	}
	o.now = 501
	b.Update(o, 501)
	// The block mask is recomputed after the loop, so slow still sees the
	// frostbite bit during the update that retires it.
	if o.Param(0x18) != 60 {
		t.Fatalf("slow took over in the retiring update %v", o.Param(0x18))
	}
	o.now = 502
	b.Update(o, 502)
	if o.Param(0x18) != 45 || o.Param(0x8c) != 125 || b.SpeedOwner != 8 {
		t.Fatalf("slow after frostbite %v %v", o.Param(0x18), o.Param(0x8c))
	}
}

/*
================
TestBreakOnHit

Damage frees root; a hit wakes sleep and breaks stun at 25 %, unless a
time bomb is attached.
================
*/
func TestBreakOnHit(t *testing.T) {
	var b Block
	o := &fakeOwner{alive: true, monster: true, block: &b, roll: true}
	b.Apply(o, Record{Status: Root, Grade: 1, DurationMs: 9000}, 0)
	b.Apply(o, Record{Status: Sleep, Grade: 1, DurationMs: 9000}, 0)
	b.Apply(o, Record{Status: TimeBomb, Grade: 1, DurationMs: 9000}, 0)
	b.BreakOnHit(o, HitContext{Magical: true, Attack: true})
	if b.Has(Root) || !b.Has(Sleep) {
		t.Fatal("time bomb must shield sleep but not root")
	}
	b.Clear(o, TimeBomb)
	if b.Slots[TimeBomb].Retired != true || o.detonations != 0 {
		t.Fatal("cured bomb detonated")
	}
	b.BreakOnHit(o, HitContext{Magical: true, Attack: true})
	if b.Has(Sleep) {
		t.Fatal("hit did not wake sleep")
	}
}

/*
================
TestGradesFollowClientMask

Grade bytes follow the mask in ascending bit order over 017FCFC0.
================
*/
func TestGradesFollowClientMask(t *testing.T) {
	var b Block
	o := &fakeOwner{alive: true, monster: true, block: &b}
	b.Apply(o, Record{Status: Division, Grade: 5, DurationMs: 9000}, 0)
	b.Apply(o, Record{Status: Dark, Grade: 7, DurationMs: 9000}, 0)
	b.Apply(o, Record{Status: Bleeding, Grade: 2, DurationMs: 9000}, 0)
	b.Apply(o, Record{Status: Burn, Level: 3, DurationMs: 9000}, 0)
	if g := b.Grades(); len(g) != 2 || g[0] != 2 || g[1] != 5 {
		t.Fatalf("grades %v", g)
	}
}

/*
==================
TestBurnTableAbsorptionAndDuration

Vectors carried over from the former monster.Burn port: C63C94 level
damage absorbed by magical parry (param 8), and the 750 ms-per-level
lifetime ending strictly after its deadline.
==================
*/
func TestBurnTableAbsorptionAndDuration(t *testing.T) {
	for _, c := range []struct {
		level uint32
		parry float32
		want  uint32
	}{{1, 0, 8}, {1, 1, 7}, {1, 9, 7}, {1, 100, 4}, {21, 20, 41}, {140, 0, 5533}} {
		var b Block
		o := &fakeOwner{alive: true, monster: true, block: &b, hp: 1 << 30, params: map[uint16]float32{8: c.parry}, now: 10000}
		b.Apply(o, Record{Status: Burn, Level: 30, DurationMs: durationPerLevel(Burn, 30), Rate24: burnDamage(c.level), Scale20: 1}, 10000)
		b.Update(o, 10000)
		if len(o.hits) != 1 || o.hits[0] != c.want {
			t.Fatal(c, o.hits)
		}
		o.now = 32500
		if b.Update(o, 32500); !b.Has(Burn) {
			t.Fatal("burn ended at its deadline")
		}
		o.now = 32501
		if b.Update(o, 32501); b.Has(Burn) {
			t.Fatal("burn outlived its deadline")
		}
	}
	if burnDamage(141) != 0 || burnDamage(0) != 0 {
		t.Fatal("out-of-table level produced damage")
	}
}

/*
==================
TestGradeRollBleedingVectors

Vectors carried over from the former combat.BleedingParameters port
(591A2A..591B79): level gap, disease bonus, flat resistance halved per
grade above its own grade, and percentage resistance.
==================
*/
func TestGradeRollBleedingVectors(t *testing.T) {
	for _, tc := range []struct {
		level                       uint8
		bonus, flat, grade, percent int32
		duration                    uint32
		chance                      int32
	}{
		{80, 0, 0, 0, 0, 30000, 18}, {90, 0, 0, 0, 0, 22500, 9}, {100, 0, 0, 0, 0, 15000, 1}, {255, 0, 0, 0, 0, 15000, 1},
		{90, 1, 0, 0, 0, 30000, 19}, {80, 0, 7, 6, 0, 30000, 17}, {80, 0, 7, 9, 0, 30000, 11}, {80, 0, 0, 0, 50, 30000, 9},
	} {
		var gotChance int32
		random := chanceProbe{record: &gotChance}
		in := RollInput{Params: params(0x626c, 30000, 18, 8, 156, 20), TargetLevel: tc.level, TargetBonus: float32(tc.bonus)}
		in.Resistance[5] = Resistance{Flat: tc.flat, Grade: tc.grade, Percent: tc.percent}
		r := Roll(in, random)
		if len(r) != 1 || r[0].DurationMs != tc.duration || gotChance != tc.chance || r[0].Param38 != 156 || r[0].Param40 != 20 {
			t.Fatalf("%+v: %+v chance=%d", tc, r, gotChance)
		}
	}
}

/*
================
chanceProbe

================
*/
type chanceProbe struct{ record *int32 }

/*
================
Chance

================
*/
func (c chanceProbe) Chance(_ uint32, chance int32) bool { *c.record = chance; return true }

/*
================
Rand

================
*/
func (c chanceProbe) Rand() int32 { return 0 }

/*
================
TestCurseRollVectors

Vectors carried over from the former combat.CurseParameters port: the
Impotent roll shares the grade-shape duration/chance scaling.
================
*/
func TestCurseRollVectors(t *testing.T) {
	for _, tc := range []struct {
		level    uint8
		duration uint32
		chance   int32
	}{{80, 30000, 20}, {90, 22500, 10}, {100, 15000, 2}, {255, 15000, 2}} {
		var gotChance int32
		r := Roll(RollInput{Params: params(0x63737372, 30000, 20, 8, 35), TargetLevel: tc.level}, chanceProbe{record: &gotChance})
		if len(r) != 1 || r[0].Status != Impotent || r[0].DurationMs != tc.duration || gotChance != tc.chance || r[0].Param38 != 35 {
			t.Fatalf("%+v got %+v chance %d", tc, r, gotChance)
		}
	}
}
