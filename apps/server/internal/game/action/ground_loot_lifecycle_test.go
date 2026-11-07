/*
===========================================================================

ground_loot_lifecycle_test.go - monster loot remains in the observer lifecycle

Drive each fatal damage publisher into real transport sessions, then advance
the maintenance clock. A received spawn must admit ownership and expiry events
for both the killer and a peer, without requiring a scene reload.

===========================================================================
*/

package action

import (
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
	"opensro.online/server/internal/transport/worldsession"
)

/*
================
lootPublication

Keep the producer's original frame metadata through the test seam. Rebuilding
spawn packets here would hide precisely the missing publication under test.
================
*/
type lootPublication struct {
	runtime     *Runtime
	actor       *enterworld.Character
	actorFrames []wire.Frame
	peerFrames  []wire.Frame
}

/*
================
makeLootPublication
================
*/
func makeLootPublication(t *testing.T, lane string) lootPublication {
	t.Helper()
	rt, clock, actor, target := newCombatTestRuntime(t, 1)
	skillID := uint32(2)
	if lane == "area" {
		areaRuntime, monsters := areaFixture(t, 1)
		rt = areaRuntime
		actor = rt.findCharacter(testDivision, "asd2")
		target = monsters[0]
		skill := shippedOffense(t, "SKILL_CH_LIGHTNING_CHUNDUNG_A_01")
		rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
		actor.Skills = append(actor.Skills, skill.ID)
		actor.CurrentMP = testInt64(1000)
		skillID = skill.ID
	}
	if lane == "persistent" {
		rt, clock, actor, target = periodicFixture(t, 1)
		skillID = testPeriodicSkillID
	}
	if lane == "projectile" {
		var gid uint32
		var skill enterworld.SkillRow
		rt, actor, gid, skill, _ = arrowFixture(t)
		target, _ = rt.Monsters.Get(testDivision, gid)
		impacts := rt.Monsters.ApplyDamageSequence(testDivision, gid, target.CurrentHP,
			[]simulation.MonsterDamagePlan{{GID: gid, Damage: target.CurrentHP - 1}})
		if len(impacts) != 1 || impacts[0].CurrentHP != 1 {
			t.Fatal("projectile victim setup failed")
		}
		skillID = skill.ID
	}
	installSmallGoldRef(rt)
	// A constant zero roll admits gold on every area victim; only the gold
	// reference is installed, so unrelated authored item candidates are skipped.
	rt.DropRoll = constantDropRoll(0)
	published := lootPublication{runtime: rt, actor: actor}
	var batches []simulation.DivisionFrames
	if lane == "abnormal" {
		record := abnormal.Record{Status: abnormal.Burn, Level: 100, DurationMs: 75000,
			Rate24: 8, Scale20: 1, SourceGID: enterworld.ObjectIDForCharacter(actor), SourceName: actor.Name}
		result := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP,
			[]simulation.MonsterDamagePlan{{GID: target.Gid, CreditGID: record.SourceGID,
				Abnormal: []abnormal.Record{record}, AbnormalSources: rt.Monsters.PrepareAbnormalSources(testDivision, []abnormal.Record{record})}})
		if len(result) != 1 {
			t.Fatal("abnormal setup failed")
		}
		batches = rt.advanceMonsterAbnormals(clock.NowMs())
	} else {
		request := wire.SkillAction{ActionId: skillID, HasTarget: true, TargetGid: target.Gid}.Encode()
		if lane == "basic" {
			request = wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()
		}
		result := rt.HandleTargetInteract(testDivision, actor, request)
		published.actorFrames, published.peerFrames = result.Frames, result.Broadcast
		if lane == "projectile" {
			skill, _ := rt.deps.SkillData().SkillByID(skillID)
			batches = rt.advanceProjectileCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
			published.actorFrames, published.peerFrames = nil, nil
		}
		if lane == "persistent" {
			batches = rt.advancePeriodicEffects(clock.NowMs() + 2000)
			published.actorFrames, published.peerFrames = nil, nil
		}
	}
	for _, batch := range batches {
		for _, frame := range batch.Frames {
			converted := wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope}
			if batch.OnlyCharacterID == 0 {
				published.peerFrames = append(published.peerFrames, converted)
			}
			if batch.OnlyCharacterID == 0 || batch.OnlyCharacterID == actor.ID {
				published.actorFrames = append(published.actorFrames, converted)
			}
		}
	}
	if rt.Ground.Count(testDivision) == 0 {
		t.Fatal("fatal lane produced no loot")
	}
	return published
}

/*
================
TestMonsterLootPublicationSurvivesOwnershipAndExpiry
================
*/
func TestMonsterLootPublicationSurvivesOwnershipAndExpiry(t *testing.T) {
	for _, lane := range []string{"direct", "basic", "projectile", "area", "persistent", "abnormal"} {
		t.Run(lane, func(t *testing.T) {
			publication := makeLootPublication(t, lane)
			rt := publication.runtime
			server := wireStartServer(t, rt)
			actor := wireConnect(t, server, testDivision, publication.actor.Name)
			peer := wireConnect(t, server, testDivision, "loot-observer")
			wireObserveCharacter(t, server, peer, publication.actor)
			actorSession, _ := server.Hub.Session(actor.sessionID)
			peerSession, _ := server.Hub.Session(peer.sessionID)
			SendFrames(actorSession, publication.actorFrames)
			BroadcastObservedFrames(server.Hub, testDivision, actor.sessionID,
				enterworld.ObjectIDForCharacter(publication.actor), publication.peerFrames)
			for _, frame := range publication.actorFrames {
				actor.expectFrame(t, frame.Opcode, frame.Payload)
			}
			for _, frame := range publication.peerFrames {
				peer.expectFrame(t, frame.Opcode, frame.Payload)
			}
			drops := rt.Ground.All(testDivision)
			for _, viewer := range []*transport.Session{actorSession, peerSession} {
				revision, _ := viewer.SceneRevision()
				observed, current := viewer.PublishedObjects(revision)
				for _, drop := range drops {
					if !current || !slices.Contains(observed, drop.Gid) {
						t.Fatalf("received loot %d is absent from session %d publication scope", drop.Gid, viewer.ID)
					}
				}
			}
			bridge := worldsession.New(server.Hub)
			for _, age := range []time.Duration{grounditem.OwnerLifetime, grounditem.FixtureLifetime} {
				source := tickSessionSource{}
				for _, viewer := range []*transport.Session{actorSession, peerSession} {
					revision, _ := viewer.SceneRevision()
					observed, _ := viewer.PublishedObjects(revision)
					source.sessions = append(source.sessions, simulation.SessionSnapshot{
						SessionID: worldsession.SessionSceneID(viewer), DivisionID: testDivision, PublishedObjects: observed,
					})
				}
				ticker := simulation.NewTicker(source, bridge)
				ticker.Hooks = []simulation.NamedTickHook{{Hook: rt.ReleaseExpiredOwnership}, {Hook: rt.SweepExpired}}
				ticker.RunTick(drops[0].DroppedAt.Add(age).UnixMilli())
				opcode := wire.OpGroundOwnershipExpired
				if age == grounditem.FixtureLifetime {
					opcode = wire.OpObjectDespawn
				}
				for _, drop := range drops {
					payload := wire.NewWriter(4).U32(drop.Gid).Payload()
					actor.expectFrame(t, opcode, payload)
					peer.expectFrame(t, opcode, payload)
				}
			}
			if rt.Ground.Count(testDivision) != 0 {
				t.Fatal("expired authority retained loot")
			}
			for _, viewer := range []*transport.Session{actorSession, peerSession} {
				revision, _ := viewer.SceneRevision()
				observed, _ := viewer.PublishedObjects(revision)
				for _, drop := range drops {
					if slices.Contains(observed, drop.Gid) {
						t.Fatal("expired session retained loot")
					}
				}
			}
		})
	}
}

/*
================
wireBootstrapGroundObjects

Publishes ground drops into a viewer session through enterworld.GroundObjectListRows
(0x3417 OpcodeObjectListChunk), matching the EnterWorld bootstrap path before any
movement tick or peer/COS observation.
================
*/
func wireBootstrapGroundObjects(t *testing.T, srv *transport.Server, viewer *wireClient, items []grounditem.Item) {
	t.Helper()
	session, _ := srv.Hub.Session(viewer.sessionID)
	rows := enterworld.GroundObjectListRows(items)
	batch := make([]transport.Frame, len(rows))
	payloads := make([][]byte, len(rows))
	for i, row := range rows {
		payload := make([]byte, len(row.Payload))
		for j, b := range row.Payload {
			payload[j] = byte(b)
		}
		payloads[i] = payload
		batch[i] = transport.Frame{
			Opcode:  row.NativeOpcode,
			Payload: payload,
			Scope:   transport.ScopeChanges(row.Scope),
		}
	}
	if err := session.SendBatch(batch); err != nil {
		t.Fatal(err)
	}
	for _, payload := range payloads {
		viewer.expectFrame(t, enterworld.OpcodeObjectListChunk, payload)
	}
}

/*
================
TestGroundPickupDespawnReachesBootstrapViewerWithoutObservedPicker

A viewer that published a ground item via 0x3417 bootstrap without observing
the picking character or grab pet must still receive 0x36AB OpObjectDespawn
when the drop is picked up, matching SweepExpired.
================
*/
func TestGroundPickupDespawnReachesBootstrapViewerWithoutObservedPicker(t *testing.T) {
	t.Run("player-pickup", func(t *testing.T) {
		c := wireCharacter()
		rt, _ := newTestRuntime(c, testItems())
		server := wireStartServer(t, rt)

		drop := rt.Ground.Add(testDivision, grounditem.Item{
			RefObjID:   62,
			Codename:   "ITEM_ETC_GOLD_02",
			TypeFlags:  wire.PackTypeFlags(3, 3, 5, 2),
			GoldAmount: 50,
			Position:   grounditem.Point{RegionID: wireRegionID, X: wireX, Z: wireZ},
			Y:          wireY,
		})

		picker := wireConnect(t, server, testDivision, c.Name)
		viewer := wireConnect(t, server, testDivision, "bootstrap-viewer")
		wireBootstrapGroundObjects(t, server, viewer, rt.Ground.All(testDivision))

		viewerSession, _ := server.Hub.Session(viewer.sessionID)
		revision, _ := viewerSession.SceneRevision()
		observed, current := viewerSession.PublishedObjects(revision)
		if !current || !slices.Contains(observed, drop.Gid) {
			t.Fatalf("bootstrap viewer did not record drop %d in published scope: %v", drop.Gid, observed)
		}
		if slices.Contains(observed, enterworld.ObjectIDForCharacter(c)) {
			t.Fatalf("bootstrap viewer unexpectedly observed picker %d", enterworld.ObjectIDForCharacter(c))
		}

		picker.send(t, wire.OpTargetInteract, wire.TargetInteract{Gid: drop.Gid}.Encode())

		viewer.expectFrame(t, wire.OpObjectDespawn, wire.ObjectDespawn{Gid: drop.Gid}.Encode())
		if err := viewer.silence(50 * time.Millisecond); err != nil {
			t.Fatalf("bootstrap viewer received unexpected frame after despawn: %v", err)
		}

		observed, _ = viewerSession.PublishedObjects(revision)
		if slices.Contains(observed, drop.Gid) {
			t.Fatalf("bootstrap viewer retained picked drop %d in published scope: %v", drop.Gid, observed)
		}
	})

	t.Run("pet-arrival-pickup", func(t *testing.T) {
		c := wireCharacter()
		refs := testCosSource(testItems())
		refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 100}
		rt, _ := newTestRuntime(c, refs)
		nowMs := int64(1000)
		rt.Now = func() time.Time { return time.UnixMilli(nowMs) }
		rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }

		petGID, _ := enterworld.CosObjectIDForCharacter(c)
		c.ActiveCOS = &enterworld.CharacterCOS{
			GID: petGID, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
			Container: &domain.COSContainer{Capacity: 8},
		}
		rt.BindPetSession(testDivision, c, 1)
		rt.TickHook()(nowMs)
		pose := rt.PetPresentation(testDivision, c.Name).World.Spawn

		drop := rt.Ground.Add(testDivision, grounditem.Item{
			RefObjID:   3630,
			Codename:   "ITEM_ETC_HP_POTION_01",
			TypeFlags:  wire.PackTypeFlags(3, 3, 1, 1),
			StackCount: 1,
			Position:   grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X + 100), Z: float32(pose.Z)},
			Y:          float32(pose.Y),
		})

		server := wireStartServer(t, rt)
		viewer := wireConnect(t, server, testDivision, "bootstrap-viewer")
		wireBootstrapGroundObjects(t, server, viewer, rt.Ground.All(testDivision))

		viewerSession, _ := server.Hub.Session(viewer.sessionID)
		revision, _ := viewerSession.SceneRevision()
		observed, current := viewerSession.PublishedObjects(revision)
		if !current || !slices.Contains(observed, drop.Gid) {
			t.Fatalf("bootstrap viewer did not record drop %d in published scope: %v", drop.Gid, observed)
		}
		if slices.Contains(observed, petGID) || slices.Contains(observed, enterworld.ObjectIDForCharacter(c)) {
			t.Fatalf("bootstrap viewer unexpectedly observed pet or owner: %v", observed)
		}

		payload, err := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosPickup, CosGID: petGID, GroundGID: drop.Gid}.Encode()
		if err != nil {
			t.Fatal(err)
		}
		res := rt.HandleItemMove(testDivision, c, payload)
		if _, stillOnGround := rt.Ground.Get(testDivision, drop.Gid); !stillOnGround {
			t.Fatalf("distant drop was picked up synchronously before pet arrival: %+v", res.Frames)
		}

		source := tickSessionSource{
			sessions: []simulation.SessionSnapshot{{
				SessionID:        worldsession.SessionSceneID(viewerSession),
				DivisionID:       testDivision,
				CharacterID:      999,
				PublishedObjects: observed,
			}},
		}
		ticker := simulation.NewTicker(source, worldsession.New(server.Hub))
		ticker.Hooks = []simulation.NamedTickHook{{Hook: rt.advancePets}}
		nowMs = 1100
		ticker.RunTick(nowMs)
		if _, stillOnGround := rt.Ground.Get(testDivision, drop.Gid); !stillOnGround {
			t.Fatal("pet departure tick consumed drop before arrival")
		}
		nowMs = 2200
		ticker.RunTick(nowMs)

		if _, stillOnGround := rt.Ground.Get(testDivision, drop.Gid); stillOnGround {
			t.Fatal("pet arrival tick did not consume ground drop")
		}
		viewer.expectFrame(t, wire.OpObjectDespawn, wire.ObjectDespawn{Gid: drop.Gid}.Encode())
		if err := viewer.silence(50 * time.Millisecond); err != nil {
			t.Fatalf("bootstrap viewer received unexpected frame after pet despawn: %v", err)
		}

		observed, _ = viewerSession.PublishedObjects(revision)
		if slices.Contains(observed, drop.Gid) {
			t.Fatalf("bootstrap viewer retained pet-picked drop %d in published scope: %v", drop.Gid, observed)
		}
	})
}
