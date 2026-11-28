/*
===========================================================================

critical.go - actor-owned critical and block history for every impact

===========================================================================
*/

package action

import (
	"strings"
	"sync"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// ignoreDefenseProbabilityPrefix is the reference server's probability-history
// key namespace for MATTR_AVATAR_MDIA (0x4D000000 | (skillID & 0xFFFFFF)).
const ignoreDefenseProbabilityPrefix uint32 = 0x4D000000

/*
================
criticalActor
================
*/
type criticalActor struct {
	division, character string
	monster             uint32
}

/*
================
criticalHistory
================
*/
type criticalHistory struct {
	mu     sync.Mutex
	actors map[criticalActor]map[uint32]combat.Probability
}

// Action owns native ParamKeeper's probability history. All production attack
// paths use this door; formula helpers alone intentionally have no actor state.
// Targets do not enter the key: changing victims must not reset critical odds.
/*
================
resolveCombat
================
*/
func (rt *Runtime) resolveCombat(actor criticalActor, skill enterworld.SkillRow, attacker, defender combat.Stats) (combat.Result, error) {
	out, err := rt.resolveCombatBehindWall(actor, skill, attacker, defender, nil)
	return out.Defender, err
}

// resolveCombatBehindWall splits the hit when the defender stands behind a
// Force wall; the one critical decision serves both records.
/*
================
resolveCombatBehindWall
================
*/
func (rt *Runtime) resolveCombatBehindWall(actor criticalActor, skill enterworld.SkillRow, attacker, defender combat.Stats, wall *enterworld.SkillWall) (combat.WallOutcome, error) {
	return rt.resolveCombatRequest(combatRequest{
		actor: actor, skill: skill, attacker: attacker, defender: defender,
		wall: wall, lanes: skill.Attack.Flags & 0xc,
	})
}

/*
================
combatRequest

Lane selection is impact state, never a mutation of the authored att flags.
================
*/
type combatRequest struct {
	actor    criticalActor
	skill    enterworld.SkillRow
	attacker combat.Stats
	defender combat.Stats
	wall     *enterworld.SkillWall
	lanes    uint32
}

/*
================
resolveCombatRequest
================
*/
func (rt *Runtime) resolveCombatRequest(request combatRequest) (combat.WallOutcome, error) {
	actor, skill := request.actor, request.skill
	attacker, defender, wall := request.attacker, request.defender, request.wall
	rt.criticals.mu.Lock()
	defer rt.criticals.mu.Unlock()
	actor.character = strings.ToLower(actor.character)
	key := skill.ID & 0xffffff // key is 0x43000000 | skill ID.
	previous := rt.criticals.actors[actor][key]
	// A player in a monster's shape (transform mode 1) is the hit-outcome
	// routine's own hit group: no critical is rolled for it and its damage
	// never touches the lanes (the transformed branch below). The
	// classification reads the monster's shape before the weapon, so a
	// shaped player is never in the fortress group either.
	transformed := false
	if actor.monster == 0 && actor.character != "" {
		if shaped := rt.findCharacter(actor.division, actor.character); shaped != nil && shaped.TransformMode == 1 {
			transformed = true
		}
	}
	// A player swinging a fortress weapon (worn weapon kind TID4 16) is
	// the hit-outcome routine's fortress hit group: no critical is
	// rolled for it and its damage never touches the lanes (the
	// fortress branch below).
	fortressArmed := false
	if !transformed && actor.monster == 0 && actor.character != "" {
		fortressArmed = rt.fortressArmed(actor.division, actor.character)
	}
	// A hit on a fortress structure (the defender's ref marks it) is
	// the hit-outcome routine's target-side hit group: no critical is
	// rolled for it and its damage never touches the lanes (the
	// structure branch below). The classification reads the target
	// before the weapon, so a fortress weapon against a structure is
	// in this group, never in the fortress group.
	structureTarget := actor.monster == 0 && actor.character != "" && defender.Structure
	critical, next := false, previous
	if request.lanes&4 != 0 && !transformed && !fortressArmed && !structureTarget {
		rate, err := combat.EffectiveCriticalRate(attacker.CriticalRate, skill.CriticalModifier)
		if err != nil {
			return combat.WallOutcome{}, err
		}
		// The defender's evade critical (0x39) cuts the chance.
		rate = combat.EvadedCriticalRate(rate, defender)
		critical, next, err = combat.CriticalOutcome(float64(rate), previous, rt.CombatRoll)
		if err != nil {
			return combat.WallOutcome{}, err
		}
	}
	// In the reference server's hit-outcome routine, MATTR_AVATAR_MDIA rolls
	// after critical and before the damage lanes so the random stream order matches.
	ignoreDefense := false
	if actor.monster == 0 && !transformed && !fortressArmed && !structureTarget {
		if chance := combat.IgnoreDefenseChance(attacker, defender); chance > 0 {
			mdiaKey := ignoreDefenseProbabilityPrefix | skill.ID&0xffffff
			procced, mdiaNext, err := combat.CriticalOutcome(chance, rt.criticals.actors[actor][mdiaKey], rt.CombatRoll)
			if err != nil {
				return combat.WallOutcome{}, err
			}
			rt.rememberProbability(actor, mdiaKey, mdiaNext)
			ignoreDefense = procced
		}
	}
	// rollBlock rolls the target's block chance on the attacker's history
	// (key 0x42000000 | skill); the caller decides what a blocked record
	// drops.
	rollBlock := func() (bool, error) {
		chance := combat.BlockChance(attacker, defender, skill.Attack.Flags, skill.Ck)
		blockKey := 0x42000000 | skill.ID&0xffffff
		blocked, blockNext, err := combat.CriticalOutcome(float64(chance), rt.criticals.actors[actor][blockKey], rt.CombatRoll)
		if err != nil {
			return false, err
		}
		rt.rememberProbability(actor, blockKey, blockNext)
		return blocked, nil
	}

	var out combat.WallOutcome
	var err error
	if transformed {
		// The block roll comes first, as for any hit; a blocked
		// monster-shaped hit is a bare type-2 record and consumes no
		// damage draw. The hit has no lanes, so a standing wall never
		// splits it and its lane accumulators stay zero.
		blocked := false
		if skill.Attack.Present {
			var blockErr error
			blocked, blockErr = rollBlock()
			if blockErr != nil {
				return combat.WallOutcome{}, blockErr
			}
		}
		if blocked {
			return combat.WallOutcome{Defender: combat.Result{ResultFlags: 1, Blocked: true}}, nil
		}
		damage, damageErr := combat.LevelScaledDamage(attacker.Level, rt.CombatRoll)
		if damageErr != nil {
			return combat.WallOutcome{}, damageErr
		}
		return combat.WallOutcome{Defender: combat.Result{Damage: damage, ResultFlags: 1}}, nil
	}
	if structureTarget {
		// The block roll comes first, as for any hit; a blocked
		// structure hit is a bare type-2 record and consumes no
		// damage draw. An unblocked hit is the target-side group's
		// amount: the attacker's attack-power draw scaled by the
		// worn weapon's kind factor (the bare draw for a fortress
		// weapon). The hit has no lanes, so a standing wall never
		// splits it and its lane accumulators stay zero.
		blocked := false
		if skill.Attack.Present {
			var blockErr error
			blocked, blockErr = rollBlock()
			if blockErr != nil {
				return combat.WallOutcome{}, blockErr
			}
		}
		if blocked {
			return combat.WallOutcome{Defender: combat.Result{ResultFlags: 1, Blocked: true}}, nil
		}
		damage, damageErr := combat.StructureHitDamage(rt.weaponKind(actor.division, actor.character), attacker, rt.CombatRoll)
		if damageErr != nil {
			return combat.WallOutcome{}, damageErr
		}
		return combat.WallOutcome{Defender: combat.Result{Damage: damage, ResultFlags: 1}}, nil
	}
	if fortressArmed {
		// The block roll comes first, as for any hit; a blocked
		// fortress hit is a bare type-2 record with no damage. An
		// unblocked hit deals exactly 1: the group never reaches the
		// lane formula. (A fortress-class target never reaches this
		// branch: the target-side group above classifies it first.)
		// The hit has no lanes, so a standing wall never splits it
		// and its lane accumulators stay zero.
		blocked := false
		if skill.Attack.Present {
			var blockErr error
			blocked, blockErr = rollBlock()
			if blockErr != nil {
				return combat.WallOutcome{}, blockErr
			}
		}
		if blocked {
			return combat.WallOutcome{Defender: combat.Result{ResultFlags: 1, Blocked: true}}, nil
		}
		return combat.WallOutcome{Defender: combat.Result{Damage: 1, ResultFlags: 1}}, nil
	}
	calculation := combat.AttackCalculation{
		Attack: skill.Attack, OriginalFlags: skill.Attack.Flags, Lanes: request.lanes,
		Player: actor.monster == 0, Critical: critical, IgnoreDefense: ignoreDefense,
	}
	switch {
	case !skill.Attack.Present && skill.RatioDamage.Present:
		// A program with no attack block lands its ratio record alone:
		// the lanes would only refuse it, and no wall takes a share of
		// it (the row's bypass tag). The fortress towers' only hit is
		// this shape.
		out.Defender = combat.Result{Damage: combat.HPRatioDamage(skill.RatioDamage.Percent, defender.MaxHP), ResultFlags: 1}
	case wall != nil:
		out, err = combat.ResolveCalculationAgainstWall(attacker, defender, combat.WallCalculation{Attack: calculation, Wall: *wall}, rt.CombatRoll)
	default:
		out.Defender, err = combat.ResolveCalculation(attacker, defender, calculation, rt.CombatRoll)
	}
	if err != nil {
		return combat.WallOutcome{}, err
	}
	// A program with both an attack block and the ratio tag adds the
	// ratio amount to the lanes' total, after the wall's split (the
	// tag bypasses the absorb) and under the same ceiling.
	if skill.Attack.Present && skill.RatioDamage.Present {
		out.Defender.Damage = min(out.Defender.Damage+combat.HPRatioDamage(skill.RatioDamage.Percent, defender.MaxHP), wire.MaxSkillActionDamage)
	}
	// After the wall's share, every impact of an attack
	// skill rolls the target's block chance on the attacker's history
	// (key 0x42000000 | skill); a block drops the defender's damage.
	if skill.Attack.Present {
		blocked, blockErr := rollBlock()
		if blockErr != nil {
			return combat.WallOutcome{}, blockErr
		}
		if blocked {
			out.Defender = combat.Result{ResultFlags: out.Defender.ResultFlags, Blocked: true}
		} else if skill.CkChance != 0 {
			// An unblocked ck impact rolls its kill chance (key
			// 0x48000000 | skill). A kill is record 0x86 with no damage,
			// and the target group is marked dead. Only
			// the anti-bot stone's MSKILL_AUTOMOB_ATTACK01 carries ck.
			killKey := 0x48000000 | skill.ID&0xffffff
			slain, killNext, err := combat.CriticalOutcome(float64(skill.CkChance), rt.criticals.actors[actor][killKey], rt.CombatRoll)
			if err != nil {
				return combat.WallOutcome{}, err
			}
			rt.rememberProbability(actor, killKey, killNext)
			if slain {
				out.Defender = combat.Result{ResultFlags: out.Defender.ResultFlags, Slain: true}
			}
		}
	}
	rt.rememberProbability(actor, key, next)
	return out, nil
}

/*
================
weaponKind

The named player character's worn weapon kind: the packed type word's
kind bits (the word shifted right 11), read from slot 6. A broken or
missing weapon reads 0, as the server's equipped-item TID read does.
================
*/
func (rt *Runtime) weaponKind(division, name string) uint32 {
	c := rt.findCharacter(division, name)
	if c == nil {
		return 0
	}
	return uint32(combat.ReqiEquipment{C: c, Items: rt.statCatalogs().Items}.WeaponTID(6) >> 11)
}

/*
================
fortressArmed

Whether the named player character's worn weapon (slot 6) is a fortress
weapon: the packed type word's kind bits (TID4 << 11) read 16. A broken
or missing weapon reads 0, as the server's equipped-item TID read does.
================
*/
func (rt *Runtime) fortressArmed(division, name string) bool {
	return rt.weaponKind(division, name) == 16
}

/*
================
rememberProbability

Stores an actor's roll history under its key once the roll initialized
it. The caller holds rt.criticals.mu.
================
*/
func (rt *Runtime) rememberProbability(actor criticalActor, key uint32, next combat.Probability) {
	if !next.Initialized {
		return
	}
	if rt.criticals.actors == nil {
		rt.criticals.actors = make(map[criticalActor]map[uint32]combat.Probability)
	}
	if rt.criticals.actors[actor] == nil {
		rt.criticals.actors[actor] = make(map[uint32]combat.Probability)
	}
	rt.criticals.actors[actor][key] = next
}

/*
================
forgetCriticalCharacter
================
*/
func (rt *Runtime) forgetCriticalCharacter(division, name string) {
	rt.criticals.mu.Lock()
	defer rt.criticals.mu.Unlock()
	delete(rt.criticals.actors, criticalActor{division: division, character: strings.ToLower(name)})
}

/*
================
retireMonsterCriticals

Runs every tick over every monster that has rolled and still lives (#1820),
so it reads only HP: a full Get snapshot copied the catalogue rows per
actor and made the sweep 12.5% of server CPU while retiring almost nothing.
================
*/
func (rt *Runtime) retireMonsterCriticals() {
	rt.criticals.mu.Lock()
	defer rt.criticals.mu.Unlock()
	for key := range rt.criticals.actors {
		if key.monster == 0 {
			continue
		}
		if rt.Monsters == nil {
			delete(rt.criticals.actors, key)
			continue
		}
		if hp, ok := rt.Monsters.CurrentHP(key.division, key.monster); !ok || hp == 0 {
			delete(rt.criticals.actors, key)
		}
	}
}

/*
================
effectOutcome
================
*/
func (rt *Runtime) effectOutcome(actor criticalActor, key, chance uint32) (bool, error) {
	rt.criticals.mu.Lock()
	defer rt.criticals.mu.Unlock()
	actor.character = strings.ToLower(actor.character)
	proc, next, err := combat.CriticalOutcome(float64(chance), rt.criticals.actors[actor][key], rt.CombatRoll)
	if err == nil {
		rt.rememberProbability(actor, key, next)
	}
	return proc, err
}
