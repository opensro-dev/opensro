package action

import (
	"math"
	"sort"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// Keep a zero-HP source registry-live briefly so the client can present a
// corpse before the ordinary despawn broadcast. This is server registry
// lifecycle pacing, not an animation clock: the retail server's absorbing
// death state path has no BAN-duration lookup, and shipped death clips are
// not uniformly shorter than this interval. Client CAniMixer completion,
// death-loop handoff, reward effects, and the later 1.5 s dissolve remain
// separate owners. The monster mover already excludes zero-HP instances.
const monsterDeathPresentationRetention = 2 * time.Second

type pendingMonsterDefeat struct {
	divisionID string
	gid        uint32
	dueAtMs    int64
}

// monsterKillReward derives the two independent retail progression lanes.
// This is the full-damage solo arm of the reference server's chain:
//
//	the monster's solo kill reward
//	  -> the player's monster kill rewards
//	     -> the monster EXP formula
//	     -> the monster SEXP formula
//
// RefObjChar.ExpToGive remains the monster authority, but reward rarity is a
// dedicated native table (it is not the MaxHP multiplier). Both lanes share
// the mastery-gap, monster/player-level and rarity factors. SEXP additionally
// multiplies by 100 and divides by the level table's GUST_Mob_Exp; there is no 1:6
// shortcut in the retail pipeline. Independent factors spill to float32.
// EXP converts the product directly to int64; SEXP first spills
// the product to float32 and then converts to a signed dword.
func monsterKillReward(
	character *enterworld.Character,
	target monster.Instance,
	levels enterworld.LevelDataSource,
) (int64, int64) {
	return monsterContributionReward(character, target, levels, target.EffectiveMaxHP(), 1, false)
}

func monsterContributionReward(character *enterworld.Character, target monster.Instance, levels enterworld.LevelDataSource, damage uint32, recipientFactor float32, expShare bool) (int64, int64) {
	if damage == 0 || target.EffectiveMaxHP() == 0 {
		return 0, 0
	}
	if character == nil || !target.Ref.RewardActionPinned || target.Ref.ExpToGive == 0 {
		return 0, 0
	}
	if levels == nil {
		return 0, 0
	}
	playerLevel := int64(1)
	if character.Level != nil && *character.Level > 0 {
		playerLevel = *character.Level
	}
	mobExpBasis, basisOK := levels.MonsterExpBasis(playerLevel)
	if !basisOK || mobExpBasis <= 0 {
		return 0, 0
	}

	maxMastery := int64(0)
	for _, mastery := range character.Masteries {
		if mastery.Level > maxMastery {
			maxMastery = mastery.Level
		}
	}
	expRate, skillExpRate := masteryGapProgressionRates(playerLevel, maxMastery)
	levelScale := monsterLevelGapRewardScale(playerLevel, int64(target.Ref.Level))
	relativeLevelBonus := monsterRelativeLevelBonus(playerLevel, int64(target.Ref.Level))
	if expShare {
		relativeLevelBonus = monsterPartyRelativeLevelBonus(playerLevel, int64(target.Ref.Level), enterworld.NativeCountryByte9C(character) == 1)
	}
	// Natively unsigned damage is converted to float32, clamped to
	// max HP, divided and spilled before the factor chain. It is not a
	// fraction of total damage from the competing groups.
	damageFactor := float32(min(float32(damage), float32(target.EffectiveMaxHP())) / float32(target.EffectiveMaxHP()))
	// Natively both lanes load float32 1e-6, then clamp to 1.
	damageFactor = max(float32(1e-6), damageFactor)
	gradeMultiplier := monsterRewardGradeMultiplier(target.Rarity())
	// Natively base*grade is stored to a dword before the EXP factor chain.
	expBase := float32(float64(target.Ref.ExpToGive) * float64(gradeMultiplier))
	exp := nativePositiveExpReward(
		float64(expBase) *
			float64(damageFactor) *
			float64(levelScale) *
			float64(expRate) *
			float64(relativeLevelBonus),
	)
	// The SEXP arm keeps base*grade*100/basis in x87 until it multiplies the
	// same independently spilled factor set, then stores the finished value
	// to one dword.
	skillExp := nativePositiveReward(float32(
		float64(target.Ref.ExpToGive) *
			float64(gradeMultiplier) *
			100 / float64(mobExpBasis) *
			float64(damageFactor) *
			float64(levelScale) *
			float64(skillExpRate) *
			float64(relativeLevelBonus),
	))
	// Natively each formula result is converted back to float32
	// before the distributor's recipient factor and final truncation.
	return int64(float64(float32(exp)) * float64(recipientFactor)), int64(nativeRewardDword(float64(float32(skillExp)) * float64(recipientFactor)))
}

// The native EXP formula calls the 64-bit conversion helper, then replaces nonpositive
// results with one. There is no signed-dword saturation in this EXP lane.
func nativePositiveExpReward(value float64) int64 {
	if math.IsNaN(value) || value < 1 || value >= 0x1p63 {
		return 1
	}
	return int64(value)
}

func monsterPartyRelativeLevelBonus(playerLevel, monsterLevel int64, european bool) float32 {
	if monsterLevel+3 <= playerLevel {
		return 1
	}
	gap := monsterLevel - playerLevel
	extra := float32(0)
	switch {
	case gap >= 9:
		extra = .45
	case gap >= 7:
		extra = .30
	case gap >= 5:
		extra = .15
	}
	if european {
		extra *= 2
	}
	base := float32(float64(extra) + 1)
	return min(float32(4), max(float32(1), float32(float64(base)+float64(min(int64(13), gap+3))*float64(float32(.03)))))
}

func nativePositiveReward(value float32) int64 {
	// The native SEXP formula clamps the floating value before conversion. An
	// unordered comparison does not enter the clamp arm.
	if value < 1 {
		value = 1
	}
	return int64(nativeRewardDword(float64(value)))
}

// The native float conversion's SSE2 path (the native execution profile on the x64 server host)
// stores to float64, then CVTTSD2SI returns a signed dword. Invalid inputs
// produce 80000000; neither saturation nor Go's platform-dependent cast is
// the instruction's contract. The kill EXP grant uses the same helper after sharing.
func nativeRewardDword(value float64) int32 {
	if math.IsNaN(value) || value >= 0x1p31 || value <= -0x1p31-1 {
		return math.MinInt32
	}
	return int32(value)
}

// monsterRewardGradeMultiplier ports the native reward grade multiplier.
// The high party-monster nibble contributes x7.5. The low rarity nibble is
// independent from HP scaling: champion x2, giant x15, titan x60, elite x4,
// and type 7 x30. Static types 0/3/8 retain the current party factor.
func monsterRewardGradeMultiplier(rarity uint8) float32 {
	multiplier := float32(1)
	if rarity>>4 == 1 {
		multiplier = 7.5
	}
	switch rarity & 0x0f {
	case 0, 3, 8:
		return multiplier
	case 1:
		return multiplier * 2
	case 4:
		return multiplier * 15
	case 5:
		return multiplier * 60
	case 6:
		return multiplier * 4
	case 7:
		return multiplier * 30
	default:
		return 1
	}
}

// masteryGapProgressionRates ports the native mastery-gap progression rates.
// A positive level-minus-mastery gap trades 10% EXP for 10% SEXP per level;
// native clamps both signed directions to 0.1..1.9 instead of assuming the
// persisted mastery can never be ahead of the character level.
func masteryGapProgressionRates(level, maxMastery int64) (float32, float32) {
	gap := float32(level - maxMastery)
	expRate := float32(1) - gap*0.1
	skillExpRate := float32(1) + gap*0.1
	return clampRewardRate(expRate), clampRewardRate(skillExpRate)
}

func clampRewardRate(value float32) float32 {
	if value < 0.1 {
		return 0.1
	}
	if value > 1.9 {
		return 1.9
	}
	return value
}

// monsterLevelGapRewardScale ports the native monster level-gap reward scale.
// Monsters up through ten levels above the player retain full credit, then
// lose 2% per additional level. Monsters four-to-six levels below give 90%;
// the seven-plus arm applies 15% per level beyond the first three. Native
// clamps the resulting scale to 0.1..1.
func monsterLevelGapRewardScale(playerLevel, monsterLevel int64) float32 {
	scale := float32(1)
	if monsterLevel > playerLevel+10 {
		scale -= float32(monsterLevel-playerLevel-10) * 0.02
	} else if playerLevel > monsterLevel+3 {
		gap := playerLevel - monsterLevel
		if gap < 7 {
			scale = 0.9
		} else {
			scale -= float32(gap-3) * 0.15
		}
	}
	if scale < 0.1 {
		return 0.1
	}
	if scale > 1 {
		return 1
	}
	return scale
}

// monsterRelativeLevelBonus is the non-party arm of
// the native relative level bonus. Current action has no party
// contribution owner, so applying the native party-only 0.15/0.30/0.45
// additions here would fabricate a group. The common relative-level term is
// still exact and shared by Chinese and European characters.
func monsterRelativeLevelBonus(playerLevel, monsterLevel int64) float32 {
	if monsterLevel+3 <= playerLevel {
		return 1
	}
	steps := monsterLevel - playerLevel + 3
	if steps > 13 {
		steps = 13
	}
	// Natively the step count times the double 0.03 (float32 .03
	// widened) adds to 1 on the x87 stack; only the sum is stored to float32.
	bonus := float32(1 + float64(steps)*float64(float32(.03)))
	if bonus < 1 {
		return 1
	}
	if bonus > 4 {
		return 4
	}
	return bonus
}

/*
==================
applyMonsterKillInsideDoor

Applies one recipient's kill products - progression and the planned ground
drops - without opening a door of its own. Quest credit has its own owner
(creditQuestKillInsideDoor). The caller,
settleMonsterInsideDoor, already holds the fatal hit's single UpdateMany
door, so bow/crossbow debit, progression and the ground rows land in the
same character+ground transaction and no later tick owns the reward.
==================
*/
func (rt *Runtime) applyMonsterKillInsideDoor(
	divisionID string,
	character *enterworld.Character,
	exp, skillExp int64,
	defeatGid uint32,
	plannedDrops []grounditem.Item,
) (frames []wire.Frame, added []grounditem.Item, changed bool) {
	if character == nil || character.DeletePending {
		return nil, nil, false
	}
	if rt.UpdateExperience != nil && (exp != 0 || skillExp != 0) {
		var progressionChanged bool
		frames, progressionChanged = rt.UpdateExperience(character, exp, skillExp, defeatGid)
		changed = progressionChanged
	}
	for _, plannedDrop := range plannedDrops {
		spawned := rt.addMonsterGround(divisionID, defeatGid, plannedDrop)
		if spawned.Gid == 0 {
			continue
		}
		added = append(added, spawned)
		changed = true
	}
	return frames, added, changed
}

/*
==================
creditQuestKillInsideDoor

The original raises the quest kill event for the player whose blow killed
the monster, not for the reward representative: that player's kill
missions count it and its gather missions roll their quest drops, owned by
it. In a party with both EXP and item auto-share, the same event is
shared by the quest's DivideType instead (shareQuestKill): the killer
alone is never the whole story there. The caller holds the fatal hit's
door, as for applyMonsterKillInsideDoor.
==================
*/
func (rt *Runtime) creditQuestKillInsideDoor(
	divisionID string,
	killer *enterworld.Character,
	target monster.Instance,
	pose monster.Pose,
	now int64,
) (frames []wire.Frame, added []grounditem.Item) {
	if killer == nil || killer.DeletePending {
		return nil, nil
	}
	rt.noteRocKill(target.Ref.Codename, target.Gid, pose)
	live, alive := monster.Instance{}, false
	if rt.Monsters != nil {
		live, alive = rt.Monsters.Get(divisionID, target.Gid)
	}
	if !alive || live.CurrentHP != 0 {
		return frames, added
	}
	if rt.NoteQuestKillSite != nil {
		rt.NoteQuestKillSite(killer, live.Ref.Codename, simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z})
	}
	if shareFrames, shareDrops, shared := rt.shareQuestKill(divisionID, killer, target, pose, now); shared {
		frames = append(frames, shareFrames...)
		added = append(added, shareDrops...)
		return frames, added
	}
	if rt.UpdateQuestKill != nil {
		questFrames, _ := rt.UpdateQuestKill(killer, live.Ref.Codename, live.Rarity())
		frames = append(frames, questFrames...)
	}
	for _, plannedDrop := range rt.planQuestKillDrops(killer, target, pose, now) {
		if spawned := rt.addMonsterGround(divisionID, target.Gid, plannedDrop); spawned.Gid != 0 {
			added = append(added, spawned)
		}
	}
	return frames, added
}

// questKillShareOptions are the two auto-share bits of a party (EXP and
// item share) the native kill fan-out requires together.
const questKillShareOptions = 0x3

// questKillShareReach is the native near test of the fan-out: the same
// world as the killer and within 1,000 in 3D of him.
const questKillShareReach = 1000.0

/*
================
QuestKillTaker

One quest of a character that takes a kill: its RefID, its DivideType
(the kill-share mode) and whether it gathers from the kill instead of
counting it. The composition converts the quest runtime's own list.
================
*/
type QuestKillTaker struct {
	RefID      uint32
	DivideType uint8
	Drop       bool
}

/*
================
questKillTurnKey

One (party, quest) rotation of the DivideType-0 share.
================
*/
type questKillTurnKey struct {
	division string
	party    uint64
	refID    uint32
}

/*
================
questKillTurnEntry

One rotation's state: the roster index to try first next time, and
the last kill (ms) that consulted it. The table is bounded
(pruneQuestKillTurns): the native keeps its lists on the party and
frees them with it; the port's parties are rebuilt by the registry,
so an entry no kill has consulted for a day belongs to a party that
is gone, and past the cap those are the entries dropped.
================
*/
type questKillTurnEntry struct {
	next   uint32
	seenMs int64
}

const (
	questKillTurnMaxEntries = 4096
	questKillTurnIdleMs     = 24 * 60 * 60 * 1000
)

/*
================
questKillHolder
================
*/
type questKillHolder struct {
	c     *enterworld.Character
	taker QuestKillTaker
	near  bool
}

/*
================
shareQuestKill

The native kill fan-out in an auto-share party: per quest, the holders
are the party members whose quest takes the kill, and the quest's
DivideType (0 in turn, 1/4/5 every near holder, 2/3 the killer alone)
picks the credited among the same-world, near ones. Each credited
member counts the kill and rolls his own gather drops. Returns
true when the party shares (EXP and item), claiming the whole credit;
false leaves the kill with the killer, as today. The holder lists are
rebuilt from the party's live roster each kill; the native keeps them
per quest on the party, which differs only when the roster changes
between two kills of one rotation.

The rotations live on the runtime, keyed by the party's order: a
party the composition rebuilds (relog) restarts its rotations, and
the table is bounded (pruneQuestKillTurns) where the native frees
its lists with the party itself.
================
*/
func (rt *Runtime) shareQuestKill(divisionID string, killer *enterworld.Character, target monster.Instance, pose monster.Pose, now int64) ([]wire.Frame, []grounditem.Item, bool) {
	if rt.QuestKillTakers == nil || rt.UpdateQuestKillFor == nil {
		return nil, nil, false
	}
	rt.pruneQuestKillTurns(now)
	party := rt.rewardPartyOf(divisionID, enterworld.ObjectIDForCharacter(killer))
	if party == nil || party.Options&questKillShareOptions != questKillShareOptions {
		return nil, nil, false
	}
	killerSnapshot := rt.characterSnapshot(divisionID, killer)
	if killerSnapshot == nil {
		return nil, nil, false
	}
	killerWorld := domain.CharacterWorldInstance(killerSnapshot)
	origin := rt.liveSpawn(simulation.WorldKey(divisionID, killer.Name), killer, now)
	holders := map[uint32][]questKillHolder{}
	var refIDs []uint32
	for _, gid := range party.Members {
		member := rt.findCharacterByGid(divisionID, gid)
		if member == nil {
			continue
		}
		snapshot := rt.characterSnapshot(divisionID, member)
		if snapshot == nil {
			continue
		}
		near := domain.CharacterWorldInstance(snapshot) == killerWorld &&
			distance3D(origin, rt.liveSpawn(simulation.WorldKey(divisionID, member.Name), member, now)) <= questKillShareReach
		for _, taker := range rt.QuestKillTakers(member, target.Ref.Codename, target.Rarity()) {
			if _, seen := holders[taker.RefID]; !seen {
				refIDs = append(refIDs, taker.RefID)
			}
			holders[taker.RefID] = append(holders[taker.RefID], questKillHolder{c: member, taker: taker, near: near})
		}
	}
	sort.Slice(refIDs, func(i, j int) bool { return refIDs[i] < refIDs[j] })
	progress := map[*enterworld.Character]map[uint32]bool{}
	drops := map[*enterworld.Character]map[uint32]bool{}
	name := map[*enterworld.Character]bool{}
	credit := func(h questKillHolder) {
		sets := progress
		if h.taker.Drop {
			sets = drops
		}
		if sets[h.c] == nil {
			sets[h.c] = map[uint32]bool{}
			name[h.c] = true
		}
		sets[h.c][h.taker.RefID] = true
	}
	killerGID := enterworld.ObjectIDForCharacter(killer)
	for _, refID := range refIDs {
		hs := holders[refID]
		switch hs[0].taker.DivideType {
		case 2, 3:
			for _, h := range hs {
				if enterworld.ObjectIDForCharacter(h.c) == killerGID {
					credit(h)
				}
			}
		case 1, 4, 5:
			for _, h := range hs {
				if h.near {
					credit(h)
				}
			}
		default:
			for _, picked := range rt.questKillTurn(divisionID, party.Order, refID, hs, now) {
				credit(picked)
			}
		}
	}
	var frames []wire.Frame
	if rt.NoteQuestKillChains != nil {
		frames = append(frames, rt.NoteQuestKillChains(killer, target.Ref.Codename)...)
	}
	var members []*enterworld.Character
	for c := range name {
		members = append(members, c)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
	var added []grounditem.Item
	for _, member := range members {
		memberFrames, memberDrops := rt.applySharedKillCredit(divisionID, killer, member, target, pose, now, progress[member], drops[member])
		added = append(added, memberDrops...)
		if enterworld.ObjectIDForCharacter(member) == killerGID {
			frames = append(frames, memberFrames...)
		} else if len(memberFrames) > 0 && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(divisionID, member.Name, memberFrames)
		}
	}
	return frames, added, true
}

/*
================
questKillTurn

The next credited holder of one quest in turn: from the party's index
for the quest, the first near holder (the roster wraps); the index
moves past him. Nobody near credits nobody.
================
*/
func (rt *Runtime) questKillTurn(divisionID string, partyOrder uint64, refID uint32, hs []questKillHolder, now int64) []questKillHolder {
	if len(hs) == 0 {
		return nil
	}
	key := questKillTurnKey{division: divisionID, party: partyOrder, refID: refID}
	rt.questKillTurnsMu.Lock()
	if rt.questKillTurns == nil {
		rt.questKillTurns = map[questKillTurnKey]questKillTurnEntry{}
	}
	entry := rt.questKillTurns[key]
	entry.seenMs = now
	rt.questKillTurns[key] = entry
	start := int(entry.next) % len(hs)
	rt.questKillTurnsMu.Unlock()
	for step := 0; step < len(hs); step++ {
		h := hs[(start+step)%len(hs)]
		if !h.near {
			continue
		}
		rt.questKillTurnsMu.Lock()
		entry = rt.questKillTurns[key]
		entry.next = uint32(start + step + 1)
		entry.seenMs = now
		rt.questKillTurns[key] = entry
		rt.questKillTurnsMu.Unlock()
		return []questKillHolder{h}
	}
	return nil
}

/*
================
pruneQuestKillTurns

Bound the rotation table: past the entry cap, rotations no kill has
consulted for a day are dropped. Runs on the sharing lane only, and
only pays the scan when the table is actually oversized.
================
*/
func (rt *Runtime) pruneQuestKillTurns(now int64) {
	rt.questKillTurnsMu.Lock()
	defer rt.questKillTurnsMu.Unlock()
	if len(rt.questKillTurns) <= questKillTurnMaxEntries {
		return
	}
	for key, entry := range rt.questKillTurns {
		if now-entry.seenMs > questKillTurnIdleMs {
			delete(rt.questKillTurns, key)
		}
	}
}

/*
================
applySharedKillCredit

One credited member of a shared kill: his counted quests advance (in
his own character door when he is not the killer, whose door the
caller holds) and his gather drops are planned and spawned, owned by
him. Returns his frames and the spawned drops.
================
*/
func (rt *Runtime) applySharedKillCredit(divisionID string, killer, member *enterworld.Character, target monster.Instance, pose monster.Pose, now int64, progress, dropQuests map[uint32]bool) ([]wire.Frame, []grounditem.Item) {
	creditFrames := func() []wire.Frame {
		// The event reached this member (a counted quest or a gather
		// roll), so his kill exchanges run even when no mission of his
		// counted: an empty credit set, never nil, keeps the missions
		// out and the exchanges in.
		only := progress
		if only == nil {
			only = map[uint32]bool{}
		}
		return rt.UpdateQuestKillFor(member, target.Ref.Codename, target.Rarity(), only)
	}
	var frames []wire.Frame
	if enterworld.ObjectIDForCharacter(member) == enterworld.ObjectIDForCharacter(killer) {
		frames = creditFrames()
	} else {
		rt.deps.Update(member, "quest-kill-share", func() bool {
			frames = creditFrames()
			return len(frames) > 0
		})
	}
	var added []grounditem.Item
	if len(dropQuests) > 0 {
		for _, plannedDrop := range rt.planQuestKillDropsFor(member, target, pose, now, dropQuests) {
			if spawned := rt.addMonsterGround(divisionID, target.Gid, plannedDrop); spawned.Gid != 0 {
				added = append(added, spawned)
			}
		}
	}
	return frames, added
}

func (rt *Runtime) queueMonsterDefeat(divisionID string, gid uint32, dueAtMs int64) {
	if divisionID == "" || gid == 0 {
		return
	}
	// Natively a structure keeps standing in its destroyed state.
	if rt.Monsters != nil {
		if row, ok := rt.Monsters.Get(divisionID, gid); ok && row.Ref.Structure {
			rt.queueStructureDeath(divisionID, gid)
			return
		}
	}
	rt.pendingMonsterDefeatsMu.Lock()
	rt.pendingMonsterDefeats = append(rt.pendingMonsterDefeats, pendingMonsterDefeat{
		divisionID: divisionID,
		gid:        gid,
		dueAtMs:    dueAtMs,
	})
	rt.pendingMonsterDefeatsMu.Unlock()
}

func (rt *Runtime) drainMonsterDefeats(nowMs int64) {
	rt.pendingMonsterDefeatsMu.Lock()
	kept := rt.pendingMonsterDefeats[:0]
	var due []pendingMonsterDefeat
	for _, pending := range rt.pendingMonsterDefeats {
		if pending.dueAtMs > nowMs {
			kept = append(kept, pending)
			continue
		}
		due = append(due, pending)
	}
	rt.pendingMonsterDefeats = kept
	rt.pendingMonsterDefeatsMu.Unlock()

	if rt.Monsters == nil {
		return
	}
	for _, pending := range due {
		rt.Monsters.Defeat(
			pending.divisionID,
			pending.gid,
			time.UnixMilli(nowMs),
		)
	}
}
