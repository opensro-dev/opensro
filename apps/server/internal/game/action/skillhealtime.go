/*
===========================================================================

skillhealtime.go - heals over time: Mana Cycle, Mana Orbit and the Cleric's
Healing Cycle and Healing Orbit

A timed heal (enterworld SkillRecovery.HealOverTimePinned) installs the
row's effect on each recipient, which is the buff icon the recipient and
its observers see, and this owner heals that recipient every puls for as
long as the effect lives. The support owner (skillrecovery.go) admits,
charges and selects; this file only installs and pulses.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
healOverTime

One installed timed heal. token is the recipient's effect instance: the
heal pulses exactly as long as that instance lives. caster is the caster's
snapshot at release, whose weapon (mwmh / mwhh) and HLRU every pulse reads.
==================
*/
type healOverTime struct {
	division      string
	recipientName string
	skillID       uint32
	token         uint32
	caster        *enterworld.Character
	startedMs     int64
	lastPulseMs   int64
	durationMs    int64
	periodMs      int64
}

/*
==================
installHealsOverTime

Install the row's effect on every recipient and open its heal. Each
recipient is installed inside its own door, after the caster's charge;
one whose door refuses (gone, dead) or whose buffs refuse the replacement
is skipped, as the party heal skips it. The effect frames are public.

Inferred: the caster's own instance is a self application (context mode
1), anyone else's a recipient one (mode 2), as the timed self and targeted
effects install them; a recast on a recipient replaces its running heal
through the row's replacement rules (auraReplacementAllowed), whose
stopped instance stops pulsing.
==================
*/
func (rt *Runtime) installHealsOverTime(division string, caster *enterworld.Character, skill enterworld.SkillRow, recipients []*enterworld.Character, now int64) []wire.Frame {
	casterView := rt.characterSnapshot(division, caster)
	if casterView == nil {
		return nil
	}
	var public []wire.Frame
	for _, who := range recipients {
		if who == nil || !rt.auraReplacementAllowed(division, who, skill) {
			continue
		}
		presentation := EffectPresentation{Phase: 2}
		if who.ID == caster.ID {
			presentation.Phase = 1
		}
		token := atomic.AddUint32(&rt.castTokenCounter, 1)
		var installed []wire.Frame
		if !rt.deps.Update(who, "heal-over-time-install", func() bool {
			if who.DeletePending || !enterworld.CharacterAlive(who) {
				return false
			}
			var ok bool
			installed, ok = rt.commitCharacterEffect(division, who, skill, token, statuseffect.StateActive, false, presentation, now)
			return ok
		}) {
			continue
		}
		public = append(public, installed...)

		heal := healOverTime{
			division:      division,
			recipientName: who.Name,
			skillID:       skill.ID,
			token:         token,
			caster:        casterView,
			startedMs:     now,
			lastPulseMs:   now,
			durationMs:    int64(skill.EffectDurationMs),
			periodMs:      int64(skill.Recovery.PulseMs),
		}
		rt.healOverTimeMu.Lock()
		rt.healsOverTime = append(rt.healsOverTime, heal)
		rt.healOverTimeMu.Unlock()
	}
	return public
}

/*
==================
advanceHealsOverTime

The pulse rule of 5830B0's timed context (linkedpulse): the first pulse is
one full period after release, a pulse is due once the period has elapsed
since the last one and resets that clock to now, and the context ends once
more than dura has elapsed. A dura of 16000 and puls of 2000 therefore
heal eight times, the last at 16000.

Each pulse gives the whole heal block, the rule the eshp aura already
applies on every update of Recovery Division (skillparty.go healAura):
puls spaces the heals, it does not split the amount. Inferred: the
recipient's healing received (rhru, applyHealReceived) and its 0xAA / 0xAB
scale are read on every pulse, through skillHealAmounts, which applies
rhru once to the pulse's final amounts inside the recipient's door before
applySkillRecovery.

A heal whose effect instance stopped (cancelled, replaced, expired, the
recipient gone) is dropped without a pulse.
==================
*/
func (rt *Runtime) advanceHealsOverTime(now int64) []simulation.DivisionFrames {
	rt.healOverTimeMu.Lock()
	defer rt.healOverTimeMu.Unlock()

	var out []simulation.DivisionFrames
	kept := rt.healsOverTime[:0]
	for _, heal := range rt.healsOverTime {
		if now-heal.startedMs > heal.durationMs || !rt.instanceLive(heal.division, heal.recipientName, heal.skillID, heal.token) {
			continue
		}
		if now-heal.lastPulseMs >= heal.periodMs {
			heal.lastPulseMs = now
			out = append(out, rt.pulseHealOverTime(heal)...)
		}
		kept = append(kept, heal)
	}
	clear(rt.healsOverTime[len(kept):])
	rt.healsOverTime = kept
	return out
}

/*
==================
pulseHealOverTime

One pulse: the 5A0850 amounts for the recipient, committed inside its door
like a cast heal; the recipient alone receives its 0x33A6. A dead recipient
is left untouched by applySkillRecovery.
==================
*/
func (rt *Runtime) pulseHealOverTime(heal healOverTime) []simulation.DivisionFrames {
	who := rt.findCharacter(heal.division, heal.recipientName)
	skill, known := rt.deps.SkillData().SkillByID(heal.skillID)
	if who == nil || !known {
		return nil
	}

	var frame wire.Frame
	healed := rt.deps.Update(who, "heal-over-time-pulse", func() bool {
		hp, mp, ok := rt.skillHealAmounts(heal.division, who, heal.caster, skill, healCast)
		if !ok {
			return false
		}
		frame, ok = rt.applySkillRecovery(heal.division, who, hp, mp)
		return ok
	})
	if !healed || frame.Opcode == 0 {
		return nil
	}
	return []simulation.DivisionFrames{{DivisionID: heal.division, OnlyCharacterID: who.ID, Frames: simFrames([]wire.Frame{frame})}}
}
