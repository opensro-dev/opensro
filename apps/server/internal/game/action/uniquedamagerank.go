/*
===========================================================================

uniquedamagerank.go - the damage ranking shown when a unique dies

Port-only, not native, and off by default. The original announces
only a unique's killer (0x3058 kind 6). With SRO_UNIQUE_DAMAGE_RANK on,
every player who damaged the unique also receives the port-only 0x3FBC
frame (wire/uniquedamagerank.go): the twelve players who did the most
damage, each row's party place, and the recipient's own place. Off, the
default here, is native: no frame is built.

The damage is the monster's native contribution table (the damage argument
of every credited hit, pets credited to their owner), read from the fatal
impact's snapshot, so the ranking agrees with the EXP and loot split.

===========================================================================
*/
package action

import (
	"os"
	"sort"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvUniqueDamageRank turns the ranking on ("on", "1", "true").
const EnvUniqueDamageRank = "SRO_UNIQUE_DAMAGE_RANK"

// uniqueRarityGrade is the rarity nibble of a unique, the grade the native
// kill notice announces (RecordUniqueKiller). Grade 8 rolls unique drops but
// is not announced, so it is not ranked either.
const uniqueRarityGrade = 3

/*
================
UniqueDamageRankFromEnv
================
*/
func UniqueDamageRankFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvUniqueDamageRank))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
uniqueDamagePlace

One ranked player: the roster's character, the damage it was credited and
its party's place (0 without a party).
================
*/
type uniqueDamagePlace struct {
	gid       uint32
	character *enterworld.Character
	damage    uint32
	partyRank uint8
}

/*
================
uniqueDamageRanking

Every ranked player in place order, and the sum of all contributions.

Only contributors in the roster are ranked. A contributor absent from it
has left the division: it cannot receive the frame, and its name cannot be
read inside the damage transaction (monsterRewardRoster takes the
division list before the door for lock order). Inferred, as the original
has no such ranking: it still counts toward the total, so every
percentage is a share of the damage the unique really took.
================
*/
func uniqueDamageRanking(rows []simulation.MonsterContribution, actors map[uint32]rewardActor) ([]uniqueDamagePlace, uint64) {
	var total uint64
	var places []uniqueDamagePlace
	partyDamage := make(map[uint64]uint64)
	for _, row := range rows {
		total += uint64(row.Damage)
		a, ok := actors[row.CreditGID]
		if !ok {
			continue
		}
		places = append(places, uniqueDamagePlace{gid: row.CreditGID, character: a.character, damage: row.Damage})
		if a.party != nil {
			partyDamage[a.party.Order] += uint64(row.Damage)
		}
	}
	// Equal damage keeps the lower GID first, so every recipient sees one
	// order whatever the snapshot's order.
	sort.Slice(places, func(i, j int) bool {
		if places[i].damage != places[j].damage {
			return places[i].damage > places[j].damage
		}
		return places[i].gid < places[j].gid
	})
	parties := make([]uint64, 0, len(partyDamage))
	for order := range partyDamage {
		parties = append(parties, order)
	}
	sort.Slice(parties, func(i, j int) bool {
		if partyDamage[parties[i]] != partyDamage[parties[j]] {
			return partyDamage[parties[i]] > partyDamage[parties[j]]
		}
		return parties[i] < parties[j]
	})
	partyRank := make(map[uint64]uint8, len(parties))
	for i, order := range parties {
		partyRank[order] = uint8(min(i+1, 0xff))
	}
	for i := range places {
		if p := actors[places[i].gid].party; p != nil {
			places[i].partyRank = partyRank[p.Order]
		}
	}
	return places, total
}

/*
================
uniqueDamageRankFrames

One 0x3FBC per ranked player, each carrying that player's own place. The
fatal actor's frame returns separately because the actor's frames travel
on the request's reply.
================
*/
func (rt *Runtime) uniqueDamageRankFrames(actor *enterworld.Character, roster rewardRoster, impact simulation.MonsterDamageResult) ([]wire.Frame, []RecipientFrames) {
	if !rt.UniqueDamageRank || !impact.Fatal || impact.Instance.Rarity()&15 != uniqueRarityGrade {
		return nil, nil
	}
	places, total := uniqueDamageRanking(impact.Contributions, roster.actors)
	if len(places) == 0 {
		return nil, nil
	}
	top := make([]wire.UniqueDamageRow, 0, min(len(places), wire.UniqueDamageRankMaxRows))
	for _, p := range places[:min(len(places), wire.UniqueDamageRankMaxRows)] {
		top = append(top, wire.UniqueDamageRow{Name: p.character.Name, Damage: p.damage, PartyRank: p.partyRank})
	}
	var actorFrames []wire.Frame
	var others []RecipientFrames
	for i, p := range places {
		frame := wire.Frame{Opcode: wire.OpUniqueDamageRank, Payload: wire.EncodeUniqueDamageRank(wire.UniqueDamageRank{
			RefObjID:      impact.Instance.Ref.RefObjID,
			Total:         total,
			Rows:          top,
			SelfRank:      uint16(min(i+1, 0xffff)),
			SelfDamage:    p.damage,
			SelfPartyRank: p.partyRank,
		})}
		if p.character == actor {
			actorFrames = append(actorFrames, frame)
		} else {
			others = append(others, RecipientFrames{CharacterID: p.character.ID, Frames: []wire.Frame{frame}})
		}
	}
	return actorFrames, others
}
