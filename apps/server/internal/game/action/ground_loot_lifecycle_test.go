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
				ticker.Hooks = []simulation.TickHook{rt.ReleaseExpiredOwnership, rt.SweepExpired}
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
