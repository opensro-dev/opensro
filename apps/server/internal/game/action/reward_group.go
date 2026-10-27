/*
===========================================================================

reward_group.go - contribution groups, party shares and kill settlement

Resolve recipients from one live roster and commit their rewards inside the
damage transaction. The winning contributor supplies loot ownership and gold
bonuses, independently of the actor whose strike happened to be fatal.

===========================================================================
*/
package action

import (
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/pk"
	"reflect"
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/tunables"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
RewardParty

Order preserves process-local party identity across leader changes. It models
the native temporary tree's pointer ordering and is never sent or persisted.
================
*/
type RewardParty struct {
	Order    uint64
	Options  uint8
	LeaderID uint32
	Members  []uint32
}

/*
================
rewardActor
================
*/
type rewardActor struct {
	world     uint32
	character *enterworld.Character
	pose      simulation.Spawn
	party     *RewardParty
}

/*
================
rewardRoster
================
*/
type rewardRoster struct {
	division   string
	actors     map[uint32]rewardActor
	characters []*enterworld.Character
	// companionKill: the fatal hit came from a pet or COS, which the
	// original's quest kill event never credits (creditQuestKillInsideDoor).
	companionKill bool
}

/*
================
monsterRewardRoster

The caller owns the division operation lock. Snapshot membership once, retain
authoritative character pointers, then enter a single UpdateMany transaction.
================
*/
func (rt *Runtime) monsterRewardRoster(division string, actor *enterworld.Character, now int64) rewardRoster {
	r := rewardRoster{division: division, actors: make(map[uint32]rewardActor)}
	parties := []RewardParty(nil)
	if rt.RewardParties != nil {
		parties = rt.RewardParties(division)
	}
	memberParty := make(map[uint32]*RewardParty)
	for i := range parties {
		for _, gid := range parties[i].Members {
			memberParty[gid] = &parties[i]
		}
	}
	// The division list is taken before the read door: CharactersForDivision
	// takes the store's read lock itself, and a second read lock inside the
	// door waits behind any queued writer while the door holds that writer
	// off. The slice is a fresh copy of the live pointers, so
	// the field reads below stay inside the door.
	characters := rt.deps.CharactersForDivision(division)
	rt.deps.Read(division, func() {
		for _, c := range characters {
			if c == nil || c.DeletePending {
				continue
			}
			if rt.RewardActorPresent != nil && !rt.RewardActorPresent(division, c.Name) {
				continue
			}
			gid := enterworld.ObjectIDForCharacter(c)
			r.actors[gid] = rewardActor{world: domain.CharacterWorldInstance(c), character: c, pose: rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now), party: memberParty[gid]}
			r.characters = append(r.characters, c)
		}
	})
	return r
}

/*
================
monsterRewardGroup
================
*/
type monsterRewardGroup struct {
	order                                        uint64
	party                                        *RewardParty
	damage, representative, representativeDamage uint32
}

/*
================
monsterRewardGroups

Group experience-sharing parties while retaining the strongest individual
contributor as their loot representative.
================
*/
func monsterRewardGroups(rows []simulation.MonsterContribution, actors map[uint32]rewardActor) []monsterRewardGroup {
	// The reference server visits the source map by unsigned GID. Equal representative
	// contributions keep the first source, even if input snapshots are shuffled.
	rows = append([]simulation.MonsterContribution(nil), rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreditGID < rows[j].CreditGID })
	byOrder := make(map[uint64]*monsterRewardGroup)
	for _, row := range rows {
		a, ok := actors[row.CreditGID]
		if !ok || row.Damage == 0 {
			continue
		}
		order := uint64(reflect.ValueOf(a.character).Pointer())
		var party *RewardParty
		if a.party != nil && a.party.Options&1 != 0 {
			party = a.party
			order = party.Order
		}
		g := byOrder[order]
		if g == nil {
			g = &monsterRewardGroup{order: order, party: party}
			byOrder[order] = g
		}
		g.damage += row.Damage
		if row.Damage > g.representativeDamage {
			g.representative = row.CreditGID
			g.representativeDamage = row.Damage
		}
	}
	var out []monsterRewardGroup
	for _, g := range byOrder {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].order < out[j].order })
	return out
}

/*
================
withinPartyRewardRange

The reference server measures planar distance with Y zero. Dungeon deltas use local X/Z.
================
*/
func withinPartyRewardRange(a, b simulation.Spawn) bool {
	if !worldgeom.SamePlane(a.RegionID, b.RegionID) {
		return false
	}
	dx, dz := float64(float32(b.X))-float64(float32(a.X)), float64(float32(b.Z))-float64(float32(a.Z))
	if simulation.PlaneKindOf(a.RegionID) == simulation.PlaneOutdoor {
		dx += float64(worldgeom.SectorX(b.RegionID)-worldgeom.SectorX(a.RegionID)) * 1920
		dz += float64(worldgeom.SectorY(b.RegionID)-worldgeom.SectorY(a.RegionID)) * 1920
	}
	x, z := float32(dx), float32(dz)
	squared := float32(float64(x)*float64(x) + float64(z)*float64(z))
	return math.Sqrt(float64(squared)) <= 1000
}

/*
================
isPartyMonster

The reference server's party-monster test: the rarity byte's high nibble is exactly
1. The level-difference scale then halves EXP (0.5f)
when the monster's level is below the receiver's (or the party average).
================
*/
func isPartyMonster(instance monster.Instance) bool {
	return instance.Rarity()&0xF0 == 0x10
}

/*
================
rewardLevel
================
*/
func rewardLevel(c *enterworld.Character) int64 {
	if c.Level == nil || *c.Level < 1 {
		return 1
	}
	return *c.Level
}

/*
================
partyRewardFactors

The reference server spills bonus and level shares independently to float32.

evenFloor is a deliberate beta deviation (the growth switch turns it on):
the native party kill EXP distribution weights each share by level alone,
so a level 1 beside a level 23 took 1/24 of its own kill EXP and party
power-levelling fell ~13x behind soloing. With the floor no member's share
drops below an even 1/N split; higher levels keep their larger share.
================
*/
func partyRewardFactors(members []rewardActor, target monster.Instance, evenFloor bool) []float32 {
	var chinese, other, sum, maxLevel int64
	for _, a := range members {
		l := rewardLevel(a.character)
		sum += l
		maxLevel = max(maxLevel, l)
		if enterworld.NativeCountryByte9C(a.character) == 0 {
			chinese++
		} else {
			other++
		}
	}
	if sum == 0 {
		return nil
	}
	bonus := float32(1)
	if other > 0 {
		bonus = float32(1 + float64(other-1)*float64(float32(.2)) + float64(float32(float64(chinese)*float64(float32(.1)))))
	} else if chinese > 0 {
		bonus = float32(1 + float64(chinese-1)*float64(float32(.1)))
	}
	penalty := float32(1)
	if isPartyMonster(target) && int64(target.Ref.Level) < sum/int64(len(members)) {
		penalty = .5
	}
	var out []float32
	for _, a := range members {
		share := float32(float64(rewardLevel(a.character)) / float64(sum))
		if even := float32(1 / float64(len(members))); evenFloor && share < even {
			share = even
		}
		b := bonus
		if float64(share) > .7 && len(members) >= 3 && maxLevel >= 21 {
			b = float32(1 + (float64(bonus)-1)*.5)
		}
		out = append(out, float32(float64(share)*float64(b)*float64(penalty)))
	}
	return out
}

/*
================
RecipientFrames

Private rewards remain addressed to their owner through publication.
================
*/
type RecipientFrames struct {
	CharacterID int64
	Frames      []wire.Frame
}

/*
================
recipientDivisionFrames
================
*/
func recipientDivisionFrames(division string, recipients []RecipientFrames) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, recipient := range recipients {
		batch := simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: recipient.CharacterID}
		for _, frame := range recipient.Frames {
			batch.Frames = append(batch.Frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope})
		}
		out = append(out, batch)
	}
	return out
}

/*
================
monsterSettlement
================
*/
type monsterSettlement struct {
	actorFrames, public []wire.Frame
	otherPublic         []wire.Frame
	others              []RecipientFrames
	drops               []grounditem.Item
}

/*
================
easePenaltyForMonsterKill

The original eases a murder penalty by the level gap for the kill's reward
representative (the top contributor of the top-damage group, the player
its death path picks for the drops), not for the player whose blow landed
last. Frames for a representative who is not the actor go to its own
session.
================
*/
func (rt *Runtime) easePenaltyForMonsterKill(division string, actor, killer *enterworld.Character, target monster.Instance, out *monsterSettlement) {
	if killer == nil || killer.PK == nil || killer.PK.Penalty == 0 {
		return
	}
	before := killer.PVPState()
	relief := pk.MonsterKillRelief(target.Ref.Level, uint8(min(rewardLevel(killer), 0xff)))
	frames := pkRecordFrames(killer, pk.AddPenalty(killer, relief, rt.Now()))
	rt.notePKRecord(division, killer)
	if killer == actor {
		out.actorFrames = append(out.actorFrames, frames...)
	} else if len(frames) > 0 {
		out.others = append(out.others, RecipientFrames{killer.ID, frames})
	}
	if before != killer.PVPState() {
		out.public = append(out.public, playerPVPStateFrame(killer))
	}
}

/*
================
settleMonsterInsideDoor

Run inside the damage/cost UpdateMany. Native pays every group, then returns
the representative of the greatest unsigned damage group for loot ownership.
================
*/
func (rt *Runtime) settleMonsterInsideDoor(division string, actor *enterworld.Character, roster rewardRoster, impact simulation.MonsterDamageResult, pose monster.Pose, now int64) monsterSettlement {
	return rt.settleMonsterInsideDoorCounted(division, actor, roster, impact, pose, now, true)
}

/*
================
settleMonsterInsideDoorCounted

settleMonsterInsideDoor with the weekly seal board's counting switch
(sealboard.go): the GM kill path settles with it off, so a GM-made drop
never reaches the board.
================
*/
func (rt *Runtime) settleMonsterInsideDoorCounted(division string, actor *enterworld.Character, roster rewardRoster, impact simulation.MonsterDamageResult, pose monster.Pose, now int64, countSeals bool) monsterSettlement {
	var out monsterSettlement
	if !impact.Fatal || impact.Instance.Ref.TrainingDummy {
		return out
	}
	// One snapshot of the live tunables for the whole kill (thiefparty.go).
	settings := tunables.Current()
	for _, award := range rt.grantBerserkForKill(actor, roster, impact) {
		if award.CharacterID == actor.ID {
			out.actorFrames = append(out.actorFrames, award.Frames...)
		} else {
			out.others = append(out.others, award)
		}
	}
	// The reference server's PvP kill rewards: the killing blow's job wearer earns job EXP for a
	// thief or hunter monster (pkreward.go). Only a player's own blow reaches
	// it in the original; a pet's or COS's fatal hit pays no job EXP.
	if actor != nil && !roster.companionKill {
		jobFrames, others := rt.payMonsterJobKillInDoor(division, actor, impact.Instance, pose, now)
		out.actorFrames = append(out.actorFrames, jobFrames...)
		out.others = append(out.others, others...)
	}
	// Port-only, not native (uniquedamagerank.go): every ranked contributor
	// sees the unique's damage ranking, whoever owns the loot.
	rankFrames, rankOthers := rt.uniqueDamageRankFrames(actor, roster, impact)
	out.actorFrames = append(out.actorFrames, rankFrames...)
	out.others = append(out.others, rankOthers...)
	groups := monsterRewardGroups(impact.Contributions, roster.actors)
	var winner uint32
	var best uint32
	for _, g := range groups {
		if g.damage > best {
			best = g.damage
			winner = g.representative
		}
	}
	if a, ok := roster.actors[winner]; ok {
		rt.easePenaltyForMonsterKill(division, actor, a.character, impact.Instance, &out)
	}
	origin := simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
	originWorld := uint32(impact.Population.ID)
	if a, ok := roster.actors[winner]; ok {
		origin = a.pose
		originWorld = a.world
	}
	var count uint16
	for _, g := range groups {
		var members []rewardActor
		var factors []float32
		if g.party == nil {
			a := roster.actors[g.representative]
			members = []rewardActor{a}
			factor := float32(1)
			if a.party != nil {
				factor = float32(1 + float64(len(a.party.Members)-1)*float64(float32(.03)))
			}
			if isPartyMonster(impact.Instance) && int64(impact.Instance.Ref.Level) < rewardLevel(a.character) {
				factor *= .5
			}
			factors = []float32{factor}
		} else {
			for _, gid := range g.party.Members {
				if a, ok := roster.actors[gid]; ok && a.world == originWorld && enterworld.CharacterAlive(a.character) && withinPartyRewardRange(origin, a.pose) {
					members = append(members, a)
				}
			}
			count = uint16(len(members)) // each party call overwrites, even a non-winning group
			factors = partyRewardFactors(members, impact.Instance, rt.PartyShareFloor)
		}
		for i, a := range members {
			exp, sexp := monsterContributionReward(a.character, impact.Instance, rt.deps.LevelData(), g.damage, factors[i], a.party != nil && a.party.Options&1 != 0)
			exp, sexp = paramJobRewardBonus(a.character, exp, sexp, now)
			// Port-only, not native (thiefparty.go): the member's party's thieves.
			exp = thiefPartyExp(exp, rt.thiefPartyPercent(division, a.party, settings))
			var frames []wire.Frame
			if rt.UpdateExperience != nil && (exp != 0 || sexp != 0) {
				frames, _ = rt.UpdateExperience(a.character, exp, sexp, impact.Instance.Gid)
			}
			// Port-only, not native (killstats.go): the award counts in
			// the recipient's band whether or not a sink is wired.
			rt.countKillStatAward(a.character, exp, sexp, now)
			out.public = append(out.public, wire.ProgressionBroadcastFrames(frames)...)
			if a.character != actor {
				out.otherPublic = append(out.otherPublic, wire.ProgressionBroadcastFrames(frames)...)
			}
			if a.character == actor {
				out.actorFrames = append(out.actorFrames, frames...)
			} else if private := wire.ProgressionPrivateFrames(frames); len(private) > 0 {
				out.others = append(out.others, RecipientFrames{a.character.ID, private})
			}
			// Reference server: the recipient's attack pets share the award even when
			// the owner's own EXP came to nothing.
			petFrames, petArea := rt.awardAttackPetExperience(a.character, impact.Instance, g.damage, factors[i], now)
			out.public = append(out.public, petArea...)
			if a.character == actor {
				out.actorFrames = append(out.actorFrames, petFrames...)
			} else if len(petFrames) > 0 {
				out.others = append(out.others, RecipientFrames{a.character.ID, petFrames})
			}
		}
	}
	if a, ok := roster.actors[winner]; ok {
		// Loot generation's player-level admission and ownership both use the
		// selected representative, never the actor who supplied the fatal hit.
		planned := rt.planMonsterKillLoot(a.character, impact.Instance, pose, now)
		rt.applyMonsterGoldBonus(division, a.character, planned)
		rt.applyThiefPartyGold(division, a.character, planned, settings)
		frames, drops, _ := rt.applyMonsterKillInsideDoor(division, a.character, 0, 0, impact.Instance.Gid, planned)
		out.drops = drops
		// The weekly seal board (sealboard.go) counts the committed loot
		// here, before quest-supply drops join out.drops below: those
		// never count. A GM-spawned monster's loot is conjured, not
		// hunted, so it never counts either, however it died.
		if countSeals && !impact.Instance.GMSpawned {
			rt.countSealDrops(a.character, drops, now)
		}
		if a.character == actor {
			out.actorFrames = append(out.actorFrames, frames...)
		} else if len(frames) > 0 {
			out.others = append(out.others, RecipientFrames{a.character.ID, frames})
		}
		if ref, announced := rt.Monsters.RecordUniqueKiller(division, impact.Instance.Gid, a.character.Name); announced && rt.RecordUniqueKill != nil {
			rt.RecordUniqueKill(division, domain.UniqueKill{AtMs: now, RefObjID: ref, KillerCharID: a.character.ID, KillerName: a.character.Name})
		}
	}
	if !roster.companionKill && actor != nil {
		frames, drops := rt.creditQuestKillInsideDoor(division, actor, impact.Instance, pose, now)
		out.actorFrames = append(out.actorFrames, frames...)
		out.drops = append(out.drops, drops...)
	}
	// Port-only, not native (killstats.go): the kill counts once, in
	// the fatal actor's band - or the winning representative's when a
	// companion landed the blow (actor is nil there).
	killer := actor
	if killer == nil {
		if a, ok := roster.actors[winner]; ok {
			killer = a.character
		}
	}
	rt.countKillStatKill(killer, now)
	rt.Monsters.ArmNestFromReward(division, impact.Instance.Gid, count)
	return out
}
