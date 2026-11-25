/*
===========================================================================

spiritability_test.go - tests for spiritability.go

Port-only, behind SRO_NASRUN (#3456): a worn, awake Spirit lends its
skills and stats, and loses them when it comes off, lapses, or the
switch is off.

===========================================================================
*/

package action

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	spiritTestCodename   = "ITEM_MALL_AVATAR_M_NASRUN"
	spiritTestConfusion  = 31143
	spiritTestIgnore     = 31151
	spiritTestPlus       = 3
	spiritTestLifeSecond = 3600
)

/*
================
abilityCharacter

A character with a +3 red mall Spirit awake for an hour in bag slot 20,
and the two skills a +3 lends in the skill data.
================
*/
func abilityCharacter(t *testing.T, socket bool) (*enterworld.Character, *Runtime, *fakeClock) {
	t.Helper()
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 20, RefObjID: 90010, Codename: spiritTestCodename,
		TypeFlags: wire.PackTypeFlags(3, 1, 14, 1), VarianceBits: "0", StackCount: 1, Plus: spiritTestPlus}}
	// newTestRuntime's setup, with the two lent skills in the skill data.
	rt := NewRuntime(&enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {c}},
		Items: staticItemSource{spiritTestCodename: {RefObjID: 90010, Codename: spiritTestCodename,
			TypeIDs: [4]int64{3, 1, 14, 1}, Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}}},
		Skills: codenameSkills{staticSkillSource{
			spiritTestConfusion: {ID: spiritTestConfusion, Codename: "SKILL_MALL_NASRUN_THEDEVILCONFUSION_01"},
			spiritTestIgnore:    {ID: spiritTestIgnore, Codename: "SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_01"},
		}},
		NpcSpawns: enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	rt.NasrunSocket = socket
	c.MissionInventory[0].Data = spiritAwakeFor(rt, spiritTestLifeSecond)
	return c, rt, clock
}

/*
================
TestWornSpiritLendsItsSkillsUntilItLapses
================
*/
func TestWornSpiritLendsItsSkillsUntilItLapses(t *testing.T) {
	c, rt, clock := abilityCharacter(t, true)
	if domain.SkillLearned(c, spiritTestConfusion) {
		t.Fatal("the Spirit's skill is learned before it is worn")
	}
	wear := rt.HandleItemMove(testDivision, c, []byte{0x24, 20, nasrun.SpiritSlot})
	if wear.Frames[0].Payload[0] != 1 {
		t.Fatalf("wear: %+v", wear)
	}
	s := c.AvatarInventory.Spirit
	if s == nil || s.Plus != spiritTestPlus || s.HPPercent != 18 || s.Block != 1 || len(s.Skills) != 2 {
		t.Fatalf("worn +3 Spirit effect = %+v", s)
	}
	if !domain.SkillLearned(c, spiritTestConfusion) || !domain.SkillLearned(c, spiritTestIgnore) {
		t.Fatal("a worn +3 Spirit does not lend Confusion and Ignore 1")
	}
	// The entry bootstrap derives the same list from the stored rows (#4384).
	if got := rt.SpiritSkillsAt(testDivision, c); !slices.Equal(got, s.Skills) {
		t.Fatalf("entry spirit skills = %v, want %v", got, s.Skills)
	}
	// The owner's skill strip learns the lent skills with the move (#4384).
	if got := spiritSkillsSent(t, wear.Frames); !slices.Equal(got, s.Skills) {
		t.Fatalf("spirit skills sent = %v, want %v", got, s.Skills)
	}
	// Not yet: a tick before the expiry keeps it.
	rt.advanceSpirits(clock.Now().UnixMilli())
	if c.AvatarInventory.Spirit == nil {
		t.Fatal("the Spirit came off before its clock ran out")
	}
	clock.Advance(spiritTestLifeSecond * time.Second)
	rt.advanceSpirits(clock.Now().UnixMilli())
	if c.AvatarInventory.Spirit != nil || domain.SkillLearned(c, spiritTestConfusion) {
		t.Fatalf("a lapsed Spirit still lends: %+v", c.AvatarInventory.Spirit)
	}
	// It stays worn: only its share is gone.
	if len(c.AvatarInventory.Rows) != 1 {
		t.Fatalf("the lapse took the Spirit off: %+v", c.AvatarInventory.Rows)
	}
}

/*
================
TestSpiritTakenOffLendsNothing
================
*/
func TestSpiritTakenOffLendsNothing(t *testing.T) {
	c, rt, _ := abilityCharacter(t, true)
	rt.HandleItemMove(testDivision, c, []byte{0x24, 20, nasrun.SpiritSlot})
	off := rt.HandleItemMove(testDivision, c, []byte{0x23, nasrun.SpiritSlot, 20})
	if off.Frames[0].Payload[0] != 1 {
		t.Fatalf("take off: %+v", off)
	}
	if got := spiritSkillsSent(t, off.Frames); got == nil || len(got) != 0 {
		t.Fatalf("spirit skills sent on take off = %v, want an empty list", got)
	}
	if c.AvatarInventory.Spirit != nil || domain.SkillLearned(c, spiritTestIgnore) {
		t.Fatalf("a removed Spirit still lends: %+v", c.AvatarInventory.Spirit)
	}
	if _, indexed := rt.spiritActors.Load(simulation.WorldKey(testDivision, c.Name)); indexed {
		t.Fatal("a removed Spirit stays in the lapse index")
	}
}

/*
================
TestStoredSpiritIsInertWithTheSwitchOff

An awake Spirit stored in slot 4 while the switch was on lends nothing
on entry with the switch off (native), and everything with it on.
================
*/
func TestStoredSpiritIsInertWithTheSwitchOff(t *testing.T) {
	for _, socket := range []bool{false, true} {
		c, rt, _ := abilityCharacter(t, socket)
		spirit := c.MissionInventory[0]
		spirit.Slot = int64(nasrun.SpiritSlot)
		c.MissionInventory = nil
		c.AvatarInventory = &domain.AvatarInventory{Capacity: nasrun.StorageSlots, Rows: []enterworld.InventoryRow{spirit}}
		rt.restoreSpirit(testDivision, c.Name)
		if got := c.AvatarInventory.Spirit != nil; got != socket {
			t.Fatalf("switch %v: entry attached = %v", socket, got)
		}
	}
}

/*
================
spiritSkillTable

A skill table holding what a +3 red Spirit lends, Devil's Confusion and
Monster Defence Ignore 1,
in the shapes the VSRO client authors (synthetic rows, not the retail
ones): a cast self buff of dura, dru {20, 20}, hst3 10 and reqi 14 1, and
an instant buff of dura, msid 1 and reqi 14 1.
================
*/
func spiritSkillTable(t *testing.T) *enterworld.TextdataSkills {
	t.Helper()
	row := func(id uint32, code, activity string, tail ...uint32) string {
		fields := make([]string, 118)
		for i := range fields {
			fields[i] = "0"
		}
		// Each row its own group, as the client's (881 Confusion, 883
		// Ignore): a shared group would share the cooldown.
		fields[0], fields[1], fields[2], fields[3], fields[4], fields[5] = "1", strconv.FormatUint(uint64(id), 10), strconv.FormatUint(uint64(id), 10), code, "x", code
		fields[7], fields[8], fields[14], fields[34], fields[47] = "1", activity, "1800000", "1", "3"
		for _, col := range []int{50, 51, 57, 58, 59} {
			fields[col] = "255"
		}
		if activity == "2" {
			fields[11], fields[12], fields[13], fields[18] = "166", "1", "800", "1795162206"
		}
		fields[61], fields[62], fields[63], fields[64], fields[65], fields[68] = "x.ddj", "SN_X", "xxx", "SN_X_TT_DESC", "xxx", "3"
		for i, word := range tail {
			fields[69+i] = strconv.FormatUint(uint64(word), 10)
		}
		return strings.Join(fields, "\t")
	}
	const dura, dru, hst3, reqi, msid = 0x64757261, 0x647275, 0x68737433, 0x72657169, 0x6d736964
	dir := t.TempDir()
	rows := []string{
		row(spiritTestConfusion, "SKILL_MALL_NASRUN_THEDEVILCONFUSION_01", "2", dura, 600000, dru, 20, 20, hst3, 10, reqi, 14, 1),
		row(spiritTestIgnore, "SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_01", "1", dura, 300000, msid, 1, reqi, 14, 1),
	}
	if err := os.WriteFile(filepath.Join(dir, "skilldata.txt"), []byte("shard_a.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shard_a.txt"), []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return enterworld.NewTextdataSkills(dir)
}

/*
================
TestWornSpiritsSkillsCastAndApply

End to end: without the Spirit, Devil's Confusion is not the character's;
with the +3 Spirit worn it casts and speeds the character up, its dru
raises both damage-rate pairs, and the Ignore skill raises the
ignore-defence chance (0xBC) by its msid. Taking the Spirit off ends both.
================
*/
func TestWornSpiritsSkillsCastAndApply(t *testing.T) {
	c, rt, clock := abilityCharacter(t, true)
	skills := spiritSkillTable(t)
	rt.deps.(*enterworld.Deps).Skills = skills
	c.CurrentMP = testInt64(10000)
	if result := castSelf(rt, c, spiritTestConfusion); openedCast(result) {
		t.Fatal("Devil's Confusion cast without a Spirit")
	}
	rt.HandleItemMove(testDivision, c, []byte{0x24, 20, nasrun.SpiritSlot})
	_, run := worldSpeeds(rt, c)
	// The caster's damage rates (0x80..0x83: the physical pair, then the
	// magical pair) before the buff, for the dru assertions below.
	rates := func() [4]float32 {
		stats, _, err := rt.playerCombatStats(testDivision, c)
		if err != nil {
			t.Fatal(err)
		}
		var out [4]float32
		for i, param := range []uint16{0x80, 0x81, 0x82, 0x83} {
			v, _ := stats.Param(param)
			out[i] = v
		}
		return out
	}
	baseRates := rates()
	if result := castSelf(rt, c, spiritTestConfusion); !openedCast(result) {
		t.Fatalf("Devil's Confusion refused: %q %+v", result.DiagnosticRefusal, result.Frames)
	}
	landCast(rt, clock, skills, spiritTestConfusion)
	// The buff's dru {20, 20} raises both damage-rate pairs by 20 while
	// it holds (#4407: v1.188 installer 0x595A97; word 0 on the physical
	// pair 0x80/0x81, word 1 on the magical pair 0x82/0x83).
	for i, got := range rates() {
		if want := baseRates[i] + 20; got != want {
			t.Fatalf("damage rate 0x%02x = %v under Devil's Confusion, want %v (base %v + dru 20)", 0x80+i, got, want, baseRates[i])
		}
	}
	if _, faster := worldSpeeds(rt, c); faster <= run {
		t.Fatalf("run speed %v after Devil's Confusion, was %v: hst3 never landed", faster, run)
	}
	if result := castSelf(rt, c, spiritTestIgnore); !openedCast(result) {
		t.Fatalf("Ignore 1 refused: %q %+v", result.DiagnosticRefusal, result.Frames)
	}
	landCast(rt, clock, skills, spiritTestIgnore)
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := stats.Param(0xbc); v != 1 {
		t.Fatalf("ignore-defence chance %v after Ignore 1, want 1", v)
	}
	// Off comes the Spirit: both buffs lean on it (reqi 14 1) and end.
	rt.HandleItemMove(testDivision, c, []byte{0x23, nasrun.SpiritSlot, 20})
	if hasSkillEffect(rt, c.Name, spiritTestConfusion) || hasSkillEffect(rt, c.Name, spiritTestIgnore) {
		t.Fatal("the Spirit's buffs outlived it")
	}
	if _, after := worldSpeeds(rt, c); after != run {
		t.Fatalf("run speed %v after the Spirit came off, want %v", after, run)
	}
	if got := rates(); got != baseRates {
		t.Fatalf("damage rates %v after the Spirit came off, want the base %v: the dru outlived its buff", got, baseRates)
	}
}

/*
================
landCast

Runs a cast through its casting time and action to the instant its
effect installs (damagetomp_test.go's order).
================
*/
func landCast(rt *Runtime, clock *fakeClock, skills *enterworld.TextdataSkills, id uint32) {
	row, _ := skills.SkillByID(id)
	clock.Advance(time.Duration(row.ActionCastingTimeMs+1) * time.Millisecond)
	rt.advanceProjectileCasts(clock.NowMs())
	clock.Advance(time.Duration(row.ActionDurationMs+1) * time.Millisecond)
	rt.drainSkillFinalizes(clock.NowMs())
}

/*
================
openedCast
================
*/
func openedCast(result OpResult) bool {
	for _, f := range result.Frames {
		if f.Opcode == wire.OpSkillCastResult && len(f.Payload) > 0 && f.Payload[0] == 1 {
			return true
		}
	}
	return false
}

/*
================
spiritSkillsSent

The skill ids of the one Spirit skill frame among frames: nil when none
was sent, an empty list for an empty frame.
================
*/
func spiritSkillsSent(t *testing.T, frames []wire.Frame) []uint32 {
	t.Helper()
	var out []uint32
	found := false
	for _, frame := range frames {
		if frame.Opcode != wire.OpSpiritSkills {
			continue
		}
		if found {
			t.Fatal("two Spirit skill frames")
		}
		found = true
		out = []uint32{}
		r := wire.NewReader(frame.Payload)
		count, err := r.U8()
		for i := 0; err == nil && i < int(count); i++ {
			var id uint32
			if id, err = r.U32(); err == nil {
				out = append(out, id)
			}
		}
		if err != nil || r.Done() != nil {
			t.Fatalf("malformed Spirit skill frame % x", frame.Payload)
		}
	}
	return out
}
