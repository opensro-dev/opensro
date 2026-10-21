/*
===========================================================================
petwolfstats.go - port-only Bloodscent attack stats shared by combat and UI.

Store a detached projection so keeper reads inside a character transaction do
not re-enter that transaction. The division tick expires/withdraws the grant.
===========================================================================
*/
package action

import (
	"strings"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
wolfBloodProjection
================
*/
type wolfBloodProjection struct {
	book    uint8
	percent float64
	crit    float64
	until   int64
}

/*
================
refreshWolfBlood

Only changed numeric stats need a new private character-sheet frame; merely
refreshing the six-second lease does not flood the owner with duplicate stats.
================
*/
func (rt *Runtime) refreshWolfBlood(step petCombatStep, eligible bool) {
	key := step.key
	key.name = strings.ToLower(key.name)
	next := wolfBloodProjection{}
	if eligible {
		s, _, ok := companion.Passive(step.pet, "Bloodscent", companion.ModifiersFor(step.pet))
		if ok && s.Option >= 2 {
			next.percent, next.crit = wolfDamageBonus(step, 1)
			if next.percent > 0 {
				next.book, next.until = s.ID, step.state.wolf.bloodUntil[1]
			}
		}
	}
	rt.wolfHowls.Lock()
	old := rt.wolfHowls.blood[key]
	if next.percent == 0 {
		delete(rt.wolfHowls.blood, key)
	} else {
		if rt.wolfHowls.blood == nil {
			rt.wolfHowls.blood = make(map[petOwnerKey]wolfBloodProjection)
		}
		rt.wolfHowls.blood[key] = next
	}
	rt.wolfHowls.Unlock()
	if next.percent != old.percent || next.crit != old.crit {
		frames := rt.gaugeDropFrames(key.division, step.state.character, false, false, true)
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: key.division, OnlyCharacterID: step.state.character.ID, Frames: simFrames(frames)}})
	}
}

/*
================
wolfBloodWrites

Same attack min/max parameters feed 0x343C and the authoritative formula.
Eligibility is checked on the supplied snapshot without another roster read.
================
*/
func (rt *Runtime) wolfBloodWrites(division string, c *enterworld.Character) []paramkeeper.Write {
	if !rt.AttackPetExpansion || c == nil || !enterworld.CharacterAlive(c) || rt.inTown(division, c) {
		return nil
	}
	now := rt.Now().UnixMilli()
	var grant wolfBloodProjection
	rt.wolfHowls.Lock()
	for _, pet := range c.Companions() {
		if !pet.Summoned || pet.CurrentHP == 0 || !rt.petExpansionAllowed(c, pet) {
			continue
		}
		row := rt.wolfHowls.blood[petOwnerKey{division: division, name: strings.ToLower(c.Name), gid: pet.GID}]
		skill, _, ok := companion.Passive(pet, "Bloodscent", companion.ModifiersFor(pet))
		if ok && skill.ID == row.book && row.until > now && row.percent > grant.percent {
			grant = row
		}
	}
	rt.wolfHowls.Unlock()
	if grant.percent <= 0 {
		return nil
	}
	var writes []paramkeeper.Write
	for id := uint16(13); id <= 16; id++ {
		writes = append(writes, paramkeeper.Write{Parameter: id, Channel: paramkeeper.PercentProduct, Source: 0x7ffffc00, Value: float32(grant.percent)})
	}
	if grant.crit > 0 {
		writes = append(writes, paramkeeper.Write{Parameter: 12, Channel: paramkeeper.Flat, Source: 0x7ffffc00, Value: float32(grant.crit)})
	}
	return writes
}
