/*
===========================================================================

pandorabox_monster_test.go - the Pandora's Box monster (#4540)

The box lands untouchable on the factory ring of 20, its HP drains
toward the job's clock without dying, it casts its skill from itself
as each wave lands, and it leaves when the event ends. Without the box
row the opener hears the port's step notices instead.

===========================================================================
*/
package action

import (
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// pandoraTestBoxHP is the v1.696 row's 1000 HP.
const pandoraTestBoxHP = 1000

// pandoraTestBoxSkillID is the box cast's id in the fixture's table.
const pandoraTestBoxSkillID = 31133

/*
================
pandoraBoxSkills

A skill table holding only the box's cast, with an authored action
lifecycle (no casting time, a 3 s action).
================
*/
func pandoraBoxSkills() staticSkillSource {
	return staticSkillSource{pandoraTestBoxSkillID: {
		ID: pandoraTestBoxSkillID, Codename: pandoraBoxSkill,
		ActionCastingTimePinned: true, ActionDurationPinned: true, ActionDurationMs: 3000,
	}}
}

/*
================
TestPandoraDrainCostFollowsTheClock

The box's HP permille is held to 1000 - tick * 1000 / 240 in integer
steps: a full box loses 8 permille at tick 2, a box already under the
clock loses nothing, and the last tick asks for everything.
================
*/
func TestPandoraDrainCostFollowsTheClock(t *testing.T) {
	cases := []struct {
		hp, max uint32
		tick    int64
		want    uint32
	}{
		{hp: 1000, max: 1000, tick: 2, want: 8},
		{hp: 992, max: 1000, tick: 2, want: 0},
		{hp: 500, max: 1000, tick: 121, want: 4},
		{hp: 1000, max: 1000, tick: pandoraTicks, want: 1000},
		{hp: 1, max: 0, tick: 2, want: 0},
	}
	for _, c := range cases {
		if got := pandoraDrainCost(c.hp, c.max, c.tick); got != c.want {
			t.Errorf("hp %d of %d at tick %d: cost %d, want %d", c.hp, c.max, c.tick, got, c.want)
		}
	}
}

/*
================
TestPandoraBoxMonsterStandsDrainsCastsAndLeaves

The box lands untouchable within 20 of the opener and refuses every
attack; the first wave's tick drains it to 992 HP and plays its cast
from the box, with no notice to the opener; the job's last tick drains
it to its last point, not to death; the closing event's box leaves on
the next lifetime pass.
================
*/
func TestPandoraBoxMonsterStandsDrainsCastsAndLeaves(t *testing.T) {
	rt, event, clock, told := pandoraTestRuntime(t)
	rt.deps = &enterworld.Deps{Skills: pandoraBoxSkills()}
	event.box = rt.landPandoraBox(event, clock.UnixMilli())
	if event.box == 0 {
		t.Fatal("the box monster did not land")
	}
	rt.pandora.events = []*pandoraEvent{event}
	box, ok := rt.Monsters.Get(testDivision, event.box)
	if !ok || box.BodyStatus != untouchableBodyStatus || box.CurrentHP != pandoraTestBoxHP {
		t.Fatalf("the box stands %+v", box)
	}
	if d := math.Hypot(box.Spawn.X-event.anchor.X, box.Spawn.Z-event.anchor.Z); d > pandoraBoxRadius+1e-3 {
		t.Fatalf("the box landed %f from its opener", d)
	}
	if code := rt.monsterTargetRefusal(testCharacter(), box); code != 0x3020 {
		t.Fatalf("an attack on the box answered %#x, want 0x3020", code)
	}

	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	out := rt.advancePandoraBoxes(clock.UnixMilli())
	if event.wave != 1 || len(out) != 1 || out[0].SourceGID != event.box || len(out[0].Frames) == 0 ||
		out[0].Frames[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("the first wave's tick sent %+v", out)
	}
	if len(*told) != 0 {
		t.Fatalf("the opener heard %d notices beside a standing box", len(*told))
	}
	if hp, _ := rt.Monsters.CurrentHP(testDivision, event.box); hp != 992 {
		t.Fatalf("the box holds %d HP after tick 2, want 992", hp)
	}

	*clock = time.UnixMilli(event.endsAtMs)
	rt.advancePandoraBoxes(clock.UnixMilli())
	if len(rt.pandora.events) != 0 {
		t.Fatal("the event stayed open")
	}
	if hp, _ := rt.Monsters.CurrentHP(testDivision, event.box); hp != 1 {
		t.Fatalf("the last tick left %d HP, want the last point", hp)
	}
	rt.Monsters.ExpireMonsterLifetimes(clock.UnixMilli())
	if _, ok := rt.Monsters.Get(testDivision, event.box); ok {
		t.Fatal("the closed event's box stayed")
	}
}

/*
================
TestPandoraWithoutTheBoxRowTellsTheSteps

A shard without the box row lands none, and the opener hears the wave
notice the box's HP bar would otherwise show.
================
*/
func TestPandoraWithoutTheBoxRowTellsTheSteps(t *testing.T) {
	rt, event, clock, told := pandoraTestRuntime(t)
	if !event.tells(pandoraTimeLine) || event.tells("") {
		t.Fatal("a box-less event keeps its notices")
	}
	event.box = 1
	if event.tells(pandoraWaveLine) || !event.tells(pandoraNoKindLine) {
		t.Fatal("a standing box silences the steps but not a missing kind")
	}
	event.box = 0
	rt.pandora.events = []*pandoraEvent{event}
	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	if out := rt.advancePandoraBoxes(clock.UnixMilli()); len(out) != 0 || len(*told) != 1 {
		t.Fatalf("a box-less wave sent %d casts and %d notices", len(out), len(*told))
	}
}
