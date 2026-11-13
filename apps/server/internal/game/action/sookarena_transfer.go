/*
===========================================================================
sookarena_transfer.go - port-only Chain Reaction from committed player hits.

Secondary hits use the credited lane, never the player strike lane, so they
cannot recursively copy damage or consume additional attacks.
===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
sookTransferHit
================
*/
type sookTransferHit struct {
	division  string
	character *enterworld.Character
	at        simulation.Spawn
	impacts   []simulation.MonsterDamageResult
	now       int64
}

/*
================
transferSookDamage - copy actual HP debits, not overkill, inside the exact lease.
================
*/
func (rt *Runtime) transferSookDamage(in sookTransferHit) {
	c := in.character
	run := rt.sookRun(in.division, c)
	if run == nil || run.program.PausedAt != 0 || in.now >= run.program.TransferUntil || len(in.impacts) == 0 || in.impacts[0].Population != run.lease {
		return
	}
	var total uint64
	for _, impact := range in.impacts {
		total += uint64(impact.Applied)
	}
	damage := uint32(min(total, uint64(wire.MaxSkillActionDamage)))
	if damage == 0 {
		return
	}
	snapshot := rt.characterSnapshotCombat(in.division, c)
	if snapshot == nil {
		return
	}
	count := 0
	for _, target := range rt.Monsters.CombatCandidatesInPopulation(in.division, run.lease, in.at, 100, in.now, true) {
		if target.Gid == in.impacts[0].Instance.Gid || target.CurrentHP == 0 {
			continue
		}
		hit, ok := rt.commitCreditedMonsterHit(in.division, c, snapshot, enterworld.SkillRow{}, target, combat.Result{Damage: damage, ResultFlags: 1}, "sook-transfer", in.now)
		if !ok {
			continue
		}
		impact := hit.impacts[0]
		out := rt.creditedHitResult(in.division, target, hit, []wire.Frame{wire.SkillDamageTickFrame(enterworld.ObjectIDForCharacter(c), impact.Fatal, target.Gid, []uint32{impact.Damage})}, in.now)
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: in.division, OnlyCharacterID: c.ID, Frames: simFrames(out.Broadcast)}, {DivisionID: in.division, OnlyCharacterID: c.ID, Frames: simFrames(out.ActorPrivate)}})
		rt.queueMonsterLegFrames(recipientDivisionFrames(in.division, out.Recipients))
		count++
		if count == 10 {
			break
		}
	}
}
