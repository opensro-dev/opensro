/*
===========================================================================

monsterabnormal_roster_test.go - the abnormal pass shares one reward roster

The abnormal pass used to rebuild the whole division reward roster for
every monster with an active block, although the roster is a division
snapshot the first candidate of the pass already paid for. These tests
pin the sharing: one build per division per pass, and settlements that
stay per-monster (a companion's kill still suppresses only its own
quest credit) while riding the shared snapshot.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

/*
================
seedMonsterAbnormal

Install one abnormal record on a live monster with its source already
named, keeping the source registered so the tick's hits stay credited
(seedDepartedAbnormal forgets the source; these tests need the credit).
creditGID is the contribution credit the prepared source carries: the
owner's gid for a companion's record, the caster's own otherwise.
================
*/
func seedMonsterAbnormal(t *testing.T, rt *Runtime, gid uint32, record abnormal.Record, creditGID uint32) {
	t.Helper()
	current, _ := rt.Monsters.Get(testDivision, gid)
	plan := []simulation.MonsterDamagePlan{{GID: gid, CreditGID: creditGID, Abnormal: []abnormal.Record{record}, AbnormalSources: rt.Monsters.PrepareAbnormalSources(testDivision, []abnormal.Record{record})}}
	if r := rt.Monsters.ApplyDamageSequence(testDivision, gid, current.CurrentHP, plan); len(r) != 1 || r[0].Instance.Abnormal == nil {
		t.Fatal("status admission")
	}
}

/*
================
playerBurnRecord

The burn the combat fixtures kill with: caster-credited to the player.
================
*/
func playerBurnRecord(c *enterworld.Character) abnormal.Record {
	return abnormal.Record{Status: abnormal.Burn, Level: 100, DurationMs: 100 * 750, Rate24: 8, Scale20: 1,
		SourceGID: enterworld.ObjectIDForCharacter(c), SourceName: c.Name}
}

/*
================
TestAbnormalPassBuildsOneRosterPerDivision

Two monsters of one division carry burns into one pass. The reward
parties feed is the roster build's one per-build call, so it counts
builds exactly: the pass must build once, not once per monster, and
both monsters must still burn. Reverting to per-monster builds fails
the count.
================
*/
func TestAbnormalPassBuildsOneRosterPerDivision(t *testing.T) {
	rt, clock, actor, target := newCombatTestRuntime(t, 100000)
	at := rt.LiveSpawnFor(testDivision, actor)
	at.X += 30
	extra := wolfSpawnAt(t, rt, actor, at, clock.NowMs())
	record := playerBurnRecord(actor)
	seedMonsterAbnormal(t, rt, target.Gid, record, record.SourceGID)
	seedMonsterAbnormal(t, rt, extra.Gid, record, record.SourceGID)
	builds := 0
	rt.RewardParties = func(string) []RewardParty { builds++; return nil }
	rt.advanceMonsterAbnormals(clock.NowMs())
	if builds != 1 {
		t.Fatalf("roster builds in one pass = %d, want 1", builds)
	}
	for _, gid := range []uint32{target.Gid, extra.Gid} {
		after, _ := rt.Monsters.Get(testDivision, gid)
		if after.CurrentHP >= 100000 {
			t.Fatalf("monster %d never burned (%d HP)", gid, after.CurrentHP)
		}
	}
}

/*
================
TestAbnormalPassSettlesTwoKillsOnOneRoster

Two caster-credited burns kill in the same pass. Both kills must pay
experience and quest credit through the one shared roster: the first
settlement may not drain or consume what the second one reads.
================
*/
func TestAbnormalPassSettlesTwoKillsOnOneRoster(t *testing.T) {
	rt, clock, actor, target := newCombatTestRuntime(t, 1)
	at := rt.LiveSpawnFor(testDivision, actor)
	at.X += 30
	extra := wolfSpawnAt(t, rt, actor, at, clock.NowMs())
	record := playerBurnRecord(actor)
	seedMonsterAbnormal(t, rt, target.Gid, record, record.SourceGID)
	seedMonsterAbnormal(t, rt, extra.Gid, record, record.SourceGID)
	installSmallGoldRef(rt)
	rt.DropRoll = constantDropRoll(0)
	var expCalls, questCalls int
	rt.UpdateExperience = func(c *enterworld.Character, exp, sexp int64, _ uint32) ([]wire.Frame, bool) {
		expCalls++
		return []wire.Frame{{Opcode: wire.OpExpUpdate}}, true
	}
	rt.UpdateQuestKill = func(c *enterworld.Character, _ string, _ uint8) ([]wire.Frame, bool) {
		questCalls++
		return nil, true
	}
	rt.advanceMonsterAbnormals(clock.NowMs())
	for _, gid := range []uint32{target.Gid, extra.Gid} {
		after, _ := rt.Monsters.Get(testDivision, gid)
		if after.CurrentHP != 0 {
			t.Fatalf("monster %d survived the pass (%d HP)", gid, after.CurrentHP)
		}
	}
	if expCalls != 2 || questCalls != 2 {
		t.Fatalf("settlements on the shared roster: exp %d, quest %d, want 2 and 2", expCalls, questCalls)
	}
}

/*
================
TestAbnormalPassCompanionKillKeepsItsQuestSuppression

A companion's burn and the player's own burn kill in one pass, the
companion's monster first (candidates run by gid). The companion kill
pays the owner experience but no quest credit; the player's kill,
settled afterwards on the same shared roster, must still earn its
quest credit. Writing the companion verdict onto the shared roster
instead of the settling copy fails the quest count.
================
*/
func TestAbnormalPassCompanionKillKeepsItsQuestSuppression(t *testing.T) {
	rt, clock, actor, target := newCombatTestRuntime(t, 1)
	at := rt.LiveSpawnFor(testDivision, actor)
	at.X += 30
	extra := wolfSpawnAt(t, rt, actor, at, clock.NowMs())
	const cosGID = uint32(0x00C05)
	actor.ActiveCOS = &domain.CharacterCOS{GID: cosGID, Summoned: true}
	companionRecord := playerBurnRecord(actor)
	companionRecord.SourceGID, companionRecord.SourceName = cosGID, ""
	playerRecord := playerBurnRecord(actor)
	// Candidates run by ascending gid; the companion's monster goes first.
	first, second := target, extra
	if extra.Gid < target.Gid {
		first, second = extra, target
	}
	seedMonsterAbnormal(t, rt, first.Gid, companionRecord, enterworld.ObjectIDForCharacter(actor))
	seedMonsterAbnormal(t, rt, second.Gid, playerRecord, playerRecord.SourceGID)
	installSmallGoldRef(rt)
	rt.DropRoll = constantDropRoll(0)
	var expCalls, questCalls int
	rt.UpdateExperience = func(c *enterworld.Character, exp, sexp int64, _ uint32) ([]wire.Frame, bool) {
		expCalls++
		return []wire.Frame{{Opcode: wire.OpExpUpdate}}, true
	}
	rt.UpdateQuestKill = func(c *enterworld.Character, _ string, _ uint8) ([]wire.Frame, bool) {
		questCalls++
		return nil, true
	}
	rt.advanceMonsterAbnormals(clock.NowMs())
	for _, gid := range []uint32{target.Gid, extra.Gid} {
		after, _ := rt.Monsters.Get(testDivision, gid)
		if after.CurrentHP != 0 {
			t.Fatalf("monster %d survived the pass (%d HP)", gid, after.CurrentHP)
		}
	}
	if expCalls != 2 {
		t.Fatalf("both kills pay the owner: exp calls = %d, want 2", expCalls)
	}
	if questCalls != 1 {
		t.Fatalf("only the player's own kill credits the quest: quest calls = %d, want 1", questCalls)
	}
}

/*
================
TestAbnormalPassQuestCreditLandsOnPlayersKill

Same geometry as the suppression pin (the companion's monster settles
first, the player's own second, one shared roster), but watching the
kill site instead of the credit count. Exactly one quest credit is
expected and it must name the player's monster. A credit count cannot
see a companion verdict written onto the shared roster instead of the
settling copy: the companion's kill takes the credit and the player's
is suppressed - same total, wrong kill.
================
*/
func TestAbnormalPassQuestCreditLandsOnPlayersKill(t *testing.T) {
	rt, clock, actor, target := newCombatTestRuntime(t, 1)
	at := rt.LiveSpawnFor(testDivision, actor)
	at.X += 30
	extra := wolfSpawnAt(t, rt, actor, at, clock.NowMs())
	const cosGID = uint32(0x00C05)
	actor.ActiveCOS = &domain.CharacterCOS{GID: cosGID, Summoned: true}
	companionRecord := playerBurnRecord(actor)
	companionRecord.SourceGID, companionRecord.SourceName = cosGID, ""
	playerRecord := playerBurnRecord(actor)
	// Candidates run by ascending gid; the companion's monster goes first.
	first, second := target, extra
	if extra.Gid < target.Gid {
		first, second = extra, target
	}
	seedMonsterAbnormal(t, rt, first.Gid, companionRecord, enterworld.ObjectIDForCharacter(actor))
	seedMonsterAbnormal(t, rt, second.Gid, playerRecord, playerRecord.SourceGID)
	installSmallGoldRef(rt)
	rt.DropRoll = constantDropRoll(0)
	fellAt := make(map[uint32]simulation.Spawn)
	for _, gid := range []uint32{first.Gid, second.Gid} {
		mover, found := rt.Monsters.Mover(testDivision, gid)
		if !found {
			t.Fatalf("monster %d has no mover to die at", gid)
		}
		pose := mover.LivePoseAt(clock.NowMs(), nil)
		fellAt[gid] = simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
	}
	var questCalls int
	rt.UpdateQuestKill = func(c *enterworld.Character, _ string, _ uint8) ([]wire.Frame, bool) {
		questCalls++
		return nil, true
	}
	var sites []simulation.Spawn
	rt.NoteQuestKillSite = func(_ *enterworld.Character, _ string, fell simulation.Spawn) {
		sites = append(sites, fell)
	}
	rt.advanceMonsterAbnormals(clock.NowMs())
	if questCalls != 1 || len(sites) != 1 {
		t.Fatalf("quest credits = %d (sites %d), want exactly 1", questCalls, len(sites))
	}
	site, want := sites[0], fellAt[second.Gid]
	if site.RegionID != want.RegionID || site.X != want.X || site.Z != want.Z {
		t.Fatalf("quest credit landed on the companion's monster at %+v, want the player's own kill at %+v", site, want)
	}
}
