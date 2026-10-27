/*
===========================================================================

tickhooks_bench_test.go - the long-tail tick hooks, measured (#1820 D)

The prod slow-leg lines named three action hooks with long tails:
openSkillCastOwnerSnapshot (max 1.7 s), checkpointOnlineSkillJobs (585 ms)
and retireGroundApproaches (588 ms). These benches seed each hook with a
prod-shaped load so a fix is judged on before/after numbers, not on the
spike that happened to be sampled. Loads are deliberately past the live
shard's size (a few dozen sessions): the tail shows when a stalled tick
lets queues back up.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// benchFinalizeCounts are {pending finalizes, current commands, projectiles}.
var benchFinalizeCounts = []struct {
	name                string
	finalizes, commands int
	projectiles         int
}{
	{"finalizes=0", 0, 0, 0},
	{"finalizes=1000", 1000, 200, 200},
	{"finalizes=10000", 10000, 1000, 1000},
	{"finalizes=50000", 50000, 2000, 2000},
}

/*
================
benchSnapshotRuntime

A runtime holding a backed-up finalize queue, as after a stalled tick.
================
*/
func benchSnapshotRuntime(finalizes, commands, projectiles int) *Runtime {
	rt := NewRuntime(&enterworld.Deps{}, nil)
	for i := 0; i < finalizes; i++ {
		name := fmt.Sprintf("char%05d", i)
		rt.pendingSkillFinalizes = append(rt.pendingSkillFinalizes, pendingSkillFinalize{
			divisionID:       testDivision,
			characterName:    name,
			ownerKey:         simulation.WorldKey(testDivision, name),
			presentationOnly: i%2 == 0,
		})
	}
	if rt.currentSkillCommands == nil {
		rt.currentSkillCommands = make(map[string]currentSkillCommand)
	}
	for i := 0; i < commands; i++ {
		rt.currentSkillCommands[simulation.WorldKey(testDivision, fmt.Sprintf("caster%05d", i))] = currentSkillCommand{}
	}
	for i := 0; i < projectiles; i++ {
		name := fmt.Sprintf("archer%05d", i)
		rt.pendingProjectileCasts = append(rt.pendingProjectileCasts, pendingProjectileCast{
			divisionID:    testDivision,
			characterName: name,
			ownerKey:      simulation.WorldKey(testDivision, name),
		})
	}
	return rt
}

/*
================
BenchmarkOpenSkillCastOwnerSnapshot
================
*/
func BenchmarkOpenSkillCastOwnerSnapshot(b *testing.B) {
	for _, shape := range benchFinalizeCounts {
		b.Run(shape.name, func(b *testing.B) {
			rt := benchSnapshotRuntime(shape.finalizes, shape.commands, shape.projectiles)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				owners := rt.openSkillCastOwnerSnapshot()
				if len(owners) == 0 && shape.finalizes > 0 {
					b.Fatal("snapshot lost its owners")
				}
			}
		})
	}
}

/*
================
BenchmarkRetireGroundApproaches

Stops queued for one division (the lock-churn worst case) and spread over
fifty. Seeding happens outside the timed span; the hook drains the queue.
================
*/
func BenchmarkRetireGroundApproaches(b *testing.B) {
	shapes := []struct {
		name      string
		stops     int
		divisions int
	}{
		{"stops=100/div=1", 100, 1},
		{"stops=1000/div=1", 1000, 1},
		{"stops=5000/div=1", 5000, 1},
		{"stops=5000/div=50", 5000, 50},
	}
	for _, shape := range shapes {
		b.Run(shape.name, func(b *testing.B) {
			rt := NewRuntime(&enterworld.Deps{}, nil)
			seed := func() {
				for i := 0; i < shape.stops; i++ {
					division := testDivision
					if shape.divisions > 1 {
						division = fmt.Sprintf("div%02d", i%shape.divisions)
					}
					name := fmt.Sprintf("walker%05d", i)
					rt.groundApproachStops.Store(simulation.WorldKey(division, name),
						groundApproachStop{division: division, name: name, revision: 7})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				seed()
				b.StartTimer()
				rt.retireGroundApproaches()
			}
		})
	}
}

// benchLookupSource is the indexed character source the store wires in
// production: CharacterByName answers from a map, not a roster scan.
type benchLookupSource struct {
	byDivision map[string][]*enterworld.Character
	byName     map[string]*enterworld.Character
}

/*
================
CharactersForDivision
================
*/
func (s *benchLookupSource) CharactersForDivision(divisionID string) []*enterworld.Character {
	return s.byDivision[divisionID]
}

/*
================
CharacterByName
================
*/
func (s *benchLookupSource) CharacterByName(divisionID, name string) *enterworld.Character {
	return s.byName[simulation.WorldKey(divisionID, name)]
}

/*
================
benchCheckpointRuntime

A runtime with owners persistent effects in the registry and the matching
characters behind an indexed source. The deferred door runs the update in
place: the store's own cost is the clone lane's (#1820 C), not this hook's.
================
*/
func benchCheckpointRuntime(owners, effectsPerOwner int) (*Runtime, []*enterworld.Character) {
	source := &benchLookupSource{
		byDivision: map[string][]*enterworld.Character{},
		byName:     map[string]*enterworld.Character{},
	}
	characters := make([]*enterworld.Character, owners)
	for i := 0; i < owners; i++ {
		c := testCharacter()
		c.ID, c.Name = int64(1000+i), fmt.Sprintf("buffed%04d", i)
		characters[i] = c
		source.byDivision[testDivision] = append(source.byDivision[testDivision], c)
		source.byName[simulation.WorldKey(testDivision, c.Name)] = c
	}
	deps := &enterworld.Deps{
		Characters: source,
		UpdateCharacterDeferred: func(character *enterworld.Character, label string, update func() bool) bool {
			return update()
		},
	}
	rt := NewRuntime(deps, nil)
	for i, c := range characters {
		for j := 0; j < effectsPerOwner; j++ {
			rt.effects.Apply(statuseffect.Effect{
				Persistent:    true,
				DivisionID:    testDivision,
				CharacterName: c.Name,
				SkillID:       uint32(1000 + j),
				SkillGroup:    uint32(1000 + j),
				InstanceToken: uint32(1 + i*effectsPerOwner + j),
				StartedAtMs:   0,
				ExpiresAtMs:   30 * 24 * 3600 * 1000,
			})
		}
	}
	return rt, characters
}

/*
================
BenchmarkCheckpointOnlineSkillJobsScan

Nothing due: the per-tick price of walking every owner's effects.
================
*/
func BenchmarkCheckpointOnlineSkillJobsScan(b *testing.B) {
	rt, _ := benchCheckpointRuntime(200, 6)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt.checkpointOnlineSkillJobs(1000)
	}
}

/*
================
TestOpenSkillCastOwnerSnapshotMatchesRecomputedKeys

The snapshot reads the owner keys cached at queue time. Pin its output
against the pre-cache algorithm (WorldKey recomputed per entry, mixed
case included) so the cache can never drift from the key it replaces.
================
*/
func TestOpenSkillCastOwnerSnapshotMatchesRecomputedKeys(t *testing.T) {
	rt := NewRuntime(&enterworld.Deps{}, nil)
	// WorldKey lowercases the character name only; divisions are canonical
	// ids. Mixed-case names are the case the cached key must fold exactly
	// as the recomputed one did.
	finals := []string{"Alice", "alice", "Bob"}
	for i, name := range finals {
		rt.pendingSkillFinalizes = append(rt.pendingSkillFinalizes, pendingSkillFinalize{
			divisionID: testDivision, characterName: name,
			ownerKey: simulation.WorldKey(testDivision, name), presentationOnly: i == 2,
		})
	}
	rt.pendingProjectileCasts = append(rt.pendingProjectileCasts, pendingProjectileCast{
		divisionID: testDivision, characterName: "Carol",
		ownerKey: simulation.WorldKey(testDivision, "Carol"),
	})
	if rt.currentSkillCommands == nil {
		rt.currentSkillCommands = make(map[string]currentSkillCommand)
	}
	rt.currentSkillCommands[simulation.WorldKey(testDivision, "alice")] = currentSkillCommand{}

	want := map[string]bool{}
	for _, pending := range rt.pendingSkillFinalizes {
		if !pending.presentationOnly {
			want[simulation.WorldKey(pending.divisionID, pending.characterName)] = false
		}
	}
	for owner := range rt.currentSkillCommands {
		want[owner] = true
	}
	for _, pending := range rt.pendingProjectileCasts {
		want[simulation.WorldKey(pending.divisionID, pending.characterName)] = true
	}
	got := rt.openSkillCastOwnerSnapshot()
	if len(got) != len(want) {
		t.Fatalf("snapshot = %v, want %v", got, want)
	}
	for key, open := range want {
		if got[key] != open {
			t.Fatalf("snapshot[%q] = %t, want %t (snapshot %v)", key, got[key], open, got)
		}
	}
	// Alice's current command wins over her finalize entry, Bob's
	// presentation-only close gates nothing, Carol is open via projectile.
	if !rt.hasOpenSkillCast(testDivision, "ALICE") || rt.hasOpenSkillCast(testDivision, "bob") ||
		!rt.hasOpenSkillCast(testDivision, "CAROL") {
		t.Fatal("hasOpenSkillCast disagrees with the cached keys")
	}
}

/*
================
TestRetireGroundApproachesDrainsEveryDivision

Lane cached or not, every queued stop is consumed exactly once: a
pickup-armed stop pushes its release, and nothing remains queued
afterwards. (The replaced-mid-drain guard itself is the unchanged
CompareAndDelete inside retireGroundApproaches.)
================
*/
func TestRetireGroundApproachesDrainsEveryDivision(t *testing.T) {
	rt := NewRuntime(&enterworld.Deps{}, nil)
	var pushed []string
	rt.PushCharacterFrames = func(divisionID, characterName string, frames []wire.Frame) {
		for _, frame := range frames {
			if frame.Opcode != wire.OpActionState {
				t.Errorf("pushed opcode %04x, want the action-state release", frame.Opcode)
			}
		}
		pushed = append(pushed, simulation.WorldKey(divisionID, characterName))
	}
	// One pickup approach armed at revision 7 in each of two divisions.
	for _, division := range []string{testDivision, "other-division"} {
		name := "walker-" + division
		rt.Pending.ArmGround(grounditem.Pending{
			Key: grounditem.PendingKey(division, name), DivisionID: division,
			CharacterName: name, MovementRevision: 7,
		})
		rt.RetireGroundApproach(division, name, 7)
	}
	// A plain stop with no approach behind it retires silently.
	rt.RetireGroundApproach(testDivision, "plain", 3)

	rt.retireGroundApproaches()

	remaining := 0
	rt.groundApproachStops.Range(func(key, value any) bool {
		remaining++
		return true
	})
	if remaining != 0 {
		t.Fatalf("leftover stops = %d, want the queue drained", remaining)
	}
	if len(pushed) != 2 {
		t.Fatalf("release pushes = %v, want one per armed division", pushed)
	}
}

/*
================
BenchmarkCheckpointOnlineSkillJobsDue

Every owner crosses a 300 s checkpoint boundary on every call: the
synchronised burst after a mass login.
================
*/
func BenchmarkCheckpointOnlineSkillJobsDue(b *testing.B) {
	rt, _ := benchCheckpointRuntime(200, 6)
	now := int64(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now += 300_000
		rt.checkpointOnlineSkillJobs(now)
	}
}
