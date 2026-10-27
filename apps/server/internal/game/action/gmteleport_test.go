/*
===========================================================================

gmteleport_test.go - /GOTOWN, /TOTOWN, /MOVETOUSER and /RECALLUSER

Each command moves exactly one character, answers [1, sub] when it did and
[2, sub] when it did not, and leaves every other record where it was.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
gmTeleportFixture

A GM and a second player, both in the world. Frames are collected per
character name.
================
*/
type gmTeleportFixture struct {
	rt     *Runtime
	clock  *fakeClock
	deps   *enterworld.Deps
	gm     *enterworld.Character
	target *enterworld.Character
	sent   map[string][]wire.Frame
}

func newGMTeleportFixture(t *testing.T) *gmTeleportFixture {
	t.Helper()
	gm := rebirthTestCharacter(20, 100)
	gm.GMPrivilege = true
	target := rebirthTestCharacter(20, 100)
	target.ID, target.Name = 4, "Target"
	rt, clock := newTestRuntime(gm, testItems())
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	rt.Monsters.StartDivision(testDivision)
	deps := rt.deps.(*enterworld.Deps)
	characters := deps.Characters.(enterworld.StaticCharacterSource)
	characters[testDivision] = append(characters[testDivision], target)
	deps.CanEnterWorldRegion = func(*enterworld.Character, uint16) bool { return true }
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	f := &gmTeleportFixture{rt: rt, clock: clock, deps: deps, gm: gm, target: target, sent: map[string][]wire.Frame{}}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) { f.sent[name] = append(f.sent[name], frames...) }
	for session, c := range []*enterworld.Character{gm, target} {
		if err := rt.admitPopulationSession(testDivision, c.Name, uint64(session+1)); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

/*
================
command
================
*/
func (f *gmTeleportFixture) command(subcmd uint8, name string) gmcommand.Outcome {
	frame := []byte{subcmd}
	// /GOTOWN is the subcommand alone; the others carry a sized name.
	if subcmd != gmcommand.SubGoTown {
		frame = binary.LittleEndian.AppendUint16(frame, uint16(len(name)))
		frame = append(frame, name...)
	}
	return gmcommand.HandleGmCommand(f.deps, nil, testDivision, f.gm, frame, f.rt)
}

/*
================
spawnOf
================
*/
func spawnOf(c *enterworld.Character) simulation.Spawn {
	return missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{})
}

/*
================
movedOnly

The named character was re-entered at want; the other one saw nothing and
kept its spawn.
================
*/
func (f *gmTeleportFixture) movedOnly(t *testing.T, moved, still *enterworld.Character, stillAt, want simulation.Spawn) {
	t.Helper()
	frames := f.sent[moved.Name]
	if len(frames) == 0 || frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("%s was not re-entered: %+v", moved.Name, frames)
	}
	if got := spawnOf(moved); got != want {
		t.Fatalf("%s at %+v, want %+v", moved.Name, got, want)
	}
	if len(f.sent[still.Name]) != 0 || spawnOf(still) != stillAt {
		t.Fatalf("%s moved too", still.Name)
	}
}

/*
================
TestGMGoTownMovesTheGMToItsTown
================
*/
func TestGMGoTownMovesTheGMToItsTown(t *testing.T) {
	f := newGMTeleportFixture(t)
	installMidMove(f.rt, f.gm, f.clock)
	town := f.rt.appointedRebirth(testDivision, f.gm).spawn
	targetAt := spawnOf(f.target)
	if out := f.command(gmcommand.SubGoTown, ""); !bytes.Equal(out.Ack, []byte{1, gmcommand.SubGoTown}) {
		t.Fatalf("ack % X (%s)", out.Ack, out.Refusal)
	}
	f.movedOnly(t, f.gm, f.target, targetAt, town)
	if f.gm.World.MoveSegment != nil {
		t.Fatal("the run survived the teleport")
	}
}

/*
================
TestGMToTownMovesTheTargetToItsOwnTown
================
*/
func TestGMToTownMovesTheTargetToItsOwnTown(t *testing.T) {
	f := newGMTeleportFixture(t)
	f.target.World.RebirthPoint = nil
	town := f.rt.appointedRebirth(testDivision, f.target).spawn
	gmAt := spawnOf(f.gm)
	if out := f.command(gmcommand.SubToTown, "target"); !bytes.Equal(out.Ack, []byte{1, gmcommand.SubToTown}) {
		t.Fatalf("ack % X (%s)", out.Ack, out.Refusal)
	}
	f.movedOnly(t, f.target, f.gm, gmAt, town)
}

/*
================
TestGMMoveToUserJoinsTheTargetWhereItStands

The GM lands where the target is now, mid-run, not where it was saved.
================
*/
func TestGMMoveToUserJoinsTheTargetWhereItStands(t *testing.T) {
	f := newGMTeleportFixture(t)
	installMidMove(f.rt, f.target, f.clock)
	key := simulation.WorldKey(testDivision, f.target.Name)
	standing := f.rt.liveSpawn(key, f.target, f.clock.NowMs())
	if standing == spawnOf(f.target) {
		t.Fatal("fixture: the target is not mid-run")
	}
	targetAt := spawnOf(f.target)
	if out := f.command(gmcommand.SubMoveToUser, "Target"); !bytes.Equal(out.Ack, []byte{1, gmcommand.SubMoveToUser}) {
		t.Fatalf("ack % X (%s)", out.Ack, out.Refusal)
	}
	f.movedOnly(t, f.gm, f.target, targetAt, standing)
}

/*
================
TestGMRecallUserBringsTheTargetToTheGM
================
*/
func TestGMRecallUserBringsTheTargetToTheGM(t *testing.T) {
	f := newGMTeleportFixture(t)
	installMidMove(f.rt, f.gm, f.clock)
	standing := f.rt.liveSpawn(simulation.WorldKey(testDivision, f.gm.Name), f.gm, f.clock.NowMs())
	gmAt := spawnOf(f.gm)
	if out := f.command(gmcommand.SubRecallUser, "Target"); !bytes.Equal(out.Ack, []byte{1, gmcommand.SubRecallUser}) {
		t.Fatalf("ack % X (%s)", out.Ack, out.Refusal)
	}
	f.movedOnly(t, f.target, f.gm, gmAt, standing)
}

/*
================
TestGMPlayerTeleportRefusals

Every refusal answers [2, sub] and moves nobody.
================
*/
func TestGMPlayerTeleportRefusals(t *testing.T) {
	cases := []struct {
		name    string
		subcmd  uint8
		target  string
		prepare func(*gmTeleportFixture)
	}{
		{"unknown target", gmcommand.SubToTown, "Ghost", nil},
		{"empty name", gmcommand.SubMoveToUser, "", nil},
		{"offline target", gmcommand.SubRecallUser, "Target", func(f *gmTeleportFixture) {
			f.rt.characterAdmissions.Delete(simulation.WorldKey(testDivision, f.target.Name))
		}},
		{"dead target recalled", gmcommand.SubRecallUser, "Target", func(f *gmTeleportFixture) { *f.target.CurrentHP = 0 }},
		{"dead target sent to town", gmcommand.SubToTown, "Target", func(f *gmTeleportFixture) { *f.target.CurrentHP = 0 }},
		{"target riding", gmcommand.SubRecallUser, "Target", func(f *gmTeleportFixture) {
			f.target.ActiveCOS = &enterworld.CharacterCOS{Summoned: true}
		}},
		{"dead GM", gmcommand.SubMoveToUser, "Target", func(f *gmTeleportFixture) { *f.gm.CurrentHP = 0 }},
		{"dead GM to town", gmcommand.SubGoTown, "", func(f *gmTeleportFixture) { *f.gm.CurrentHP = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newGMTeleportFixture(t)
			if tc.prepare != nil {
				tc.prepare(f)
			}
			gmAt, targetAt := spawnOf(f.gm), spawnOf(f.target)
			out := f.command(tc.subcmd, tc.target)
			if !bytes.Equal(out.Ack, []byte{gmcommand.AckResultFail, tc.subcmd}) {
				t.Fatalf("ack % X", out.Ack)
			}
			if len(f.sent) != 0 || spawnOf(f.gm) != gmAt || spawnOf(f.target) != targetAt {
				t.Fatal("a refused teleport moved someone")
			}
		})
	}
}

/*
================
TestGMPlayerTeleportNeedsPrivilege

A non-GM's frame is silent and moves nobody.
================
*/
func TestGMPlayerTeleportNeedsPrivilege(t *testing.T) {
	for _, subcmd := range []uint8{gmcommand.SubGoTown, gmcommand.SubToTown, gmcommand.SubMoveToUser, gmcommand.SubRecallUser} {
		f := newGMTeleportFixture(t)
		f.gm.GMPrivilege = false
		gmAt, targetAt := spawnOf(f.gm), spawnOf(f.target)
		if out := f.command(subcmd, "Target"); out.Ack != nil || !out.PrivilegeDenied {
			t.Fatalf("0x%02X: %+v", subcmd, out)
		}
		if len(f.sent) != 0 || spawnOf(f.gm) != gmAt || spawnOf(f.target) != targetAt {
			t.Fatalf("0x%02X moved someone", subcmd)
		}
	}
}

/*
================
TestGMMoveToNpcMovesToRosterOrMonsterNest

The native GM move-to-NPC command resolves the NPC or monster nest by codename
and teleports the GM to its coordinates when both live in the default
world.
================
*/
func TestGMMoveToNpcMovesToRosterOrMonsterNest(t *testing.T) {
	f := newGMTeleportFixture(t)
	npcSpawn := simulation.Spawn{RegionID: 0x655e, X: 310, Y: 20, Z: 420}
	f.rt.NpcRoster = []simulation.NpcDef{{
		ObjectID: 9001,
		Codename: "NPC_CH_SMITH",
		Spawn:    npcSpawn,
	}}
	targetAt := spawnOf(f.target)
	if out := f.command(gmcommand.SubMoveToNPC, "npc_ch_smith"); !bytes.Equal(out.Ack, []byte{1, gmcommand.SubMoveToNPC}) {
		t.Fatalf("ack % X (%s)", out.Ack, out.Refusal)
	}
	f.movedOnly(t, f.gm, f.target, targetAt, npcSpawn)

	// Unknown codename refuses with [2, 0x1F] and moves nobody.
	f.sent = map[string][]wire.Frame{}
	gmAt := spawnOf(f.gm)
	if out := f.command(gmcommand.SubMoveToNPC, "NPC_DOES_NOT_EXIST"); !bytes.Equal(out.Ack, []byte{2, gmcommand.SubMoveToNPC}) {
		t.Fatalf("unknown npc ack % X", out.Ack)
	}
	if len(f.sent) != 0 || spawnOf(f.gm) != gmAt {
		t.Fatal("unknown npc moved someone")
	}
}

/*
================
TestGMInstanceOpensAndClosesTimedLayers

The native GM layer command: layer == 0 opens the first
free layer on a timed instance world (and refuses permanent world 1);
layer > 0 evacuates players inside that layer to their return point and closes
the population.
================
*/
func TestGMInstanceOpensAndClosesTimedLayers(t *testing.T) {
	f := newGMTeleportFixture(t)
	// Permanent world 1 cannot open a dynamic layer (native check: NativeType != 0).
	openWorld1 := []byte{gmcommand.SubOpenWorld, 1, 0, 0, 0}
	if out := gmcommand.HandleGmCommand(f.deps, nil, testDivision, f.gm, openWorld1, f.rt); !bytes.Equal(out.Ack, []byte{2, gmcommand.SubOpenWorld}) {
		t.Fatalf("world 1 open ack = % X", out.Ack)
	}
	// Timed dungeon world 10 (INS_QNO_RM_OLDWOMAN_6) opens layer 1 when layer == 0.
	openDungeon := []byte{gmcommand.SubOpenWorld, 10, 0, 0, 0}
	if out := gmcommand.HandleGmCommand(f.deps, nil, testDivision, f.gm, openDungeon, f.rt); !bytes.Equal(out.Ack, []byte{1, gmcommand.SubOpenWorld}) {
		t.Fatalf("world 10 open ack = % X (%s)", out.Ack, out.Refusal)
	}
	lease := instance.Pack(10, 1)
	// Move Target into the opened instance layer and close it with layer == 1.
	saved := *f.target.World.SavedReturn
	wantReturn := simulation.Spawn{RegionID: saved.RegionID, X: float64(saved.X), Y: float64(saved.Y), Z: float64(saved.Z)}
	if !f.rt.relocateCharacter(testDivision, f.target, "test-enter-dungeon", func() (travelPoint, bool) {
		return travelPoint{spawn: spawnOf(f.target), world: lease}, true
	}) {
		t.Fatal("failed to relocate target into instance layer")
	}
	closeDungeon := []byte{gmcommand.SubOpenWorld, 10, 0, 1, 0}
	if out := gmcommand.HandleGmCommand(f.deps, nil, testDivision, f.gm, closeDungeon, f.rt); !bytes.Equal(out.Ack, []byte{1, gmcommand.SubOpenWorld}) {
		t.Fatalf("world 10 close ack = % X (%s)", out.Ack, out.Refusal)
	}
	if f.target.World.PackedInstance != nil || spawnOf(f.target) != wantReturn {
		t.Fatalf("target not evacuated to saved return: packed=%v spawn=%+v want=%+v", f.target.World.PackedInstance, spawnOf(f.target), wantReturn)
	}
	// Closing an already closed layer refuses with [2, 0x13].
	if out := gmcommand.HandleGmCommand(f.deps, nil, testDivision, f.gm, closeDungeon, f.rt); !bytes.Equal(out.Ack, []byte{2, gmcommand.SubOpenWorld}) {
		t.Fatalf("second close ack = % X", out.Ack)
	}
}
