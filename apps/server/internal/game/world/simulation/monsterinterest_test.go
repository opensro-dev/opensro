package simulation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

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
	shown := ops.scopeShown("viewer")
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
	if len(ops.scopeViewers) != 0 {
		t.Fatalf("scopeViewers retains %d departed sessions", len(ops.scopeViewers))
	}
}

/*
================
TestMonsterScopeStreamFrameBytes

#2134's done-when asks for spawn and despawn frames byte-identical
for a recorded scene. TestMonsterScopeStreamDeltas pins the shape
(opcodes, counts, the appear/rarity bytes); this test pins the whole
payload of every scope frame the same recorded walk emits: the
0x30D7 enter single for regionB's monster, the whole byListSub=2
bracket for regionA's pair (start, gid chunk, finalize), and the
0x36AB exit single. Any change to
the maintained-interest path that alters a byte on the wire fails
here even when the gid sets still match.
================
*/
func TestMonsterScopeStreamFrameBytes(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, regionA, regionB := scopeStreamFixture(t)
	push := &fakePusher{}

	session := viewerSessionAt(regionA, 100)
	recordTestMonsterBootstrap(ops, []SessionSnapshot{session}, t0)
	ops.RunMonsterLeg(t0, []SessionSnapshot{session}, push)
	for i := int64(1); i <= 3; i++ {
		ops.RunMonsterLeg(t0+i*250, []SessionSnapshot{session}, push)
	}
	session = viewerSessionAt(regionA, 400)
	ops.RunMonsterLeg(t0+1000, []SessionSnapshot{session}, push)

	type wantFrame struct {
		opcode uint16
		gid    uint32
		length int
		sha    string
	}
	pin := func(label string, frames []Frame, want []wantFrame) {
		t.Helper()
		if len(frames) != len(want) {
			t.Fatalf("%s: %d scope frames, want %d (%+v)", label, len(frames), len(want), frames)
		}
		for i, w := range want {
			got := frames[i]
			sum := sha256.Sum256(got.Payload)
			if got.Opcode != w.opcode || got.ScopeGID != w.gid || len(got.Payload) != w.length || hex.EncodeToString(sum[:]) != w.sha {
				t.Fatalf("%s frame %d = op %#04x gid %d len %d sha %s, want op %#04x gid %d len %d sha %s",
					label, i, got.Opcode, got.ScopeGID, len(got.Payload), hex.EncodeToString(sum[:]),
					w.opcode, w.gid, w.length, w.sha)
			}
		}
	}

	session = viewerSessionAt(regionA, 1700)
	ops.RunMonsterLeg(t0+1250, []SessionSnapshot{session}, push)
	// The crossing tick emits exactly these four frames, in order: the
	// enter single, then the exit bracket (start, the pair's gid chunk,
	// finalize). Nothing else rides the tick.
	pin("crossing", sessionFrames(push, "viewer"), []wantFrame{
		{wire.OpSingleObjectSpawn, 400003, 66, "bb5f151e32df165ccf4c2e67882c4f8f4dfc65681bfe41a14e5fbb234625f089"},
		{opObjectListStart, 0, 3, "138793139d414148c3ea406f4b273bdfb394d17fab3636115815abdf639d433d"},
		{wire.OpObjectListChunk, 0, 8, "5908ea4a61509511e79e386e2727402e192817006aec5882b8b6e25cc7cbe804"},
		{opObjectListFinalize, 0, 0, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	})

	push.toSession = nil
	session = viewerSessionAt(regionB, 740)
	ops.RunMonsterLeg(t0+1500, []SessionSnapshot{session}, push)
	pin("exit", scopeDeltaFrames(push, "viewer"), []wantFrame{
		{wire.OpObjectDespawn, 400003, 4, "ac4e0cd9b5ae0aced667120e79916ad674e5cb3e5e0d484d0cbda4c796306972"},
	})
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
	viewerState := ops.scopeViewers["viewer"]
	if viewerState == nil || !viewerState.memoOK {
		t.Fatal("no scope memo recorded after the seed leg")
	}
	memo := viewerState.memo
	if stamp, ok := ops.Monsters.scopeInterestStamp(monsterTestDivision, session.Population, viewers["viewer"], t0); !ok || stamp != memo.stamp {
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
	// still: the old block's stamp reads the same across the leg, so only
	// the block match can be what runs it in full and moves the shown set.
	still, ok := ops.Monsters.scopeInterestStamp(monsterTestDivision, session.Population, viewers["viewer"], t0+3000)
	if !ok {
		t.Fatal("no scope stamp for the still division")
	}
	viewers["viewer"] = worldgeom.RegionXZ{RegionID: regionA, X: 1800, Z: 100}
	frames = leg(t0 + 3000)
	if len(frames) == 0 {
		t.Fatal("block-crossing leg emitted no scope deltas")
	}
	if after, ok := ops.Monsters.scopeInterestStamp(monsterTestDivision, session.Population, worldgeom.RegionXZ{RegionID: regionA, X: 100, Z: 100}, t0+3000); !ok || after != still {
		t.Fatal("the division moved, so this leg proved nothing about the viewer's block")
	}
}

/*
================
TestScopeLegFarViewerSkipsNearChurn

#2134: the interest stamp names the viewer's own blocks, so a monster
crossing a block edge in a far neighbourhood does not retire a distant
viewer's skip. The near viewer still gets its despawn on the same leg;
the far viewer's memo stamp is untouched and its leg publishes nothing.
With a division-wide stamp the far viewer's stamp moved on every such
leg and its shown set was re-read in full.
================
*/
func TestScopeLegFarViewerSkipsNearChurn(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, regionA, regionB := scopeStreamFixture(t)
	a2 := scopeStreamInstance(t, ops.Monsters, regionA, 200)
	nearSession := viewerSessionAt(regionA, 100)
	// The far viewer sits at regionB's east edge: A2's eastward walk
	// crosses regionA's blocks only (its destination block neighbours
	// regionB's WEST edge), so nothing in the far view ever changes.
	farSession := viewerSessionAt(regionB, 1700)
	farSession.SessionID = "far"
	farSession.CharacterID = 8
	sessions := []SessionSnapshot{nearSession, farSession}
	viewers := map[string]worldgeom.RegionXZ{
		"viewer": {RegionID: regionA, X: 100, Z: 100},
		"far":    {RegionID: regionB, X: 1700, Z: 100},
	}
	live := map[string]bool{"viewer": true, "far": true}
	recordTestMonsterBootstrap(ops, sessions, t0)

	push := &fakePusher{}
	leg := func(nowMs int64) {
		push.toSession = nil
		ops.runScopeVisibility(nowMs, sessions, viewers, live, push)
	}
	leg(t0)
	farState := ops.scopeViewers["far"]
	if farState == nil || !farState.memoOK {
		t.Fatal("no scope memo recorded for the far viewer after the seed leg")
	}
	farMemo := farState.memo

	// Walk A2 east out of the near viewer's neighbourhood, as in
	// TestScopeLegSkipsTheUnchangedLeg: the near leg runs in full and
	// publishes exactly one despawn; the far leg must stay skipped.
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
	leg(t0 + 1500)
	if frames := scopeDeltaFrames(push, "viewer"); len(frames) != 1 || frames[0].Opcode != wire.OpObjectDespawn || frames[0].ScopeGID != a2.Gid {
		t.Fatalf("near leg frames = %+v, want one despawn for A2", frames)
	}
	if frames := scopeDeltaFrames(push, "far"); len(frames) != 0 {
		t.Fatalf("far leg emitted %d scope deltas, want 0: %+v", len(frames), frames)
	}
	if got := farState.memo; got != farMemo {
		t.Fatalf("far viewer's memo moved on near churn: %+v -> %+v", farMemo, got)
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

/*
================
TestScopeLegRebuildRetiresTheVacatedBlock

#2134 review: past maxMoverBlockDirty the block index drops its notes
and the next scan rebuilds it, discarding the old membership instead
of leaving it. A mover that crossed out of the viewer's blocks inside
that window must still retire the viewer's stamp - the rebuild bumps
every block the old index held - or the leg skips and the monster
stays shown with no despawn.
================
*/
func TestScopeLegRebuildRetiresTheVacatedBlock(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	state := &divisionMonsterState{instances: newMonsterStorage(nil), movers: newMoverStorage(nil),
		byRegion: map[uint16][]uint32{}}
	registry := &MonsterState{
		divs:  map[string]*divisionMonsterState{monsterTestDivision: state},
		clock: func() time.Time { return time.UnixMilli(t0) },
	}
	ops := &MonsterMoverOps{Monsters: registry}
	home := monster.Pose{RegionID: interestScanRegion, X: 960, Z: 960}
	away := monster.Pose{RegionID: interestScanRegion, X: 960 + 4*320, Z: 960}
	state.instances.set(1, monster.Instance{Gid: 1, CurrentHP: 1})
	state.movers.set(1, monster.MoverState{Pose: home})
	// The burst below rewrites this instanceless row: a row added
	// mid-burst would move the statics roster generation and retire
	// the stamp on its own, proving nothing about the rebuild.
	state.movers.set(2, monster.MoverState{Pose: away})

	session := SessionSnapshot{SessionID: "viewer", DivisionID: monsterTestDivision, CharacterID: 7}
	sessions := []SessionSnapshot{session}
	viewers := map[string]worldgeom.RegionXZ{"viewer": {RegionID: interestScanRegion, X: 960, Z: 960}}
	live := map[string]bool{"viewer": true}
	registry.RecordObjectList(monsterTestDivision, PlayerObjectID(7), []uint32{1})

	push := &fakePusher{}
	leg := func(nowMs int64) []Frame {
		push.toSession = nil
		ops.runScopeVisibility(nowMs, sessions, viewers, live, push)
		return scopeDeltaFrames(push, "viewer")
	}
	if frames := leg(t0); len(frames) != 0 {
		t.Fatalf("seed leg emitted %d scope deltas, want 0", len(frames))
	}

	// Burst past the dirty bound so the next scan rebuilds the
	// index, then cross the mover four blocks east - outside the
	// viewer's 3x3 - inside that window.
	for i := 0; i <= maxMoverBlockDirty; i++ {
		state.movers.set(2, monster.MoverState{Pose: away})
	}
	if state.movers.blocks.built {
		t.Fatal("the burst did not drop the block index to a rebuild")
	}
	state.movers.set(1, monster.MoverState{Pose: away})

	if frames := leg(t0 + 100); len(frames) != 1 || frames[0].Opcode != wire.OpObjectDespawn || frames[0].ScopeGID != 1 {
		t.Fatalf("post-rebuild leg frames = %+v, want one despawn for gid 1", frames)
	}
	if ops.scopeShown("viewer")[1] {
		t.Fatal("gid 1 is still shown after the rebuild retired its block")
	}
}

/*
================
scopeBenchWorld

One division holding the interest-scan crowd, with viewerCount
sessions clustered in the centre blocks or spread over the five-by-
five regions around them. The division state comes back so a bench
can flip movers across a block edge between legs.
================
*/
func scopeBenchWorld(viewerCount int, spread bool) (*MonsterMoverOps, []SessionSnapshot, map[string]worldgeom.RegionXZ, map[string]bool, *divisionMonsterState) {
	state := interestScanCrowd(3000, 300)
	state.byRegion = make(map[uint16][]uint32)
	// Six thousand static listings in far regions no viewer's ring
	// reaches: a live division lists its whole map, while a leg only
	// ever asks the rings its viewers stand in. The listings never
	// enter an answer; they are what an eager statics rebuild probes
	// on every roster move (#3027).
	for gid := uint32(3001); gid <= 9000; gid++ {
		region := uint16(interestScanRegion + 0x400 + gid%8)
		state.instances.set(gid, monster.Instance{Gid: gid, CurrentHP: 1})
		state.listRegionGid(region, gid)
	}
	registry := &MonsterState{
		divs:  map[string]*divisionMonsterState{monsterTestDivision: state},
		clock: func() time.Time { return time.UnixMilli(1_784_000_000_000) },
	}
	ops := &MonsterMoverOps{Monsters: registry}
	sessions := make([]SessionSnapshot, 0, viewerCount)
	viewers := make(map[string]worldgeom.RegionXZ, viewerCount)
	live := make(map[string]bool, viewerCount)
	for i := 0; i < viewerCount; i++ {
		id := fmt.Sprintf("s%d", i)
		viewer := worldgeom.RegionXZ{RegionID: interestScanRegion, X: 900 + float64(i%7)*20, Z: 900 + float64(i/7)*20}
		if spread {
			region := uint16(interestScanRegion + i%5 - 2 + (i/5%5-2)*0x100)
			viewer = worldgeom.RegionXZ{RegionID: region, X: 960, Z: 960}
		}
		sessions = append(sessions, SessionSnapshot{
			SessionID: id, DivisionID: monsterTestDivision, CharacterID: int64(1000 + i),
			World: WorldState{Spawn: Spawn{RegionID: viewer.RegionID, X: viewer.X, Y: 10, Z: viewer.Z}, SpawnSet: true},
		})
		viewers[id] = viewer
		live[id] = true
	}
	return ops, sessions, viewers, live, state
}

/*
================
BenchmarkScopeVisibilityLeg

One runScopeVisibility leg over the seeded viewers of a 3000-mover
crowd (#2134). Quiet: nothing moves between legs, so the
unchanged-leg skip serves everyone. Roaming: eight movers straddle a
block edge inside the clustered viewers' area, so their stamps move
on every leg while far neighbourhoods stand still. Churn shapes also
remove and re-add a mover every leg: the roster move retires the
statics stamp, so each leg re-reads statics as a live division's
spawn and despawn traffic makes it (#3027).
================
*/
func BenchmarkScopeVisibilityLeg(b *testing.B) {
	shapes := []struct {
		name    string
		viewers int
		spread  bool
		flips   int
		churn   bool
	}{
		{"clustered/50viewers/quiet", 50, false, 0, false},
		{"clustered/50viewers/roaming8", 50, false, 8, false},
		{"clustered/200viewers/quiet", 200, false, 0, false},
		{"clustered/200viewers/roaming8", 200, false, 8, false},
		{"spread/200viewers/roaming8", 200, true, 8, false},
		{"clustered/200viewers/roaming8+churn", 200, false, 8, true},
		{"spread/200viewers/roaming8+churn", 200, true, 8, true},
	}
	for _, shape := range shapes {
		b.Run(shape.name, func(b *testing.B) {
			ops, sessions, viewerMap, live, state := scopeBenchWorld(shape.viewers, shape.spread)
			push := &fakePusher{}
			nowMs := int64(1_784_000_000_000)
			for leg := 0; leg < 3; leg++ {
				nowMs += 100
				ops.runScopeVisibility(nowMs, sessions, viewerMap, live, push)
			}
			if len(ops.scopeViewers) != shape.viewers {
				b.Fatalf("seeded %d viewers, want %d", len(ops.scopeViewers), shape.viewers)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				nowMs += 100
				for flip := 0; flip < shape.flips; flip++ {
					x := 950.0
					if (i+flip)%2 == 1 {
						x = 990.0
					}
					state.movers.set(uint32(400+flip), monster.MoverState{Pose: monster.Pose{RegionID: interestScanRegion, X: x, Z: 960}})
				}
				if shape.churn {
					gid := uint32(500 + i%8)
					pose := monster.Pose{RegionID: interestScanRegion, X: 960, Z: 960}
					state.movers.remove(gid)
					state.movers.set(gid, monster.MoverState{Pose: pose})
				}
				push.toSession = push.toSession[:0]
				ops.runScopeVisibility(nowMs, sessions, viewerMap, live, push)
			}
		})
	}
}
