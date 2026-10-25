/*
===========================================================================

flagwar_rebirth_test.go - camp revival and the match's final return

The public rebirth lane must keep the match lease, revive atomically, and
rebuild only the returning player's arena scene. Closing the instance is
ordinary relocation, including for a corpse; it never grants a revival.
These fixtures use level-one keepers and need no licensed game data or
running transport.

===========================================================================
*/

package action

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
flagWarRebirthFixture

One active, announced match. The clock is deliberately past mission start
so a reset must reconstruct elapsed time rather than restart the timer.
================
*/
type flagWarRebirthFixture struct {
	rig     *flagWarMatchRuntime
	match   *flagWarMatch
	players []*enterworld.Character
	peers   map[string][]wire.Frame
}

/*
================
newFlagWarRebirthFixture
================
*/
func newFlagWarRebirthFixture(t *testing.T) *flagWarRebirthFixture {
	t.Helper()
	players := []*enterworld.Character{
		flagWarPlayer(111, "Alpha", 1), flagWarPlayer(112, "Bravo", 1),
		flagWarPlayer(113, "Charly", 1), flagWarPlayer(114, "Delta", 1),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	rig.rt.TeleportGrace = false
	match := rig.startMatch(t, players...)
	rig.rt.advanceFlagWarMatches(match.startMs + 1)
	rig.clock.now = time.UnixMilli(match.startMs + 12345)
	clear(rig.pushed)
	f := &flagWarRebirthFixture{rig: rig, match: match, players: players, peers: map[string][]wire.Frame{}}
	rig.rt.PushDivisionPeerFrames = func(_, except string, frames []wire.Frame) {
		f.peers[except] = append(f.peers[except], frames...)
	}
	return f
}

/*
================
world
================
*/
func (f *flagWarRebirthFixture) world(c *enterworld.Character) simulation.WorldState {
	return f.rig.rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState {
		return simulation.SeedWorldState(c)
	})
}

/*
================
settleCorpse

Seed a settled corpse away from either camp and run the match death owner.
Its death record is a sentinel: an instance revival must not overwrite the
ordinary world's reverse-return record with an arena point.
================
*/
func (f *flagWarRebirthFixture) settleCorpse(c *enterworld.Character, kind pk.DeathKind) simulation.WorldState {
	rt := f.rig.rt
	unlock := rt.lockDivision(testDivision)
	defer unlock()
	rt.deps.Update(c, "flag-war-test-death", func() bool {
		c.CurrentHP, c.CurrentMP = testInt64(0), testInt64(0)
		c.DeathKind = uint8(kind)
		c.LastExpLoss = 17
		state := rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState {
			return simulation.SeedWorldState(c)
		}, func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: flagWarArenaRegionID, X: 700, Y: 12, Z: -200, Angle: 1234}
			w.MoveSegment = nil
			w.SettleDeath(f.rig.clock.NowMs())
		})
		writeBackWorld(c, state)
		c.World.LastDeathPoint = &domain.WorldPoint{WorldSpawn: *worldSpawnFromMission(instanceWorldTestStart), World: 1}
		rt.recordFlagWarDeath(testDivision, c, nil, f.rig.clock.NowMs())
		return true
	})
	return f.world(c)
}

/*
================
requireReborn

Check the committed keeper, bootstrap, peer correction and life publication,
not merely a successful-looking response count.
================
*/
func (f *flagWarRebirthFixture) requireReborn(t *testing.T, c *enterworld.Character, result OpResult, want simulation.Spawn) {
	t.Helper()
	if !enterworld.CharacterAlive(c) || c.CurrentHP == nil || *c.CurrentHP != enterworld.DerivedMaxHP(c) ||
		c.CurrentMP == nil || *c.CurrentMP != enterworld.DerivedMaxMP(c) {
		t.Fatalf("revived vitals hp=%v mp=%v", c.CurrentHP, c.CurrentMP)
	}
	if c.DeathKind != 0 || c.LastExpLoss != 0 || c.NativeBodyStatus != untouchableBodyStatus {
		t.Fatalf("revived death/body state: kind=%d loss=%d body=%d", c.DeathKind, c.LastExpLoss, c.NativeBodyStatus)
	}
	state := f.world(c)
	if state.Spawn != want || state.MoveSegment != nil || state.Sitting || !state.SpawnSet {
		t.Fatalf("revived world=%+v, want settled %+v", state, want)
	}
	if len(result.Frames) < 2 || result.Frames[0].Opcode != enterworld.OpcodeResetClient ||
		result.Frames[1].Opcode != transport.OpEnterWorldResult {
		t.Fatalf("revival did not prepare a complete re-entry: %+v", result)
	}
	bootstrap, err := transport.DecodeEnterWorldResult(result.Frames[1].Payload)
	if err != nil || !bootstrap.OK {
		t.Fatalf("invalid revival bootstrap: %+v/%v", bootstrap, err)
	}
	var entry struct {
		Bootstrap struct {
			Character struct {
				HP int64 `json:"hp"`
				MP int64 `json:"mp"`
			} `json:"character"`
			LocalPlayerEntry struct {
				StartProfile struct {
					RegionID uint16  `json:"regionId"`
					X        float64 `json:"x"`
					Z        float64 `json:"z"`
				} `json:"startProfile"`
			} `json:"localPlayerEntry"`
		} `json:"bootstrap"`
	}
	if err := json.Unmarshal(bootstrap.Blob, &entry); err != nil {
		t.Fatal(err)
	}
	profile := entry.Bootstrap.LocalPlayerEntry.StartProfile
	if entry.Bootstrap.Character.HP != *c.CurrentHP || entry.Bootstrap.Character.MP != *c.CurrentMP ||
		profile.RegionID != want.RegionID || profile.X != want.X || profile.Z != want.Z {
		t.Fatalf("bootstrap retained the corpse: %+v", entry.Bootstrap)
	}
	if len(result.Broadcast) < 3 || result.Broadcast[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("missing peer revival: %+v", result.Broadcast)
	}
	correction, err := wire.DecodeObjectSourceCorrection(result.Broadcast[0].Payload)
	if err != nil || correction.Position.RegionID != want.RegionID || correction.Position.X != float32(want.X) ||
		correction.Position.Y != float32(want.Y) || correction.Position.Z != float32(want.Z) || correction.Position.Heading != want.Angle {
		t.Fatalf("peer correction=%+v/%v, want %+v", correction, err, want)
	}
	for _, frames := range [][]wire.Frame{result.Frames, result.Broadcast} {
		alive, protected := false, false
		for _, frame := range frames {
			if frame.Opcode != wire.OpObjectStateRefresh {
				continue
			}
			state, err := wire.DecodeObjectStateRefresh(frame.Payload)
			if err != nil {
				t.Fatal(err)
			}
			alive = alive || state.StateType == wire.StateChannelLife && state.Value == wire.LifeStateAlive
			protected = protected || state.StateType == wire.StateChannelBody && state.Value == untouchableBodyStatus
		}
		if !alive || !protected {
			t.Fatalf("revival omitted LIFE or grace: %+v", frames)
		}
	}
}

/*
================
TestFlagWarRebirthBothChoicesKeepTeamMembership
================
*/
func TestFlagWarRebirthBothChoicesKeepTeamMembership(t *testing.T) {
	for team := 0; team < 2; team++ {
		for _, choice := range []byte{wire.RebirthAtSpecifiedPoint, wire.RebirthAtPresentPoint} {
			for _, kind := range []pk.DeathKind{pk.DeathMonster, pk.DeathSpecialWorld} {
				t.Run(fmt.Sprintf("team%d/choice%d/kind%d", team, choice, kind), func(t *testing.T) {
					f := newFlagWarRebirthFixture(t)
					c := f.players[team]
					corpse := f.settleCorpse(c, kind)
					before := c.Snapshot()
					lease, admitted := f.rig.rt.EntryPopulationLease(testDivision, c.Name)
					if !admitted || lease != f.match.lease {
						t.Fatal("fixture lacks the match lease")
					}
					result := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{choice})
					f.requireReborn(t, c, result, flagWarTeamSpawns[team])
					if domain.CharacterWorldInstance(c) != uint32(lease.ID) ||
						!reflect.DeepEqual(c.World.InstanceReturn, before.World.InstanceReturn) ||
						!reflect.DeepEqual(c.World.LastDeathPoint, before.World.LastDeathPoint) || f.world(c).LifeRevision != corpse.LifeRevision+1 {
						t.Fatalf("camp revival lost the layer, anchor or death record: %+v", c.World)
					}
					if got, ok := f.rig.rt.EntryPopulationLease(testDivision, c.Name); !ok || got != lease {
						t.Fatalf("camp re-admitted into a different population: %+v/%v", got, ok)
					}
					f.rig.rt.advanceFlagWarMatches(f.rig.clock.NowMs())
					if !f.match.participants[c.ID].entered {
						t.Fatal("reconciliation removed a revived participant")
					}
					committed, world := c.Snapshot(), f.world(c)
					for _, duplicate := range []byte{wire.RebirthAtSpecifiedPoint, wire.RebirthAtPresentPoint} {
						again := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{duplicate})
						if len(again.Frames) != 0 || len(again.Broadcast) != 0 ||
							!reflect.DeepEqual(c.Snapshot(), committed) || !reflect.DeepEqual(f.world(c), world) {
							t.Fatalf("duplicate rebirth committed twice: %+v", again)
						}
					}
				})
			}
		}
	}
}

/*
================
TestFlagWarCarrierDeathDropsFlagBeforeCampRebirth
================
*/
func TestFlagWarCarrierDeathDropsFlagBeforeCampRebirth(t *testing.T) {
	f := newFlagWarRebirthFixture(t)
	c := f.players[1]
	f.match.carrierID = c.ID
	f.match.carrierSinceMs = f.rig.clock.NowMs() - 1000
	f.match.channels[0] = &flagWarChannel{characterID: c.ID}
	corpse := f.settleCorpse(c, pk.DeathMonster)
	if f.match.carrierID != 0 || !f.match.flagLive || f.match.flagSpawn != corpse.Spawn || f.match.channels[0] != nil {
		t.Fatal("the death owner did not drop the carrier's flag and channel")
	}
	result := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint})
	f.requireReborn(t, c, result, flagWarTeamSpawns[1])
	flag := f.match.objects[flagWarObjectGID(flagWarSlotFlag)]
	if f.match.carrierID != 0 || !f.match.flagLive || flag == nil || !flag.live || flag.spawn != corpse.Spawn {
		t.Fatal("camp revival recovered or moved the dropped flag")
	}
	if !f.match.participants[c.ID].entered {
		t.Fatal("carrier death removed the participant")
	}
}

/*
================
TestFlagWarCampReentryReconstructsOnlyTheLocalScene
================
*/
func TestFlagWarCampReentryReconstructsOnlyTheLocalScene(t *testing.T) {
	f := newFlagWarRebirthFixture(t)
	c, carrier := f.players[0], f.players[1]
	f.settleCorpse(c, pk.DeathSpecialWorld)
	f.match.carrierID = carrier.ID
	f.match.objects[flagWarObjectGID(flagWarSlotDoor0)].live = false
	want := []wire.Frame{
		{Opcode: wire.OpFlagWarEvent, Payload: wire.EncodeFlagWarMissionStart(0)},
		{Opcode: wire.OpFlagWarEvent, Payload: wire.EncodeFlagWarElapsed(12345)},
	}
	for _, object := range f.match.sortedObjects() {
		if object.live {
			want = append(want, wire.Frame{Opcode: wire.OpObjectListChunk, Payload: f.rig.rt.flagWarObjectRow(object)})
		}
	}
	want = append(want, wire.Frame{Opcode: wire.OpFlagWarEvent, Payload: wire.EncodeFlagWarCarrier(enterworld.ObjectIDForCharacter(carrier))})
	result := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtSpecifiedPoint})
	f.requireReborn(t, c, result, flagWarTeamSpawns[0])
	first := -1
	for index, frame := range result.Frames {
		if frame.Opcode == wire.OpFlagWarEvent {
			first = index
			break
		}
	}
	if first <= 0 || !reflect.DeepEqual(result.Frames[first:], want) {
		t.Fatalf("local scene after reset=%+v, want %+v", result.Frames, want)
	}
	if len(want) != 7 {
		t.Fatalf("fixture has %d scene frames, want team, clock, four live objects and carrier", len(want))
	}
	for _, frame := range result.Broadcast {
		if frame.Opcode == wire.OpFlagWarEvent || frame.Opcode == wire.OpObjectListChunk || frame.Opcode == enterworld.OpcodeResetClient {
			t.Fatalf("peer received a local scene reset: %+v", frame)
		}
	}
	for name, frames := range f.rig.pushed {
		if len(frames) != 0 {
			t.Fatalf("camp re-entry replayed notices to %s: %+v", name, frames)
		}
	}
}

/*
================
TestFlagWarCampRequiresAnActiveExactResident

An ineligible level-one monster corpse retains ordinary present-point
rebirth. A stale match record must not grant a full camp reset.
================
*/
func TestFlagWarCampRequiresAnActiveExactResident(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*flagWarRebirthFixture, *enterworld.Character)
	}{
		{"not-participant", func(f *flagWarRebirthFixture, c *enterworld.Character) { delete(f.match.participants, c.ID) }},
		{"not-entered", func(f *flagWarRebirthFixture, c *enterworld.Character) { f.match.participants[c.ID].entered = false }},
		{"invalid-team", func(f *flagWarRebirthFixture, c *enterworld.Character) { f.match.participants[c.ID].team = 2 }},
		{"wrong-name", func(f *flagWarRebirthFixture, c *enterworld.Character) {
			f.match.participants[c.ID].registrant.Name = "Other"
		}},
		{"wrong-division", func(f *flagWarRebirthFixture, c *enterworld.Character) {
			f.match.participants[c.ID].registrant.Division = "Other"
		}},
		{"stale-lease", func(f *flagWarRebirthFixture, _ *enterworld.Character) { f.match.lease.Generation++ }},
		{"wrong-layer", func(_ *flagWarRebirthFixture, c *enterworld.Character) {
			setCharacterWorld(c, instance.ID(domain.DefaultWorldInstance))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFlagWarRebirthFixture(t)
			c := f.players[0]
			corpse := f.settleCorpse(c, pk.DeathMonster)
			tc.mutate(f, c)
			wantHP, wantMP := f.rig.rt.presentRebirthVitals(testDivision, c)
			result := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint})
			if !enterworld.CharacterAlive(c) || f.world(c).Spawn != corpse.Spawn || *c.CurrentHP != wantHP || *c.CurrentMP != wantMP {
				t.Fatalf("ineligible corpse: spawn=%+v hp=%v mp=%v, want ordinary %d/%d", f.world(c).Spawn, c.CurrentHP, c.CurrentMP, wantHP, wantMP)
			}
			for _, frame := range result.Frames {
				if frame.Opcode == enterworld.OpcodeResetClient || frame.Opcode == wire.OpFlagWarEvent {
					t.Fatalf("ineligible corpse received a camp scene: %+v", result.Frames)
				}
			}
		})
	}
}

/*
================
TestFlagWarFinishedOrRetiringMatchRevivesInTown

Both choices leave a finished arena, including a special-world corpse.
The ordinary beginner present-point rule cannot strand it in that layer.
================
*/
func TestFlagWarFinishedOrRetiringMatchRevivesInTown(t *testing.T) {
	for _, phase := range []string{"closing", "clock-ended", "retiring"} {
		for _, choice := range []byte{wire.RebirthAtSpecifiedPoint, wire.RebirthAtPresentPoint} {
			t.Run(fmt.Sprintf("%s/choice%d", phase, choice), func(t *testing.T) {
				f := newFlagWarRebirthFixture(t)
				c := f.players[0]
				town := simulation.EuropeStartProfile()
				c.World.RebirthPoint = worldSpawnFromMission(town)
				f.settleCorpse(c, pk.DeathSpecialWorld)
				switch phase {
				case "closing":
					f.match.closeMs = f.rig.clock.NowMs() + 1000
				case "clock-ended":
					f.match.endsMs = f.rig.clock.NowMs()
				case "retiring":
					if _, ok := f.rig.rt.Monsters.BeginPopulationRetirement(testDivision, f.match.lease); !ok {
						t.Fatal("could not retire the match population")
					}
				}
				result := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{choice})
				f.requireReborn(t, c, result, town)
				if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance {
					t.Fatal("finished match rebirth retained the arena layer")
				}
				for _, frame := range result.Frames {
					if frame.Opcode == wire.OpFlagWarEvent {
						t.Fatal("town revival replayed the closed arena scene")
					}
				}
			})
		}
	}
}

/*
================
TestFlagWarCampPreparationFailureAndMutationCanRetry
================
*/
func TestFlagWarCampPreparationFailureAndMutationCanRetry(t *testing.T) {
	for _, failure := range []string{"prepare-failure", "character-mutation", "phase-mutation", "retirement-mutation"} {
		t.Run(failure, func(t *testing.T) {
			f := newFlagWarRebirthFixture(t)
			c := f.players[0]
			town := simulation.EuropeStartProfile()
			c.World.RebirthPoint = worldSpawnFromMission(town)
			corpse := f.settleCorpse(c, pk.DeathMonster)
			before := c.Snapshot()
			deps := f.rig.rt.deps
			wrapper := failedRebirthPreparation{Dependencies: deps}
			if failure == "character-mutation" {
				wrapper.mutate = func() { c.Gold = testInt64(12345) }
			}
			if failure == "phase-mutation" {
				wrapper.mutate = func() {
					f.match.mu.Lock()
					f.match.closeMs = f.rig.clock.NowMs() + 1000
					f.match.mu.Unlock()
				}
			}
			if failure == "retirement-mutation" {
				wrapper.mutate = func() { f.rig.rt.Monsters.BeginPopulationRetirement(testDivision, f.match.lease) }
			}
			f.rig.rt.deps = wrapper
			result := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint})
			if failure == "character-mutation" {
				before.Gold = testInt64(12345)
			}
			if len(result.Frames) != 0 || len(result.Broadcast) != 0 || !reflect.DeepEqual(c.Snapshot(), before) ||
				!reflect.DeepEqual(f.world(c), corpse) {
				t.Fatalf("failed preparation partially committed: %+v", result)
			}
			if got, ok := f.rig.rt.EntryPopulationLease(testDivision, c.Name); !ok || got != f.match.lease {
				t.Fatalf("failed preparation lost its match lease: %+v/%v", got, ok)
			}
			f.rig.rt.deps = deps
			f.match.closeMs = 0
			retry := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint})
			want := flagWarTeamSpawns[0]
			if failure == "retirement-mutation" {
				want = town
			}
			f.requireReborn(t, c, retry, want)
			if f.world(c).LifeRevision != corpse.LifeRevision+1 {
				t.Fatal("the retry committed more than one life transition")
			}
		})
	}
}

/*
================
TestFlagWarCampReentryKeepsLiveCompanionsAndRetiresCorpses
================
*/
func TestFlagWarCampReentryKeepsLiveCompanionsAndRetiresCorpses(t *testing.T) {
	for _, alive := range []bool{false, true} {
		t.Run(fmt.Sprintf("alive-%v", alive), func(t *testing.T) {
			f := newFlagWarRebirthFixture(t)
			c := f.players[0]
			f.settleCorpse(c, pk.DeathMonster)
			equipCombatTestPet(t, f.rig.rt, c, 1)
			gid := c.ActiveCOS.GID
			if !alive {
				c.ActiveCOS.CurrentHP = 0
			}
			key := petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: gid}
			session := &petSession{character: c, ready: true, follower: simulation.NewPetFollower(gid, f.world(c).Spawn)}
			f.rig.rt.petSessions = map[petOwnerKey]*petSession{key: session}
			previous := *session
			deps := f.rig.rt.deps
			f.rig.rt.deps = failedRebirthPreparation{Dependencies: deps}
			failed := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtSpecifiedPoint})
			if len(failed.Frames) != 0 || !reflect.DeepEqual(*session, previous) {
				t.Fatal("failed camp preparation did not restore the companion session")
			}
			f.rig.rt.deps = deps
			result := f.rig.rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtSpecifiedPoint})
			f.requireReborn(t, c, result, flagWarTeamSpawns[0])
			rebuilt, despawned := false, false
			for _, frame := range result.Frames {
				rebuilt = rebuilt || frame.Opcode == wire.OpCosRecordCreate
			}
			for _, frame := range result.Broadcast {
				despawned = despawned || frame.Opcode == wire.OpObjectDespawn
			}
			if rebuilt != alive || despawned == alive {
				t.Fatalf("companion alive=%v rebuilt=%v despawned=%v", alive, rebuilt, despawned)
			}
			if alive {
				want := f.rig.rt.companionAdmissionSpawn(c.ActiveCOS, flagWarTeamSpawns[0])
				if !c.ActiveCOS.Summoned || session.transportWorld.Spawn != want || session.generation != previous.generation+1 {
					t.Fatalf("living companion did not follow its camp admission: %+v", session)
				}
			} else if c.CompanionByGID(gid) != nil || c.ActiveCOS.Summoned {
				t.Fatal("a dead companion kept a summoned world identity")
			}
		})
	}
}

/*
================
requireOrdinaryReturn

Evacuation owns placement and anchor cleanup, not life or recovery. Check
the actual private re-entry and peer publication as well as the keeper.
================
*/
func (f *flagWarRebirthFixture) requireOrdinaryReturn(t *testing.T, c, before *enterworld.Character, world simulation.WorldState) {
	t.Helper()
	if before.World == nil || before.World.InstanceReturn == nil {
		t.Fatal("fixture lacks its original return anchor")
	}
	anchor := before.World.InstanceReturn
	want := simulation.Spawn{RegionID: anchor.RegionID, X: float64(anchor.X), Y: float64(anchor.Y), Z: float64(anchor.Z), Angle: anchor.Angle}
	if domain.CharacterWorldInstance(c) != anchor.PackedInstance || c.World.InstanceReturn != nil || f.world(c).Spawn != want {
		t.Fatalf("final return lost its captured anchor: %+v", c.World)
	}
	if !reflect.DeepEqual(c.CurrentHP, before.CurrentHP) || !reflect.DeepEqual(c.CurrentMP, before.CurrentMP) ||
		enterworld.CharacterAlive(c) != enterworld.CharacterAlive(before) || c.DeathKind != before.DeathKind ||
		c.LastExpLoss != before.LastExpLoss || !reflect.DeepEqual(c.World.LastDeathPoint, before.World.LastDeathPoint) ||
		c.NativeBodyStatus != before.NativeBodyStatus || c.BodyStatusOwner != before.BodyStatusOwner ||
		f.world(c).LifeRevision != world.LifeRevision {
		t.Fatal("ordinary return changed life, vitals, death record or body protection")
	}
	frames := f.rig.pushed[c.Name]
	if len(frames) == 0 || frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatal("ordinary return did not publish its re-entry")
	}
	for _, frame := range frames {
		if frame.Opcode == wire.OpFlagWarEvent {
			t.Fatal("final return replayed an arena scene after leaving")
		}
		if frame.Opcode != wire.OpObjectStateRefresh {
			continue
		}
		state, err := wire.DecodeObjectStateRefresh(frame.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if state.StateType == wire.StateChannelBody && state.Value == untouchableBodyStatus ||
			!enterworld.CharacterAlive(before) && state.StateType == wire.StateChannelLife && state.Value == wire.LifeStateAlive {
			t.Fatal("ordinary return published a revival or new untouchable grant")
		}
	}
	peers := f.peers[c.Name]
	if len(peers) != 1 || peers[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("ordinary return peer publication=%+v, want only the relocation correction", peers)
	}
}

/*
================
TestFlagWarCloseOnlyRelocatesDeadAndLivingResidents
================
*/
func TestFlagWarCloseOnlyRelocatesDeadAndLivingResidents(t *testing.T) {
	for _, dead := range []bool{false, true} {
		t.Run(fmt.Sprintf("dead-%v", dead), func(t *testing.T) {
			f := newFlagWarRebirthFixture(t)
			c := f.players[0]
			if dead {
				f.settleCorpse(c, pk.DeathMonster)
			} else {
				c.CurrentHP, c.CurrentMP = testInt64(17), testInt64(9)
			}
			before := c.Snapshot()
			world := f.world(c)
			f.rig.rt.CloseInstanceWorld(testDivision, f.match.lease)
			f.requireOrdinaryReturn(t, c, before, world)
			if _, open := f.rig.rt.Monsters.PopulationLease(testDivision, f.match.lease.ID); open {
				t.Fatal("an evacuated layer was not released")
			}
		})
	}
}

/*
================
TestFlagWarEndReturnReentryFailureKeepsRetryContext
================
*/
func TestFlagWarEndReturnReentryFailureKeepsRetryContext(t *testing.T) {
	for _, dead := range []bool{false, true} {
		t.Run(fmt.Sprintf("dead-%v", dead), func(t *testing.T) {
			f := newFlagWarRebirthFixture(t)
			c := f.players[0]
			if dead {
				f.settleCorpse(c, pk.DeathMonster)
			} else {
				c.CurrentHP, c.CurrentMP = testInt64(17), testInt64(9)
			}
			world := f.world(c)
			before := c.Snapshot()
			deps := f.rig.rt.deps
			f.rig.rt.deps = portalRejectEntry{Dependencies: deps}
			f.rig.clock.now = time.UnixMilli(f.match.endsMs)
			f.rig.rt.advanceFlagWarMatches(f.rig.clock.NowMs())
			if f.match.closeMs == 0 {
				t.Fatal("match did not enter its closing phase")
			}
			f.rig.clock.now = time.UnixMilli(f.match.closeMs)
			f.rig.rt.advanceFlagWarMatches(f.rig.clock.NowMs())
			after := f.world(c)
			if !reflect.DeepEqual(c.Snapshot(), before) || after.Spawn != world.Spawn || after.LifeRevision != world.LifeRevision ||
				!reflect.DeepEqual(after.MoveSegment, world.MoveSegment) ||
				len(f.rig.pushed[c.Name]) != 0 || len(f.peers[c.Name]) != 0 {
				t.Fatal("failed return changed life, placement, anchor or publication")
			}
			if f.rig.rt.flagWarMatchFor(testDivision) != f.match {
				t.Fatal("a failed evacuation discarded the occupied match")
			}
			if lease, open := f.rig.rt.Monsters.PopulationLease(testDivision, f.match.lease.ID); !open || lease != f.match.lease {
				t.Fatal("a failed return released its occupied layer")
			}
			if lease, admitted := f.rig.rt.EntryPopulationLease(testDivision, c.Name); !admitted || lease != f.match.lease {
				t.Fatal("a failed return lost its original admission")
			}
			f.rig.rt.deps = deps
			f.rig.rt.TickHook()(f.rig.clock.NowMs() + 1)
			f.requireOrdinaryReturn(t, c, before, world)
			if f.rig.rt.flagWarMatchFor(testDivision) != nil {
				t.Fatal("successful retry kept a released match")
			}
			committed, returnedWorld := c.Snapshot(), f.world(c)
			f.rig.rt.TickHook()(f.rig.clock.NowMs() + 2)
			after = f.world(c)
			if !reflect.DeepEqual(c.Snapshot(), committed) || after.Spawn != returnedWorld.Spawn ||
				after.LifeRevision != returnedWorld.LifeRevision || !reflect.DeepEqual(after.MoveSegment, returnedWorld.MoveSegment) {
				t.Fatal("a later close tick committed the return twice")
			}
		})
	}
}

/*
================
TestFlagWarReturnToFortressAddsNoRevivalGrace

The captured destination's siege duration must never turn a plain return
into a revival grant. Teleport grace is off for both living and dead actors.
================
*/
func TestFlagWarReturnToFortressAddsNoRevivalGrace(t *testing.T) {
	for _, dead := range []bool{false, true} {
		t.Run(fmt.Sprintf("dead-%v", dead), func(t *testing.T) {
			f := newFlagWarRebirthFixture(t)
			c := f.players[0]
			if dead {
				f.settleCorpse(c, pk.DeathMonster)
			} else {
				c.CurrentHP, c.CurrentMP = testInt64(17), testInt64(9)
			}
			fortress := instance.Pack(2, 1)
			if _, open := f.rig.rt.Monsters.PopulationLease(testDivision, fortress); !open {
				if _, status := f.rig.rt.Monsters.AllocatePopulation(testDivision, fortress); status != instance.Success {
					t.Fatalf("allocate permanent fortress population: %d", status)
				}
			}
			c.World.InstanceReturn.PackedInstance = uint32(fortress)
			before, world := c.Snapshot(), f.world(c)
			f.rig.rt.CloseInstanceWorld(testDivision, f.match.lease)
			f.requireOrdinaryReturn(t, c, before, world)
			f.rig.clock.Advance(11000 * time.Millisecond)
			f.rig.rt.TickHook()(f.rig.clock.NowMs())
			if c.NativeBodyStatus != before.NativeBodyStatus || c.BodyStatusOwner != before.BodyStatusOwner {
				t.Fatal("a plain return acquired a delayed revival grant")
			}
		})
	}
}
