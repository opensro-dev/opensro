/*
===========================================================================

skillaccept_test.go - authoritative combat behavior and state transitions

Exercise the production combat lane, including its committed HP and wire results.

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opSkillEffectCtrlB505 uint16 = 0xB505
	skillCastFinalizeLen  int    = 6
)

/*
================
TestBasicAttackEngagePersistsAcrossTheRetailActionBracket
================
*/
func TestBasicAttackEngagePersistsAcrossTheRetailActionBracket(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	engage := wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()

	first := rt.HandleTargetInteract(testDivision, character, engage)
	firstToken, firstDamage, fatal := assertSkillDamageOpen(
		t, first.Frames, 2, enterworld.ObjectIDForCharacter(character), target.Gid,
	)
	if firstDamage == 0 || fatal {
		t.Fatalf("first base strike = damage %d fatal %v, want positive nonfatal", firstDamage, fatal)
	}
	if len(rt.combatIntentSnapshot()) != 1 {
		t.Fatal("accepted engage did not persist one authoritative combat intent")
	}

	assertOnlySkillReleases(t, rt.TickHook()(clock.NowMs()))
	routed := rt.TickHook()(clock.At(testBasicAttackActionDuration).UnixMilli())
	assertSkillCastClose(t, routed, testDivision, firstToken)
	if len(routed) != 1 || routed[0].DivisionID != testDivision || len(routed[0].Frames) != 1 {
		t.Fatalf("release tick = %+v, want exactly one B505 and no same-turn B245 reacquire", routed)
	}

	// The retained engage may reacquire only after this published free boundary.
	routed = rt.TickHook()(clock.At(testBasicAttackActionDuration + time.Millisecond).UnixMilli())
	if len(routed) != 1 || routed[0].DivisionID != testDivision || len(routed[0].Frames) != 1 {
		t.Fatalf("next-turn repeat = %+v, want one B245 after the release boundary", routed)
	}
	_, secondDamage, secondFatal := assertSkillDamageOpen(t,
		[]wire.Frame{{Opcode: routed[0].Frames[0].Opcode, Payload: routed[0].Frames[0].Payload}},
		2, enterworld.ObjectIDForCharacter(character), target.Gid)
	if secondDamage == 0 || secondFatal {
		t.Fatalf("second base strike = damage %d fatal %v, want positive nonfatal", secondDamage, secondFatal)
	}
}

// Fresh records intentionally omit currentHp: nil is the compact persisted
// representation of a full gauge. Exercise the exact native 0x72CD owner so
// no gameplay refactor can regress to treating that representation as dead.
/*
================
TestBasicAttackEngageTreatsAbsentCurrentHPAsFull
================
*/
func TestBasicAttackEngageTreatsAbsentCurrentHPAsFull(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	character.CurrentHP = nil

	result := rt.HandleTargetInteract(
		testDivision,
		character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode(),
	)
	if result.DiagnosticRefusal != "" {
		t.Fatalf("fresh-character base strike diagnostic refusal = %q", result.DiagnosticRefusal)
	}
	_, damage, fatal := assertSkillDamageOpen(
		t, result.Frames, 2, enterworld.ObjectIDForCharacter(character), target.Gid,
	)
	if damage == 0 || fatal {
		t.Fatalf("fresh-character base strike = damage %d fatal %v, want positive nonfatal", damage, fatal)
	}
	if character.CurrentHP != nil {
		t.Fatal("attacking materialized the actor's compact full-HP representation")
	}
}

/*
================
TestBasicAttackFreshEngageReplacesAReleasedPursuit
================
*/
func TestBasicAttackFreshEngageReplacesAReleasedPursuit(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	*character.World.Spawn.X = 900
	engage := wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()

	first := rt.HandleTargetInteract(testDivision, character, engage)
	first = assertAndSeparateActionSession(t, first)
	if len(first.Frames) != 1 || first.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("first engage = %04X, want one B738 pursuit", opcodesOf(first.Frames))
	}
	rt.ClearCombatIntent(testDivision, character.Name)
	if intents := rt.combatIntentSnapshot(); len(intents) != 0 {
		t.Fatalf("manual-move ownership release retained stale combat intent: %+v", intents)
	}

	// A later double-click is a new command, not a continuation of the released
	// pursuit. It must create a fresh intent even when the authoritative world
	// still contains the former approach segment.
	second := rt.HandleTargetInteract(testDivision, character, engage)
	if len(second.Frames) != 1 || second.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("fresh re-engage = %04X, want a new B738 pursuit", opcodesOf(second.Frames))
	}
	intents := rt.combatIntentSnapshot()
	if len(intents) != 1 || intents[0].TargetGid != target.Gid || !intents[0].HasApproach {
		t.Fatalf("fresh re-engage intent = %+v, want one active pursuit for gid %d", intents, target.Gid)
	}
}

/*
================
TestBasicAttackTransitionSamplesOneAuthoritativeInstant
================
*/
func TestBasicAttackTransitionSamplesOneAuthoritativeInstant(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	clockReads := 0
	rt.Now = func() time.Time {
		clockReads++
		return clock.Now()
	}

	result := rt.HandleTargetInteract(testDivision, character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	assertSkillDamageOpen(t, result.Frames, 2, enterworld.ObjectIDForCharacter(character), target.Gid)
	if clockReads != 1 {
		t.Fatalf("one basic-attack transition sampled the world clock %d times, want exactly 1", clockReads)
	}
}

/*
================
TestBasicAttackTransitionCommitsFacingBeforeB245
================
*/
func TestBasicAttackTransitionCommitsFacingBeforeB245(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	*character.World.Spawn.Angle = 0x8000 // west, while the target is east

	result := rt.HandleTargetInteract(testDivision, character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	result = assertAndSeparateActionSession(t, result)
	if len(result.Frames) != 2 || result.Frames[0].Opcode != wire.OpObjectSourceCorrection ||
		result.Frames[1].Opcode != wire.OpSkillCastResult {
		t.Fatalf("wrong-facing attack frames = %04X, want B2F5 before B245", opcodesOf(result.Frames))
	}
	correction, err := wire.DecodeObjectSourceCorrection(result.Frames[0].Payload)
	if err != nil {
		t.Fatalf("decode player attack-facing correction: %v", err)
	}
	want, ok := simulation.HeadingFromMovement(
		simulation.Spawn{RegionID: uint16(*character.World.Spawn.RegionID), X: *character.World.Spawn.X, Y: *character.World.Spawn.Y, Z: *character.World.Spawn.Z},
		simulation.Spawn{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z},
	)
	if !ok || correction.Heading != want {
		t.Fatalf("player attack-facing heading = %#04x, want live target bearing %#04x", correction.Heading, want)
	}
	if got := uint16(*character.World.Spawn.Angle); got != want {
		t.Fatalf("authoritative character heading = %#04x, want committed %#04x", got, want)
	}
	if len(result.Broadcast) != 2 || result.Broadcast[0].Opcode != wire.OpObjectSourceCorrection ||
		result.Broadcast[1].Opcode != wire.OpSkillCastResult {
		t.Fatalf("peer attack transition = %04X, want same ordered B2F5/B245 contract", opcodesOf(result.Broadcast))
	}
	assertSkillStationaryTarget(t, result.Frames[1].Payload)
}

/*
================
TestBasicAttackTransitionUsesRegionAwareTargetBearing
================
*/
func TestBasicAttackTransitionUsesRegionAwareTargetBearing(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	playerRegion := uint16(*character.World.Spawn.RegionID)
	targetRegion := simulation.RegionIDForSectors(
		simulation.SectorX(playerRegion)+1,
		simulation.SectorY(playerRegion),
	)
	*character.World.Spawn.X = simulation.NativeRegionSize - 2
	*character.World.Spawn.Z = 458
	*character.World.Spawn.Angle = 0x8000
	mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
	if !ok {
		t.Fatal("cross-region target mover missing")
	}
	mover.Pose.RegionID = targetRegion
	mover.Pose.X = 3
	mover.Pose.Z = 458
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	if !rt.Monsters.CommitMover(testDivision, target.Gid, mover) {
		t.Fatal("commit cross-region target pose")
	}

	result := rt.HandleTargetInteract(testDivision, character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	result = assertAndSeparateActionSession(t, result)
	if len(result.Frames) != 2 || result.Frames[0].Opcode != wire.OpObjectSourceCorrection ||
		result.Frames[1].Opcode != wire.OpSkillCastResult {
		t.Fatalf("cross-region player attack frames = %04X, want B2F5 before B245", opcodesOf(result.Frames))
	}
	correction, err := wire.DecodeObjectSourceCorrection(result.Frames[0].Payload)
	if err != nil {
		t.Fatalf("decode cross-region player correction: %v", err)
	}
	want, ok := simulation.HeadingFromMovement(
		simulation.Spawn{RegionID: playerRegion, X: simulation.NativeRegionSize - 2, Y: 20, Z: 458},
		simulation.Spawn{RegionID: targetRegion, X: 3, Y: 20, Z: 458},
	)
	if !ok || correction.Heading != want {
		t.Fatalf("cross-region player attack heading = %#04x, want %#04x", correction.Heading, want)
	}
	assertSkillStationaryTarget(t, result.Frames[1].Payload)
}

/*
================
TestBasicAttackEngageApproachesBeforeStriking
================
*/
func TestBasicAttackEngageApproachesBeforeStriking(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	*character.World.Spawn.X = 900

	approach := rt.HandleTargetInteract(testDivision, character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	approach = assertAndSeparateActionSession(t, approach)
	if len(approach.Frames) != 1 || approach.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("out-of-range engage = %+v, want authoritative B738 approach", approach)
	}
	if len(rt.combatIntentSnapshot()) != 1 {
		t.Fatal("approach did not retain the attack intent")
	}
	if current, ok := rt.Monsters.Get(testDivision, target.Gid); !ok || current.CurrentHP != target.CurrentHP {
		t.Fatalf("approach changed HP before range admission: %+v/%v", current, ok)
	}

	clock.Advance(10 * time.Second)
	routed := rt.TickHook()(clock.NowMs())
	if len(routed) != 1 || len(routed[0].Frames) != 2 ||
		routed[0].Frames[0].Opcode != wire.OpObjectSourceCorrection ||
		routed[0].Frames[1].Opcode != wire.OpSkillCastResult {
		t.Fatalf("arrived attack tick = %+v, want ordered pursuit-settle B2F5 then B245", routed)
	}
}

/*
================
TestBasicAttackPursuitKeepsOneLegAndResteersOnBoundedTargetDrift
================
*/
func TestBasicAttackPursuitKeepsOneLegAndResteersOnBoundedTargetDrift(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	*character.World.Spawn.X = 900

	// Start the target on a long authored leg before the engage. The target's
	// future endpoint is private mover state; an identity-bound attack may use
	// only its live pose at the authoritative sample time.
	mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
	if !ok {
		t.Fatal("target mover missing")
	}
	mover.From = mover.Pose
	mover.To = mover.Pose
	mover.To.X += 60
	mover.DepartMs = clock.NowMs()
	mover.ArriveMs = clock.At(time.Second).UnixMilli()
	if !rt.Monsters.CommitMover(testDivision, target.Gid, mover) {
		t.Fatal("failed to commit moving-target fixture")
	}
	clock.Advance(250 * time.Millisecond)

	first := rt.HandleTargetInteract(testDivision, character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	first = assertAndSeparateActionSession(t, first)
	if len(first.Frames) != 1 || first.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("first pursuit = %+v, want one movement goal", first)
	}
	intent := rt.combatIntentSnapshot()[0]
	if !intent.HasApproach {
		t.Fatal("first approach did not own a persistent pursuit leg")
	}
	liveTarget := mover.LivePoseAt(clock.NowMs(), nil)
	if math.Abs(intent.ApproachTargetSample.X-liveTarget.X) > 0.001 {
		t.Fatalf("first approach target X = %.3f, want live target %.3f", intent.ApproachTargetSample.X, liveTarget.X)
	}
	if math.Abs(intent.ApproachTargetSample.X-mover.To.X) < 0.001 {
		t.Fatalf("first approach leaked private target endpoint %.3f", mover.To.X)
	}
	worldKey := simulation.WorldKey(testDivision, character.Name)
	world := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if distance := simulation.WorldDistance2D(world.Spawn, intent.ApproachTargetSample); math.Abs(distance-14) > math.Sqrt2/2 {
		t.Fatalf("first moving-target pursuit stand-off is %.3f, want bodies 4+6 plus inner sword reach 4", distance)
	}

	// Even material target drift cannot replace a goal before the packet floor.
	if routed := rt.TickHook()(clock.Now().Add(99 * time.Millisecond).UnixMilli()); len(routed) != 0 {
		t.Fatalf("young pursuit emitted %+v, want no restarted goal", routed)
	}

	// At the floor, re-author from the target's new live poseâ€”not its endpoint.
	routed := rt.TickHook()(clock.Now().Add(100 * time.Millisecond).UnixMilli())
	if len(routed) != 1 || len(routed[0].Frames) != 1 || routed[0].Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("bounded pursuit re-aim = %+v, want one movement goal", routed)
	}
	intent = rt.combatIntentSnapshot()[0]
	liveTarget = mover.LivePoseAt(clock.Now().Add(100*time.Millisecond).UnixMilli(), nil)
	if math.Abs(intent.ApproachTargetSample.X-liveTarget.X) > 0.001 {
		t.Fatalf("re-aim target X = %.3f, want live target %.3f", intent.ApproachTargetSample.X, liveTarget.X)
	}
	world = rt.Worlds.Snapshot(worldKey, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if distance := simulation.WorldDistance2D(world.Spawn, intent.ApproachTargetSample); math.Abs(distance-14) > math.Sqrt2/2 {
		t.Fatalf("moving-target re-aim stand-off is %.3f, want the same complete spacing contract", distance)
	}
	for _, after := range []time.Duration{10 * time.Millisecond, 50 * time.Millisecond, 99 * time.Millisecond} {
		if again := rt.TickHook()(clock.Now().Add(100*time.Millisecond + after).UnixMilli()); len(again) != 0 {
			t.Fatalf("young replacement pursuit restarted at +%s: %+v", after, again)
		}
	}
}

/*
================
TestBasicAttackPursuitUsesCompleteBodyAndReachSpacing
================
*/
func TestBasicAttackPursuitUsesCompleteBodyAndReachSpacing(t *testing.T) {
	target := simulation.Spawn{RegionID: 0x5c9e, X: 1000, Y: 20, Z: 1000}
	angle := 0.36 * math.Pi / 180
	from := simulation.Spawn{
		RegionID: target.RegionID,
		X:        target.X - math.Cos(angle)*100,
		Y:        target.Y,
		Z:        target.Z - math.Sin(angle)*100,
	}

	spacing := simulation.CombatSpacing{
		ActorBodyRadius: simulation.BodyRadius(4), TargetBodyRadius: simulation.BodyRadius(6),
		ActionReach: simulation.ActionReach(70),
	}
	goal, disposition := spacing.ApproachGoal(from, target)
	if disposition != simulation.CombatApproachMove {
		t.Fatal("out-of-range approach did not produce a goal")
	}
	if goal.X != math.Round(goal.X) || goal.Z != math.Round(goal.Z) {
		t.Fatalf("approach goal (%f,%f) is not the integer 0xB738 destination", goal.X, goal.Z)
	}
	if distance := simulation.WorldDistance2D(goal, target); math.Abs(distance-59) > math.Sqrt2/2 {
		t.Fatalf("settled-target stand-off is %.9f, want bodies plus 70 percent of ranged reach", distance)
	}

	goal, disposition = spacing.ApproachGoal(from, target)
	if disposition != simulation.CombatApproachMove {
		t.Fatal("moving-target approach did not produce a goal")
	}
	if distance := simulation.WorldDistance2D(goal, target); math.Abs(distance-59) > math.Sqrt2/2 {
		t.Fatalf("repeat stand-off is %.9f, want movement state independent of combat geometry", distance)
	}
}

/*
================
TestBasicAttackPursuitDoesNotWaitAtFutureTargetDestination
================
*/
func TestBasicAttackPursuitDoesNotWaitAtFutureTargetDestination(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	*character.World.Spawn.X = 900

	mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
	if !ok {
		t.Fatal("target mover missing")
	}
	mover.From = mover.Pose
	mover.From.X = 930
	mover.To = mover.Pose
	mover.DepartMs = clock.NowMs()
	mover.ArriveMs = clock.At(time.Second).UnixMilli()
	if !rt.Monsters.CommitMover(testDivision, target.Gid, mover) {
		t.Fatal("commit approaching target plan")
	}

	result := rt.HandleTargetInteract(testDivision, character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	result = assertAndSeparateActionSession(t, result)
	if len(result.Frames) != 1 || result.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("moving-target engage emitted %+v, want approach toward its live pose", result.Frames)
	}
	intents := rt.combatIntentSnapshot()
	if len(intents) != 1 || intents[0].TargetGid != target.Gid || !intents[0].HasApproach {
		t.Fatalf("live-target approach discarded engage intent: %+v", intents)
	}
	if got := intents[0].ApproachTargetSample.X; got != mover.From.X {
		t.Fatalf("approach sampled target X %.3f, want current X %.3f (future endpoint %.3f is private)", got, mover.From.X, mover.To.X)
	}

	routed := rt.TickHook()(clock.At(time.Second).UnixMilli())
	if len(routed) != 1 || len(routed[0].Frames) != 1 ||
		routed[0].Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("live target arrival = %+v, want a new bounded live-target approach, not a waited-at-endpoint strike", routed)
	}
}

/*
================
TestBasicAttackPursuitDeadlineMatchesProductionTick
================
*/
func TestBasicAttackPursuitDeadlineMatchesProductionTick(t *testing.T) {
	if time.Duration(attackPursuitResteerMinMs)*time.Millisecond != simulation.DefaultTickInterval {
		t.Fatal("short pursuit goals must not wait behind a slower owner deadline")
	}
}

/*
================
TestBasicAttackBlockedApproachDefersWithoutDiscardingEngage
================
*/
func TestBasicAttackBlockedApproachDefersWithoutDiscardingEngage(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	*character.World.Spawn.X = 900
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		return from, nil
	}

	blocked := rt.HandleTargetInteract(testDivision, character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	blocked = assertAndSeparateActionSession(t, blocked)
	if len(blocked.Frames) != 0 {
		t.Fatalf("no-progress constrained approach = %04X, want no fabricated response", opcodesOf(blocked.Frames))
	}
	if len(rt.combatIntentSnapshot()) != 1 {
		t.Fatal("temporary no-progress constraint discarded the valid engage")
	}

	rt.ConstrainMovement = nil
	clock.Advance(time.Second)
	routed := rt.TickHook()(clock.NowMs())
	if len(routed) != 1 || len(routed[0].Frames) != 1 ||
		routed[0].Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("unblocked deferred engage = %+v, want one B738 approach", routed)
	}
}

/*
================
assertSkillDamageOpen
================
*/
func assertSkillDamageOpen(
	t *testing.T,
	frames []wire.Frame,
	actionID, casterGid, targetGid uint32,
) (token, damage uint32, fatal bool) {
	t.Helper()
	var skillFrames []wire.Frame
	for _, frame := range frames {
		if frame.Opcode == wire.OpSkillCastResult {
			skillFrames = append(skillFrames, frame)
		}
	}
	if len(skillFrames) != 1 {
		t.Fatalf("combat accept frames = %04X, want exactly one B245", opcodesOf(frames))
	}
	payload := skillFrames[0].Payload
	if len(payload) < 34 {
		t.Fatalf("combat B245 = % X (%d bytes), want at least one complete result", payload, len(payload))
	}
	if payload[0] != 1 || payload[1] != 0 ||
		binary.LittleEndian.Uint32(payload[2:]) != actionID ||
		binary.LittleEndian.Uint32(payload[6:]) != casterGid ||
		payload[18] != 1 ||
		payload[19] == 0 || payload[20] != 1 {
		t.Fatalf("combat B245 header/target drifted: % X", payload)
	}
	impactCount := int(payload[19])
	targetCount := int(payload[20])
	if wantLength := 21 + 4*targetCount + 9*impactCount*targetCount; len(payload) != wantLength {
		t.Fatalf("combat B245 = % X (%d bytes), want %d bytes for %d impacts x %d targets", payload, len(payload), wantLength, impactCount, targetCount)
	}
	token = binary.LittleEndian.Uint32(payload[10:])
	if token == 0 {
		t.Fatal("combat B245 minted token zero")
	}
	if gotTarget := binary.LittleEndian.Uint32(payload[21:]); gotTarget != targetGid {
		t.Fatalf("combat B245 target = %#x, want %#x", gotTarget, targetGid)
	}
	for impact := 0; impact < impactCount; impact++ {
		base := 25 + impact*9
		impactFatal := payload[base]&0x80 != 0
		if impactFatal && impact != impactCount-1 {
			t.Fatalf("combat B245 impact %d is fatal with %d later impact(s)", impact, impactCount-impact-1)
		}
		packed := binary.LittleEndian.Uint32(payload[base+1:])
		if flags := uint8(packed); flags != 1 {
			t.Fatalf("normal result impact %d flags = %#x, want 1", impact, flags)
		}
		damage += packed >> 8
		fatal = fatal || impactFatal
	}
	return token, damage, fatal
}

/*
================
assertSkillStationaryTarget
================
*/
func assertSkillStationaryTarget(t *testing.T, payload []byte) {
	t.Helper()
	if len(payload) < 25 || payload[18] != 1 || len(payload) != 25+9*int(payload[19]) {
		t.Fatalf("stationary B245 must contain results without movement steering: % X", payload)
	}
	// Target position/facing is independently checked through B2F5 and the
	// authoritative character pose by the callers. Bit 3 would move the holder.
}

/*
================
assertOnlySkillReleases
================
*/
func assertOnlySkillReleases(t *testing.T, routed []simulation.DivisionFrames) {
	t.Helper()
	for _, route := range routed {
		for _, frame := range route.Frames {
			if frame.Opcode != wire.OpSkillEffectControl || len(frame.Payload) != 10 || frame.Payload[0] != 1 || frame.Payload[9] != 0 {
				t.Fatalf("unexpected frame before action finalization: %+v", frame)
			}
		}
	}
}

/*
================
TestSkillActionCastCommitsAuthoritativeDamageAndTimedBracket
================
*/
func TestSkillActionCastCommitsAuthoritativeDamageAndTimedBracket(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	payload := wire.SkillAction{
		ActionId:  2,
		HasTarget: true,
		TargetGid: target.Gid,
	}.Encode()

	result := rt.HandleTargetInteract(testDivision, character, payload)
	result = assertAndSeparateActionSession(t, result)
	if len(result.Frames) != 1 || len(result.Frames[0].Payload) < 21 || result.Frames[0].Payload[19] != 2 {
		t.Fatalf("sword base result did not preserve its two retail impact stages: %+v", result.Frames)
	}
	token, damage, fatal := assertSkillDamageOpen(
		t,
		result.Frames,
		2,
		enterworld.ObjectIDForCharacter(character),
		target.Gid,
	)
	if fatal || damage == 0 {
		t.Fatalf("ordinary result = damage %d fatal %v, want positive nonfatal", damage, fatal)
	}
	if len(result.Broadcast) != 1 ||
		!bytes.Equal(result.Frames[0].Payload, result.Broadcast[0].Payload) {
		t.Fatal("acting-session and division B245 results diverged")
	}
	committed, ok := rt.Monsters.Get(testDivision, target.Gid)
	if !ok || committed.CurrentHP != target.CurrentHP-damage {
		t.Fatalf("registry HP = %+v/%v, want %d", committed, ok, target.CurrentHP-damage)
	}
	mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
	if !ok || mover.Mode() != monster.MoverChasing ||
		mover.TargetGID() != enterworld.ObjectIDForCharacter(character) ||
		!mover.RetaliationPending() {
		t.Fatalf("post-hit retaliation mover = %+v/%v, want armed chase of attacker", mover, ok)
	}
	duplicate := rt.HandleTargetInteract(testDivision, character, payload)
	assertQueuedAction(t, duplicate)
	rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Cancel: true}.Encode())
	stillCommitted, ok := rt.Monsters.Get(testDivision, target.Gid)
	if !ok || stillCommitted.CurrentHP != committed.CurrentHP {
		t.Fatalf("refused overlapping cast changed HP: %+v/%v", stillCommitted, ok)
	}

	beforeDeadline := clock.At(testBasicAttackActionDuration - time.Millisecond).UnixMilli()
	assertOnlySkillReleases(t, rt.TickHook()(beforeDeadline))
	assertSkillCastClose(
		t,
		rt.TickHook()(clock.At(testBasicAttackActionDuration).UnixMilli()),
		testDivision,
		token,
	)
	if routed := rt.TickHook()(clock.At(testBasicAttackActionDuration + time.Second).UnixMilli()); len(routed) != 0 {
		t.Fatalf("finalize emitted twice: %+v", routed)
	}
}

/*
================
TestFatalSkillResultPublishesBeforeDefeatLifecycle
================
*/
func TestFatalSkillResultPublishesBeforeDefeatLifecycle(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 1)
	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())

	token, damage, fatal := assertSkillDamageOpen(
		t,
		result.Frames,
		2,
		enterworld.ObjectIDForCharacter(character),
		target.Gid,
	)
	if damage != 6 || !fatal {
		t.Fatalf("fatal result = damage %d fatal %v, want committed full hit 6/true", damage, fatal)
	}
	zeroHP, ok := rt.Monsters.Get(testDivision, target.Gid)
	if !ok || zeroHP.CurrentHP != 0 {
		t.Fatalf("fatal target before result finalize = %+v/%v, want present at zero HP", zeroHP, ok)
	}

	assertSkillCastClose(
		t,
		rt.TickHook()(clock.At(testBasicAttackActionDuration).UnixMilli()),
		testDivision,
		token,
	)
	if retained, ok := rt.Monsters.Get(testDivision, target.Gid); !ok || retained.CurrentHP != 0 {
		t.Fatalf("fatal source after B505 = %+v/%v, want retained through its death presentation", retained, ok)
	}
	if routed := rt.TickHook()(clock.At(testBasicAttackActionDuration + time.Millisecond).UnixMilli()); len(routed) != 0 {
		t.Fatalf("post-reward lifecycle tick broadcast frames: %+v", routed)
	}
	if retained, ok := rt.Monsters.Get(testDivision, target.Gid); !ok || retained.CurrentHP != 0 {
		t.Fatalf("reward source during death presentation = %+v/%v, want retained at zero HP", retained, ok)
	}
	retireAt := monsterDeathPresentationRetention
	if routed := rt.TickHook()(clock.At(retireAt).UnixMilli()); len(routed) != 0 {
		t.Fatalf("death-presentation retirement tick broadcast frames: %+v", routed)
	}
	if _, ok := rt.Monsters.Get(testDivision, target.Gid); ok {
		t.Fatal("fatal target still resolves after its death-presentation retention elapsed")
	}
}

/*
================
TestSkillActionCastTokensAreUniquePerCommittedCast
================
*/
func TestSkillActionCastTokensAreUniquePerCommittedCast(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	payload := wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode()

	first := rt.HandleTargetInteract(testDivision, character, payload)
	rt.TickHook()(clock.At(testBasicAttackActionDuration).UnixMilli())
	clock.Advance(testBasicAttackActionDuration)
	second := rt.HandleTargetInteract(testDivision, character, payload)
	firstToken := binary.LittleEndian.Uint32(first.Frames[0].Payload[10:])
	secondToken := binary.LittleEndian.Uint32(second.Frames[0].Payload[10:])
	if firstToken == secondToken {
		t.Fatalf("two casts minted the same token 0x%X", firstToken)
	}
}

/*
================
TestUnsupportedSkillShapesDoNotReceiveVisualOnlySuccess
================
*/
func TestUnsupportedSkillShapesDoNotReceiveVisualOnlySuccess(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	shapes := map[string]wire.SkillAction{
		"no target": {ActionId: 2},
		"unknown skill": {
			ActionId: 999, HasTarget: true, TargetGid: target.Gid,
		},
		"ground target": {
			ActionId:        2,
			HasGroundTarget: true,
			Region:          target.Spawn.RegionID,
		},
	}
	for name, cast := range shapes {
		t.Run(name, func(t *testing.T) {
			result := rt.HandleTargetInteract(testDivision, character, cast.Encode())
			if len(result.Frames) != 0 || len(result.Broadcast) != 0 ||
				result.Pending != nil {
				t.Fatalf("unsupported cast answered visual-only success: %+v", result)
			}
		})
	}
}

/*
================
TestCombatAdmissionFailsClosedBeforeHPMutation
================
*/
func TestCombatAdmissionFailsClosedBeforeHPMutation(t *testing.T) {
	tests := map[string]func(*Runtime, *enterworld.Character){
		"dead caster": func(_ *Runtime, character *enterworld.Character) {
			character.CurrentHP = testInt64(0)
		},
		"unlearned skill": func(_ *Runtime, character *enterworld.Character) {
			character.Skills = nil
		},
		"out of range": func(_ *Runtime, character *enterworld.Character) {
			*character.World.Spawn.X = 900
		},
		"unpinned item option": func(_ *Runtime, character *enterworld.Character) {
			character.MissionInventory[0].MagicOptions = []uint64{1}
		},
		// An empty ammunition weapon is not silent: 58E32D answers 0x300E
		// (TestBasicAttackWithoutAmmunitionReportsTheNotice).
	}
	for name, arrange := range tests {
		t.Run(name, func(t *testing.T) {
			rt, _, character, target := newCombatTestRuntime(t, 100)
			arrange(rt, character)

			result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
				ActionId: 2, HasTarget: true, TargetGid: target.Gid,
			}.Encode())
			if len(result.Frames) != 0 || len(result.Broadcast) != 0 {
				t.Fatalf("incomplete admission answered %+v, want silence", result)
			}
			after, ok := rt.Monsters.Get(testDivision, target.Gid)
			if !ok || after.CurrentHP != target.CurrentHP {
				t.Fatalf("refused admission changed target HP: %+v/%v", after, ok)
			}
		})
	}
}

/*
================
TestRangedBasicAttackConsumesRetailAmmunitionSocketAtomically
================
*/
func TestRangedBasicAttackConsumesRetailAmmunitionSocketAtomically(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[character.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 6
	weapon.Combat.ActionRange = 180
	character.MissionInventory[0].TypeFlags = weapon.TypeFlags()

	arrow := &enterworld.ItemRef{
		RefObjID: 62_001,
		Codename: "ITEM_ETC_AMMO_ARROW_01",
		TypeIDs:  [4]int64{3, 3, 4, 1},
	}
	items[arrow.Codename] = arrow
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot:       7,
		RefObjID:   arrow.RefObjID,
		Codename:   arrow.Codename,
		TypeFlags:  arrow.TypeFlags(),
		StackCount: 2,
	})
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Codename = "SKILL_CH_BOW_BASE_01"
	skill.RequiredWeaponKinds = [2]uint8{6, 0xff}
	skills[2] = skill

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	result = assertAndSeparateActionSession(t, result)
	if len(result.Frames) != 2 || result.Frames[1].Opcode != wire.OpAvatarInventorySlot7StackCount ||
		!bytes.Equal(result.Frames[1].Payload, []byte{1, 0}) {
		t.Fatalf("ranged actor frames = %+v, want B245 then private 3752 count=1", result.Frames)
	}
	assertSkillDamageOpen(t, result.Frames[:1], 2,
		enterworld.ObjectIDForCharacter(character), target.Gid)
	if len(result.Broadcast) != 1 || result.Broadcast[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("ranged broadcast = %+v, want B245 only (ammo is private)", result.Broadcast)
	}
	if got := character.MissionInventory[1].StackCount; got != 1 {
		t.Fatalf("arrow stack after committed shot = %d, want 1", got)
	}
}

/*
================
TestWrongRangedAmmunitionRefusesBeforeDamageOrDebit
================
*/
func TestWrongRangedAmmunitionRefusesBeforeDamageOrDebit(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[character.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 6
	weapon.Combat.ActionRange = 180
	character.MissionInventory[0].TypeFlags = weapon.TypeFlags()

	bolt := &enterworld.ItemRef{
		RefObjID: 62_002,
		Codename: "ITEM_ETC_AMMO_BOLT_01",
		TypeIDs:  [4]int64{3, 3, 4, 2},
	}
	items[bolt.Codename] = bolt
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: bolt.RefObjID, Codename: bolt.Codename,
		TypeFlags: bolt.TypeFlags(), StackCount: 2,
	})
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Codename = "SKILL_CH_BOW_BASE_01"
	skill.RequiredWeaponKinds = [2]uint8{6, 0xff}
	skills[2] = skill

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	// 58E32D: the basic attack's cnsm requirement refuses 0x300E, the
	// out-of-ammunition notice, before any damage or debit.
	assertAmmunitionRefusal(t, result)
	if character.MissionInventory[1].StackCount != 2 {
		t.Fatalf("wrong ammunition debited: %+v", character.MissionInventory)
	}
	after, ok := rt.Monsters.Get(testDivision, target.Gid)
	if !ok || after.CurrentHP != target.CurrentHP {
		t.Fatalf("wrong ammunition changed HP: %+v/%v", after, ok)
	}
}

/*
================
TestBasicAttackWithoutAmmunitionReportsTheNotice

Players saw no notice when a double-click attacked with an empty bow while
a skill did; native refuses both with 0x300E.
================
*/
func TestBasicAttackWithoutAmmunitionReportsTheNotice(t *testing.T) {
	for _, ranged := range []struct {
		name  string
		kind  int64
		skill string
		race  int64
	}{
		{"bow", 6, "SKILL_CH_BOW_BASE_01", enterworld.RaceChina},
		{"crossbow", 12, "SKILL_EU_CROSSBOW_BASE_01", enterworld.RaceEurope},
	} {
		t.Run(ranged.name, func(t *testing.T) {
			rt, _, character, target := newCombatTestRuntime(t, 100)
			// The basic attack is the race's own: bolts are European.
			character.ModelCodename = ""
			character.RaceIndex = testInt64(ranged.race)
			items := rt.deps.ItemReferences().(staticItemSource)
			weapon := items[character.MissionInventory[0].Codename]
			weapon.TypeIDs[3] = ranged.kind
			weapon.Combat.ActionRange = 180
			character.MissionInventory[0].TypeFlags = weapon.TypeFlags()
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.Codename = ranged.skill
			skill.RequiredWeaponKinds = [2]uint8{uint8(ranged.kind), 0xff}
			skills[2] = skill

			result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
				ActionId: 2, HasTarget: true, TargetGid: target.Gid,
			}.Encode())
			assertAmmunitionRefusal(t, result)
			after, ok := rt.Monsters.Get(testDivision, target.Gid)
			if !ok || after.CurrentHP != target.CurrentHP {
				t.Fatalf("an empty %s changed HP: %+v/%v", ranged.name, after, ok)
			}
		})
	}
}

/*
================
assertAmmunitionRefusal

The result carries exactly one B070 refusal with code 0x0E (0x300E).
================
*/
func assertAmmunitionRefusal(t *testing.T, result OpResult) {
	t.Helper()
	var refusals int
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpSkillCastResult && bytes.Equal(frame.Payload, []byte{2, 0x0e}) {
			refusals++
			continue
		}
		if frame.Opcode == wire.OpSkillCastResult {
			t.Fatalf("unexpected cast result %x", frame.Payload)
		}
	}
	if refusals != 1 {
		t.Fatalf("ammunition refusal frames = %+v, want one B070 {2, 0x0E}", result.Frames)
	}
}

/*
================
TestEmptyBowIsRefusedAtThePressOutOfRange

Command acceptance (phase 0x37, 4ACED4) carries the ammo bit 0x20, so an
empty bow pressed at a monster out of range is refused at once, with the
notice, and never walks: neither the basic attack nor a bow skill.
================
*/
func TestEmptyBowIsRefusedAtThePressOutOfRange(t *testing.T) {
	for _, press := range emptyBowPresses {
		t.Run(press, func(t *testing.T) {
			rt, _, c, target, id, _ := emptyBowFixture(t, press)
			result := rt.HandleTargetInteract(testDivision, c, emptyBowPress(press, id, target.Gid))
			assertAmmunitionRefusal(t, result)
			for _, frame := range result.Frames {
				if frame.Opcode == simulation.OpMovementAck {
					t.Fatal("an empty bow walked toward the target")
				}
			}
			if intents := rt.combatIntentSnapshot(); len(intents) != 0 {
				t.Fatalf("an empty bow kept a pursuit: %+v", intents)
			}
			if after, _ := rt.Monsters.Get(testDivision, target.Gid); after.CurrentHP != target.CurrentHP {
				t.Fatal("an empty bow changed HP")
			}
		})
	}
}

/*
================
TestArrivalRefusalReachesOnlyTheActor

A pursuit's refusal is decided on arrival under the simulation tick (here
the arrows were unequipped during the walk). It is the actor's alone: it
must reach the actor beside the tick's public range-entry correction, and
never ride the public route.
================
*/
func TestArrivalRefusalReachesOnlyTheActor(t *testing.T) {
	for _, press := range emptyBowPresses {
		t.Run(press, func(t *testing.T) {
			rt, clock, c, target, id, arrow := emptyBowFixture(t, press)
			c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 7, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: 5})
			result := rt.HandleTargetInteract(testDivision, c, emptyBowPress(press, id, target.Gid))
			walked := false
			for _, frame := range result.Frames {
				walked = walked || frame.Opcode == simulation.OpMovementAck
				if frame.Opcode == wire.OpSkillCastResult {
					t.Fatalf("refused before the walk: %x", frame.Payload)
				}
			}
			if !walked {
				t.Fatalf("the press did not walk: %+v", result)
			}
			c.MissionInventory = c.MissionInventory[:len(c.MissionInventory)-1]
			private := 0
			for tick := 1; tick <= 200 && private == 0; tick++ {
				for _, burst := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
					for _, frame := range burst.Frames {
						if frame.Opcode != wire.OpSkillCastResult {
							continue
						}
						if burst.OnlyCharacterID != c.ID {
							t.Fatalf("cast result %x on the public route", frame.Payload)
						}
						if !bytes.Equal(frame.Payload, []byte{2, 0x0e}) {
							t.Fatalf("cast result %x, want {2, 0x0E}", frame.Payload)
						}
						private++
					}
				}
			}
			if private != 1 {
				t.Fatalf("arrival refusals = %d, want one private 0x300E", private)
			}
		})
	}
}

// emptyBowPresses are each race's ranged basic attack and one ranged skill.
var emptyBowPresses = []string{
	"SKILL_CH_BOW_BASE_01", "SKILL_CH_BOW_CRITICAL_A_01",
	"SKILL_EU_CROSSBOW_BASE_01", "SKILL_EU_ROG_BOWA_POWER_A_01",
}

/*
================
emptyBowFixture

An archer with an unloaded bow (CH) or crossbow (EU), out of range of the
target, and the ammunition row the weapon would take, not yet carried. A
_BASE_ codename renames the fixture's skill 2 to the race's basic attack;
any other codename is the shipped skill, learned.
================
*/
func emptyBowFixture(t *testing.T, codename string) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance, uint32, *enterworld.ItemRef) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	kind, race, ammo := int64(6), enterworld.RaceChina, &enterworld.ItemRef{RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", TypeIDs: [4]int64{3, 3, 4, 1}}
	if strings.HasPrefix(codename, "SKILL_EU_") {
		c.ModelCodename = "CHAR_EU_MAN_NOBLE"
		kind, race, ammo = 12, enterworld.RaceEurope, &enterworld.ItemRef{RefObjID: 62002, Codename: "ITEM_ETC_AMMO_BOLT_01", TypeIDs: [4]int64{3, 3, 4, 2}}
	}
	c.RaceIndex = testInt64(race)
	items := rt.deps.ItemReferences().(staticItemSource)
	items[ammo.Codename] = ammo
	weapon := items[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = kind
	weapon.Combat.ActionRange = 180
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	skills := rt.deps.SkillData().(staticSkillSource)
	id := uint32(2)
	if strings.Contains(codename, "_BASE_") {
		skill := skills[2]
		skill.Codename = codename
		skill.RequiredWeaponKinds = [2]uint8{uint8(kind), 0xff}
		skills[2] = skill
	} else {
		skill := shippedOffense(t, codename)
		skills[skill.ID] = skill
		c.Skills = append(c.Skills, skill.ID)
		c.CurrentMP = testInt64(100)
		id = skill.ID
	}
	*c.World.Spawn.X = 500
	return rt, clock, c, target, id, ammo
}

/*
================
emptyBowPress

The client's request for a press: the basic attack is an engage (the
double-click), any other skill a cast.
================
*/
func emptyBowPress(codename string, id, target uint32) []byte {
	if strings.Contains(codename, "_BASE_") {
		return wire.BasicAttackEngage{TargetGid: target}.Encode()
	}
	return wire.SkillAction{ActionId: id, HasTarget: true, TargetGid: target}.Encode()
}

/*
================
TestUnarmedBasicAttackUsesRacialPunchAndRetailMeleeReach
================
*/
func TestUnarmedBasicAttackUsesRacialPunchAndRetailMeleeReach(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	character.MissionInventory = nil
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Codename = "SKILL_PUNCH_01"
	skill.ActionRange = 0
	skill.ActionRangePinned = true
	skill.RequiredWeaponKinds = [2]uint8{1, 0xff}
	skills[2] = skill

	result := rt.HandleTargetInteract(testDivision, character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	assertSkillDamageOpen(t, result.Frames, 2,
		enterworld.ObjectIDForCharacter(character), target.Gid)
}

// A skill cast without a bound character cannot mint a caster gid: it stays
// on the silent-refuse path (the pre-landing behaviour for everything the
// accept criteria do not match).
/*
================
TestSkillActionWithoutCharacterStaysSilent
================
*/
func TestSkillActionWithoutCharacterStaysSilent(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	result := rt.HandleTargetInteract(testDivision, nil, wire.SkillAction{ActionId: 0x1234}.Encode())
	if len(result.Frames) != 0 || len(result.Broadcast) != 0 || result.Pending != nil {
		t.Fatalf("nil-character skill cast answered %+v, want total silence", result)
	}
}
