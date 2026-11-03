package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

func scopeStreamInstance(t *testing.T, registry *MonsterState, regionID uint16, x float64) monster.Instance {
	t.Helper()
	registry.StartDivision(monsterTestDivision)
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	for _, instance := range registry.InstancesInRegions(monsterTestDivision, []uint16{regionID}) {
		if instance.Spawn.X == x {
			return instance
		}
	}
	t.Fatalf("no fixture instance at region %#04x x=%v", regionID, x)
	return monster.Instance{}
}

// The region ring still materializes every candidate, but only the native
// 3x3 block neighbourhood is visible: a same-region monster two blocks away
// is not admitted (the former whole-region ring admitted it).
func TestMonsterInterestIsNarrowerThanTheMaterializationRing(t *testing.T) {
	ops, regionA, _ := scopeStreamFixture(t)
	viewer := worldgeom.RegionXZ{RegionID: regionA, X: 1000, Z: 100} // block X=99
	ops.Monsters.StartDivision(monsterTestDivision)
	ops.Monsters.AdvancePopulation(ops.Monsters.CurrentTimeMillis())
	if got := len(ops.Monsters.InstancesInRegions(monsterTestDivision, RegionScopeRing(regionA))); got != 3 {
		t.Fatalf("ring candidates = %d, want all 3 fixture monsters", got)
	}
	if got := ops.Monsters.InterestInstances(monsterTestDivision, viewer, 0); len(got) != 0 {
		t.Fatalf("interest admitted %d monsters from blocks 96 and 102, want none", len(got))
	}

	push := &fakePusher{}
	ops.RunMonsterLeg(1_784_000_000_000, []SessionSnapshot{viewerSessionAt(regionA, 1000)}, push)
	if frames := sessionFrames(push, "viewer"); len(frames) != 0 {
		t.Fatalf("viewer outside every monster's neighbourhood received %d frames", len(frames))
	}
}

// Block membership reads the mover's live position, not the nest anchor.
func TestMonsterInterestFollowsTheLivePose(t *testing.T) {
	ops, regionA, _ := scopeStreamFixture(t)
	registry := ops.Monsters
	instance := scopeStreamInstance(t, registry, regionA, 100)
	mover, _ := registry.Mover(monsterTestDivision, instance.Gid)
	if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
		t.Fatalf("seed idle mover: %v", err)
	}
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatalf("transition wander: %v", err)
	}
	from := monster.Pose{RegionID: regionA, X: 100, Y: 10, Z: 100}
	mover.From, mover.Pose = from, from
	mover.To = monster.Pose{RegionID: regionA, X: 1000, Y: 10, Z: 100}
	mover.DepartMs, mover.ArriveMs = 0, 10_000
	registry.CommitMover(monsterTestDivision, instance.Gid, mover)

	viewer := worldgeom.RegionXZ{RegionID: regionA, X: 100, Z: 100} // block X=96
	visible := func(nowMs int64) bool {
		for _, candidate := range registry.InterestInstances(monsterTestDivision, viewer, nowMs) {
			if candidate.Gid == instance.Gid {
				return true
			}
		}
		return false
	}
	if !visible(5_000) { // live x=550, block 97
		t.Fatal("monster one block from the viewer must be visible")
	}
	if visible(8_000) { // live x=820, block 98, anchor still in block 96
		t.Fatal("monster two blocks from the viewer must leave interest while its anchor stays inside")
	}
}

// The client holds exactly the recorded bootstrap object list, so the first
// scope tick reconciles against it: a recorded monster now outside interest
// is removed, an unrecorded visible monster is created, and a recorded
// visible monster is left alone.
func TestFirstSightReconcilesAgainstTheRecordedObjectList(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, regionA, regionB := scopeStreamFixture(t)
	a1 := scopeStreamInstance(t, ops.Monsters, regionA, 100)
	a2 := scopeStreamInstance(t, ops.Monsters, regionA, 200)
	b := scopeStreamInstance(t, ops.Monsters, regionB, 200)
	session := viewerSessionAt(regionA, 100) // block 96: sees A1 and A2, not B
	ops.Monsters.RecordObjectList(monsterTestDivision, PlayerObjectID(session.CharacterID), []uint32{a1.Gid, b.Gid})

	push := &fakePusher{}
	ops.RunMonsterLeg(t0, []SessionSnapshot{session}, push)
	created := 0
	var removed []uint32
	for _, frame := range scopeDeltaFrames(push, "viewer") {
		switch frame.Opcode {
		case wire.OpSingleObjectSpawn:
			created++
		case wire.OpObjectDespawn:
			despawn, err := wire.DecodeObjectDespawn(frame.Payload)
			if err != nil {
				t.Fatalf("despawn decode: %v", err)
			}
			removed = append(removed, despawn.Gid)
		default:
			t.Fatalf("unexpected scope delta %#04x", frame.Opcode)
		}
	}
	if created != 1 || len(removed) != 1 || removed[0] != b.Gid {
		t.Fatalf("first-sight deltas created=%d removed=%v, want one create (A2) and B removed", created, removed)
	}
	shown := ops.shownMonsters["viewer"]
	if !shown[a1.Gid] || !shown[a2.Gid] || shown[b.Gid] || len(shown) != 2 {
		t.Fatalf("shown after reconciliation = %v, want exactly A1 and A2", shown)
	}
	if _, ok := ops.Monsters.TakeObjectList(monsterTestDivision, PlayerObjectID(session.CharacterID)); ok {
		t.Fatal("the bootstrap record must be consumed by first sight")
	}
}

// ---- the Q2 scoped visibility stream ----

// scopeStreamFixture: regionA holds two nests in block X=96 (sector 16,
// local block 0) and regionB one nest in block X=102 (sector 17, local block
// 0), all on block row Z=96. A viewer walking east along z=100 sees the A pair
// first, then B enters across the region seam, then A leaves.
func scopeStreamFixture(t *testing.T) (*MonsterMoverOps, uint16, uint16) {
	t.Helper()
	regionA := RegionIDForSectors(16, 16)
	regionB := RegionIDForSectors(17, 16)
	refs := map[uint32]monster.MonsterRef{
		1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", MonsterType: 3, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100},
	}
	template := monster.TemplateFromParts(refs, []monster.NestRow{
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: regionA, X: 100, Y: 10, Z: 100}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: regionA, X: 200, Y: 10, Z: 150}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: regionB, X: 200, Y: 10, Z: 100}},
	})
	registry := NewMonsterState(template)
	registry.StartDivision(monsterTestDivision)
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	registry.StartDivision(monsterTestDivision)
	registry.AdvancePopulation(1784000000000)
	return &MonsterMoverOps{
		Monsters:   registry,
		TacticsFor: fixedTactics(passiveTactics()),
		Rand:       func() float64 { return 0.5 },
	}, regionA, regionB
}

func viewerSessionAt(regionID uint16, x float64) SessionSnapshot {
	world := WorldState{Spawn: Spawn{RegionID: regionID, X: x, Y: 10, Z: 100}, SpawnSet: true}
	return SessionSnapshot{SessionID: "viewer", DivisionID: monsterTestDivision, CharacterID: 7, World: world, WorldInstance: 0x10001, Population: instance.Lease{ID: instance.Pack(1, 1), Generation: 1}}
}

func sessionFrames(push *fakePusher, sessionID string) []Frame {
	var out []Frame
	for _, pushed := range push.toSession {
		if pushed.sessionID == sessionID {
			out = append(out, pushed.frames...)
		}
	}
	return out
}

// The scoped visibility stream end to end over native 320-unit interest
// blocks: first sight seeds silently (the bootstrap list already delivered
// the interest set); a STATIONARY viewer emits ZERO deltas (coordinator
// seq401 condition 3); moving within the same neighbourhood emits nothing;
// jumping east makes regionB's monster enter across the region seam as one
// 0x30D7 single WITH the appear tail while regionA's two monsters leave as ONE
// byListSub=2 bracket; a single-monster exit uses the 0x36AB single.
func TestMonsterScopeStreamDeltas(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, regionA, regionB := scopeStreamFixture(t)
	push := &fakePusher{}

	// Tick 1 @ block X=96: seed - regionA's 2 monsters in scope, NO scope
	// deltas emitted (bootstrap already delivered the interest set; mover
	// goal frames for the shown monsters are legitimate and ignored here).
	session := viewerSessionAt(regionA, 100)
	recordTestMonsterBootstrap(ops, []SessionSnapshot{session}, t0)
	ops.RunMonsterLeg(t0, []SessionSnapshot{session}, push)
	if frames := scopeDeltaFrames(push, "viewer"); len(frames) != 0 {
		t.Fatalf("seed tick emitted %d scope deltas, want 0 (bootstrap already delivered the interest set)", len(frames))
	}

	// Ticks 2-4, stationary: ZERO scope deltas (the seq401 C3 canary).
	for i := int64(1); i <= 3; i++ {
		ops.RunMonsterLeg(t0+i*250, []SessionSnapshot{session}, push)
	}
	if frames := scopeDeltaFrames(push, "viewer"); len(frames) != 0 {
		t.Fatalf("stationary viewer received %d scope deltas, want 0 (spawn/despawn churn without movement is a bug)", len(frames))
	}

	// Tick 5 @ block X=97: the neighbourhood is 96-98, so A stays and B
	// (102) is still out of interest.
	session = viewerSessionAt(regionA, 400)
	ops.RunMonsterLeg(t0+1000, []SessionSnapshot{session}, push)
	if frames := scopeDeltaFrames(push, "viewer"); len(frames) != 0 {
		t.Fatalf("block-97 viewer emitted %d scope deltas, want 0 (A still visible, B not yet)", len(frames))
	}

	// Tick 6 @ block X=101 (regionA's last block): the neighbourhood is
	// 100-102, so regionB's monster ENTERS across the seam and regionA's pair
	// EXITS in the same step - one 0x30D7 single with the vt+0x68 appear tail
	// in, the two A-monsters out as one bracket.
	session = viewerSessionAt(regionA, 1700)
	ops.RunMonsterLeg(t0+1250, []SessionSnapshot{session}, push)
	frames := scopeDeltaFrames(push, "viewer")
	var singles, brackets, despawnSingles int
	for _, frame := range frames {
		switch frame.Opcode {
		case wire.OpSingleObjectSpawn:
			singles++
			// The single MUST end with the appear byte (WIP C3): create
			// row + 1.
			if frame.Payload[len(frame.Payload)-1] != MonsterAppearByte {
				t.Fatalf("0x30D7 single does not end with the appear byte: % X", frame.Payload)
			}
			if frame.Payload[len(frame.Payload)-2] != 3 {
				t.Fatalf("0x30D7 scope-enter rarity = %d, want instance rarity 3", frame.Payload[len(frame.Payload)-2])
			}
		case opObjectListStart:
			brackets++
			if frame.Payload[0] != 0x02 {
				t.Fatalf("bracket byListSub = %#02x, want 0x02 (despawn group)", frame.Payload[0])
			}
			if count := int(frame.Payload[1]) | int(frame.Payload[2])<<8; count != 2 {
				t.Fatalf("bracket count = %d, want 2 (both regionA monsters)", count)
			}
		case wire.OpObjectDespawn:
			despawnSingles++
		}
	}
	if singles != 1 || brackets != 1 || despawnSingles != 0 {
		t.Fatalf("block-101 deltas: %d singles / %d brackets / %d despawn-singles, want 1/1/0 (B enters, A pair leaves as a bracket)", singles, brackets, despawnSingles)
	}

	// Tick 7 @ block X=104 (regionB local block 2): B (102) exits alone ->
	// ONE 0x36AB single.
	push.toSession = nil
	session = viewerSessionAt(regionB, 740)
	ops.RunMonsterLeg(t0+1500, []SessionSnapshot{session}, push)
	frames = scopeDeltaFrames(push, "viewer")
	if len(frames) != 1 || frames[0].Opcode != wire.OpObjectDespawn {
		t.Fatalf("single exit frames = %+v, want exactly one 0x36AB", frames)
	}

	// Departed session: bookkeeping must not leak.
	ops.RunMonsterLeg(t0+1750, nil, push)
	if len(ops.shownMonsters) != 0 {
		t.Fatalf("shownMonsters retains %d departed sessions", len(ops.shownMonsters))
	}
	if len(ops.scopeMemo) != 0 {
		t.Fatalf("scopeMemo retains %d departed sessions", len(ops.scopeMemo))
	}
}

/*
================
TestScopeLegSkipsTheUnchangedLeg

#2134: a seeded viewer whose block and interest stamp match its last
full pass skips the delta read, and the skip never swallows a change:
a monster walking out of the neighbourhood still despawns and
walking back still spawns, because both crossings move the stamp.
The published stream is the one the unskipped leg produced.
================
*/
func TestScopeLegSkipsTheUnchangedLeg(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, regionA, _ := scopeStreamFixture(t)
	a2 := scopeStreamInstance(t, ops.Monsters, regionA, 200)
	session := viewerSessionAt(regionA, 100)
	sessions := []SessionSnapshot{session}
	viewers := map[string]worldgeom.RegionXZ{"viewer": {RegionID: regionA, X: 100, Z: 100}}
	live := map[string]bool{"viewer": true}
	recordTestMonsterBootstrap(ops, sessions, t0)

	push := &fakePusher{}
	leg := func(nowMs int64) []Frame {
		push.toSession = nil
		ops.runScopeVisibility(nowMs, sessions, viewers, live, push)
		return scopeDeltaFrames(push, "viewer")
	}
	if frames := leg(t0); len(frames) != 0 {
		t.Fatalf("seed leg emitted %d scope deltas, want 0", len(frames))
	}
	memo, ok := ops.scopeMemo["viewer"]
	if !ok {
		t.Fatalf("no scope memo recorded after the seed leg (recorded %v)", ok)
	}
	if stamp, ok := ops.Monsters.scopeInterestStamp(monsterTestDivision, session.Population, t0); !ok || stamp != memo.stamp {
		t.Fatal("the seed leg's memo is not the stamp a fresh scope read keys on")
	}
	for i := int64(1); i <= 2; i++ {
		if frames := leg(t0 + i*250); len(frames) != 0 {
			t.Fatalf("unchanged leg %d emitted %d scope deltas, want 0", i, len(frames))
		}
	}

	// Walk A2 east out of the neighbourhood (block 96 to 101). Its
	// block membership moves the stamp, so the leg runs in full and
	// the exit publishes exactly one despawn single.
	mover, ok := ops.Monsters.Mover(monsterTestDivision, a2.Gid)
	if !ok {
		t.Fatal("A2 has no mover")
	}
	if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
		t.Fatal(err)
	}
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatal(err)
	}
	mover.From = monster.Pose{RegionID: regionA, X: 200, Y: 10, Z: 150}
	mover.Pose = mover.From
	mover.To = monster.Pose{RegionID: regionA, X: 1700, Y: 10, Z: 150}
	mover.DepartMs, mover.ArriveMs = t0+500, t0+1500
	if !ops.Monsters.CommitMover(monsterTestDivision, a2.Gid, mover) {
		t.Fatal("walk-out commit rejected")
	}
	frames := leg(t0 + 1500)
	if len(frames) != 1 || frames[0].Opcode != wire.OpObjectDespawn || frames[0].ScopeGID != a2.Gid {
		t.Fatalf("walk-out leg frames = %+v, want one despawn for A2", frames)
	}

	// Walk it back: one spawn single, again through the full leg -
	// the skip must not have frozen the shown bookkeeping either.
	mover.Pose = mover.To
	mover.From = mover.To
	mover.To = monster.Pose{RegionID: regionA, X: 200, Y: 10, Z: 150}
	mover.DepartMs, mover.ArriveMs = t0+1500, t0+2500
	if !ops.Monsters.CommitMover(monsterTestDivision, a2.Gid, mover) {
		t.Fatal("walk-back commit rejected")
	}
	frames = leg(t0 + 2500)
	if len(frames) != 1 || frames[0].Opcode != wire.OpSingleObjectSpawn || frames[0].ScopeGID != a2.Gid {
		t.Fatalf("walk-back leg frames = %+v, want one spawn for A2", frames)
	}
	if frames := leg(t0 + 2750); len(frames) != 0 {
		t.Fatalf("settled leg emitted %d scope deltas, want 0", len(frames))
	}

	// The viewer crosses into a block of its own while the division stands
	// still: the stamp alone would call this leg unchanged, so the block
	// match is what runs it in full and moves the shown set.
	stamp := ops.scopeMemo["viewer"].stamp
	viewers["viewer"] = worldgeom.RegionXZ{RegionID: regionA, X: 1800, Z: 100}
	frames = leg(t0 + 3000)
	if len(frames) == 0 {
		t.Fatal("block-crossing leg emitted no scope deltas")
	}
	if ops.scopeMemo["viewer"].stamp != stamp {
		t.Fatal("the division moved, so this leg proved nothing about the viewer's block")
	}
}

/*
================
scopeRemovingPusher

A pusher that removes one actor's instance the first time a spawn
is pushed: the mid-leg removal a concurrent writer can land between
the scope leg's delta read and a later actor's enter.
================
*/
type scopeRemovingPusher struct {
	fakePusher
	remove func(spawnedGid uint32)
	fired  bool
}

/*
================
PushToSession
================
*/
func (p *scopeRemovingPusher) PushToSession(sessionID string, frames []Frame) {
	p.fakePusher.PushToSession(sessionID, frames)
	if p.fired {
		return
	}
	for _, frame := range frames {
		if frame.Opcode == wire.OpSingleObjectSpawn {
			p.fired = true
			p.remove(frame.ScopeGID)
			return
		}
	}
}

/*
================
TestScopeLegRetriesAnActorRemovedMidLeg

#2134: an actor removed between the delta read and its enter cannot
resolve, so it stays unshown - and because the memo holds the stamp
sampled before that pass, the removal (which bumps the view
generation) makes the next leg's sample differ and run in full.
When the actor is back, the owed enter lands. The skip's safety
reduces to this: every change to the computed in-scope set moves a
stamp input (block membership, instance appearance, departure or
spawn move, the statics roster and region lists), while residency
alone never does - contains spans the hot and cold tiers, so a
frozen actor stays in the set exactly as the unskipped leg reads it.
================
*/
func TestScopeLegRetriesAnActorRemovedMidLeg(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, regionA, _ := scopeStreamFixture(t)
	a1 := scopeStreamInstance(t, ops.Monsters, regionA, 100)
	a2 := scopeStreamInstance(t, ops.Monsters, regionA, 200)
	state := ops.Monsters.divs[monsterTestDivision]

	session := viewerSessionAt(regionA, 100)
	sessions := []SessionSnapshot{session}
	viewers := map[string]worldgeom.RegionXZ{"viewer": {RegionID: regionA, X: 100, Z: 100}}
	live := map[string]bool{"viewer": true}
	push := &scopeRemovingPusher{remove: func(spawnedGid uint32) {
		other := a1.Gid
		if spawnedGid == a1.Gid {
			other = a2.Gid
		}
		state.instances.remove(other)
	}}
	leg := func(nowMs int64) []Frame {
		push.toSession = nil
		ops.runScopeVisibility(nowMs, sessions, viewers, live, push)
		return scopeDeltaFrames(&push.fakePusher, "viewer")
	}

	// No bootstrap record: the first leg is the create pass. The
	// first spawn's push removes the other actor, whose enter then
	// cannot resolve: exactly one spawn lands and the pass stays
	// pending.
	frames := leg(t0)
	if len(frames) != 1 || frames[0].Opcode != wire.OpSingleObjectSpawn {
		t.Fatalf("first leg frames = %+v, want exactly one spawn", frames)
	}
	removed := a1
	if frames[0].ScopeGID == a1.Gid {
		removed = a2
	}

	// The memo holds the pre-removal stamp, so the next leg's fresh
	// sample differs (the removal bumped the view generation) and
	// runs in full, publishing nothing.
	if frames := leg(t0 + 250); len(frames) != 0 {
		t.Fatalf("quiet leg emitted %d scope deltas, want 0", len(frames))
	}

	// The actor is restored: the owed enter lands.
	state.instances.set(removed.Gid, removed)
	frames = leg(t0 + 500)
	if len(frames) != 1 || frames[0].Opcode != wire.OpSingleObjectSpawn || frames[0].ScopeGID != removed.Gid {
		t.Fatalf("restore leg frames = %+v, want one spawn for gid %d", frames, removed.Gid)
	}
}
