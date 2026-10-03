/*
===========================================================================

resurrection.go - resu: the proposal to a dead player and its answer

A resurrection cast revives nobody by itself. SkillCombat_Apply-
SkillEffectsToTargets (5946C5..5949A6) looks at each target that is a dead
player (life 2) no higher than resu word 0, works out what a revival would
give, and queues a proposal through CGObjPC_EnqueuePeerResponseProposal
(4EA0D0): 0x3080 type 4, or type 8 with an rmut skill. The v1.150 wire is
0x3393 {u8 4, u32 casterGid}; the client opens confirm box kind 4 (7644E0)
and answers 0x3393 {1, button} (CGInterface_OnMsgBoxResult case 1).

TrsWaitResponse_OnResponse (46CB30) applies a nonzero answer from a player
who is still dead: revive where the corpse lies (CGObjPC_TeleportToTown
4DF290, arg 1), add the EXP, add the HP and MP through
CGObjChar_ApplyReducedRecovery, then start the rmut skill. A refusal, a
repeated answer and the 30 s timeout (Transaction_Construct 46C6C0) do
nothing. The transaction manager keys proposals by the answering player,
so one already waiting drops the new one (TransactionMgr_InsertUnique).

===========================================================================
*/

package action

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

const (
	// opInvitationProposal is the shared 0x3393 prompt and answer.
	opInvitationProposal uint16 = 0x3393

	// resurrectionProposalType is the 0x3393 kind that opens box kind 4.
	resurrectionProposalType uint8 = 4

	// resurrectionAnswerWindowMs is the transaction's 30 s timeout;
	// 46F1E0 expires a proposal after it, not at it.
	resurrectionAnswerWindowMs = 30 * 1000
)

/*
==================
resurrectionOffer

What the proposal carries (trs_wait_response +0x2C..+0x40): the caster,
the EXP, HP and MP a revival adds, and the rmut skill it starts.
==================
*/
type resurrectionOffer struct {
	casterGID uint32
	exp       int64
	hp, mp    int64
	rmut      uint32
	expiresMs int64
}

/*
================
resurrectionOffers

The table of unanswered proposals, keyed by the dead player.
================
*/
type resurrectionOffers struct {
	mu       sync.Mutex
	byTarget map[string]resurrectionOffer
}

/*
================
resurrectionKey
================
*/
func resurrectionKey(divisionID, name string) string {
	return divisionID + "\x00" + strings.ToLower(name)
}

/*
================
pending

Reports an offer still inside its answer window.
================
*/
func (o *resurrectionOffers) pending(divisionID, name string, nowMs int64) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	offer, ok := o.byTarget[resurrectionKey(divisionID, name)]
	return ok && nowMs <= offer.expiresMs
}

/*
================
put
================
*/
func (o *resurrectionOffers) put(divisionID, name string, offer resurrectionOffer) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.byTarget[resurrectionKey(divisionID, name)] = offer
}

/*
================
take

Consumes the offer; an expired one is consumed and reported absent.
================
*/
func (o *resurrectionOffers) take(divisionID, name string, nowMs int64) (resurrectionOffer, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	key := resurrectionKey(divisionID, name)
	offer, ok := o.byTarget[key]
	delete(o.byTarget, key)
	return offer, ok && nowMs <= offer.expiresMs
}

/*
================
drop
================
*/
func (o *resurrectionOffers) drop(divisionID, name string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	key := resurrectionKey(divisionID, name)
	_, ok := o.byTarget[key]
	delete(o.byTarget, key)
	return ok
}

/*
===============================================================================

PROPOSAL

===============================================================================
*/

/*
==================
proposeResurrection

The 5946C5 arm for one recipient. The caller holds the division lock and
has already validated the target (0x3012 above word 0). Returns the prompt
for the recipient, or nothing when the player is not dead or already has a
proposal waiting.
==================
*/
func (rt *Runtime) proposeResurrection(division string, caster, recipient *enterworld.Character, skill enterworld.SkillRow, nowMs int64) []wire.Frame {
	if recipient == nil || enterworld.CharacterAlive(recipient) || recipient.DeletePending {
		return nil
	}
	if rt.resurrections.pending(division, recipient.Name, nowMs) ||
		rt.ProposalPending != nil && rt.ProposalPending(division, recipient.Name) {
		return nil
	}
	resu := skill.Abnormal
	if resu.ResuMaxLevel != 0 && recipient.Level != nil && *recipient.Level > int64(resu.ResuMaxLevel) {
		return nil
	}

	maxHP, maxMP, _, _ := rt.playerKeeperVitals(division, recipient)
	hp, mp := resurrectionVitals(skill.Heal, maxHP, maxMP)
	casterGID := enterworld.ObjectIDForCharacter(caster)
	rt.resurrections.put(division, recipient.Name, resurrectionOffer{
		casterGID: casterGID,
		exp:       resurrectionExp(recipient.LastExpLoss, resu.ResuExpPercent, recipient.PVPState() == 2),
		hp:        hp,
		mp:        mp,
		rmut:      resu.Rmut,
		expiresMs: nowMs + resurrectionAnswerWindowMs,
	})
	payload := wire.NewWriter(5).U8(resurrectionProposalType).U32(casterGID).Payload()
	return []wire.Frame{{Opcode: opInvitationProposal, Payload: payload}}
}

/*
==================
resurrectionVitals

594780..5947FF: with a heal block, HP is word 0 plus word 1 percent of the
recipient's maximum and MP is word 2 plus word 3 percent. Each product is a
32-bit IMUL read unsigned, divided by -100 and truncated, then subtracted.
Without a heal block both are 0.
==================
*/
func resurrectionVitals(heal enterworld.SkillHeal, maxHP, maxMP int64) (hp, mp int64) {
	if !heal.Present {
		return 0, 0
	}
	share := func(percent uint32, maximum int64) int32 {
		product := percent * uint32(maximum)
		return int32(crtFtol(float64(product) / -100))
	}
	hp = int64(int32(heal.HP) - share(heal.HPPercent, maxHP))
	mp = int64(int32(heal.MP) - share(heal.MPPercent, maxMP))
	return hp, mp
}

/*
==================
resurrectionExp

594805..594848: the recovered EXP is |char+0x1CE0| times word 1 percent,
held as a float and truncated. A murderer (PvP state, state+0xC, is 2)
gets half the percent.
==================
*/
func resurrectionExp(lastLoss int64, percent uint32, murderer bool) int64 {
	loss := int64(int32(lastLoss))
	if loss < 0 {
		loss = -loss
	}
	share := float32(float64(percent) / 100)
	if murderer {
		share = float32(float64(share) * 0.5)
	}
	exp := float32(float64(loss) * float64(share))
	return int64(math.Trunc(float64(exp)))
}

/*
===============================================================================

ANSWER

===============================================================================
*/

/*
==================
ResurrectionConsent

The resurrection lane on the shared 0x3393 answer (party.ConsentArm,
implemented structurally): the party router hands it the answer of any
player it holds a proposal for.
==================
*/
type ResurrectionConsent struct {
	rt *Runtime
}

/*
================
ResurrectionConsent

Returns the lane the composition root registers.
================
*/
func (rt *Runtime) ResurrectionConsent() *ResurrectionConsent {
	return &ResurrectionConsent{rt: rt}
}

/*
================
HasPendingInvite

Reports a proposal still inside its answer window.
================
*/
func (c *ResurrectionConsent) HasPendingInvite(divisionID, name string) bool {
	return c.rt.resurrections.pending(divisionID, name, c.rt.Now().UnixMilli())
}

/*
================
DropPendingInvite

Forgets the proposal for a character.
================
*/
func (c *ResurrectionConsent) DropPendingInvite(divisionID, name string) bool {
	return c.rt.resurrections.drop(divisionID, name)
}

/*
==================
ApplyConsent

Box kind 4 answers {1, button}; button 1 is yes. Any other answer consumes
the proposal and changes nothing, like TrsWaitResponse_OnResponse's zero
answer.
==================
*/
func (c *ResurrectionConsent) ApplyConsent(_ *transport.Session, divisionID string, actor *enterworld.Character, first, second uint8) {
	rt := c.rt
	if actor == nil {
		return
	}
	nowMs := rt.Now().UnixMilli()
	offer, ok := rt.resurrections.take(divisionID, actor.Name, nowMs)
	if !ok || first != 1 || second != 1 {
		return
	}
	actorFrames, peerFrames := rt.acceptResurrection(divisionID, actor.Name, offer, nowMs)
	if len(actorFrames) == 0 {
		return
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(divisionID, actor.Name, actorFrames)
	}
	if rt.PushDivisionPeerFrames != nil && len(peerFrames) > 0 {
		rt.PushDivisionPeerFrames(divisionID, actor.Name, peerFrames)
	}
}

/*
==================
acceptResurrection

46CB30 for a yes. The player must still be dead. One character door
commits the revival where the corpse lies with 1 HP (4DF290 arg 1: every
revival also clears the recorded EXP loss), the EXP (vfunc +0x170), the HP
and MP (applySkillRecovery is ApplyReducedRecovery) and the rmut skill.
Native starts rmut as an indirect cast (CSkillManager_BeginIndirectSkill);
this port installs its effect directly.
==================
*/
func (rt *Runtime) acceptResurrection(division, name string, offer resurrectionOffer, nowMs int64) (actor, peers []wire.Frame) {
	unlock := rt.lockDivision(division)
	defer unlock()

	character := rt.findCharacter(division, name)
	if character == nil {
		return nil, nil
	}
	var rmut enterworld.SkillRow
	hasRmut := false
	if skills := rt.deps.SkillData(); skills != nil && offer.rmut != 0 {
		rmut, hasRmut = skills.SkillByID(offer.rmut)
	}

	worldKey := simulation.WorldKey(division, name)
	var at simulation.Spawn
	var revivedVitals []byte
	var progression, recovery, effects, untouchable []wire.Frame
	if !rt.deps.Update(character, "resurrection-accept", func() bool {
		if character.DeletePending || enterworld.CharacterAlive(character) {
			return false
		}
		state := rt.Worlds.Update(worldKey,
			func() simulation.WorldState { return simulation.SeedWorldState(character) },
			func(world *simulation.WorldState) {
				world.MoveSegment = nil
				world.LifeRevision++
				world.Sitting = false
				world.PostureTransitionUntilMs = 0
			})
		writeBackWorld(character, state)
		character.World.MoveSegment = nil
		at = state.Spawn

		revived := int64(1)
		character.CurrentHP = &revived
		character.LastExpLoss = 0
		// 46CB30 revives through 4DF290 before applying the offered recovery.
		// Use the same protection owner as self-rebirth so expiry and replacement
		// cannot leave the player permanently protected or immediately vulnerable.
		untouchable = rt.grantReviveUntouchable(division, character, nowMs)
		revivedVitals = enterworld.BuildVitalsRefreshPayload(character)

		if offer.exp > 0 && rt.UpdateExperience != nil {
			progression, _ = rt.UpdateExperience(character, offer.exp, 0, 0)
		}
		if offer.hp != 0 || offer.mp != 0 {
			frame, ok := rt.applySkillRecovery(division, character, offer.hp, offer.mp)
			if !ok {
				log.Warnf("action: resurrection of %s revived without its HP/MP: no combat stats", name)
			} else if frame.Opcode != 0 {
				recovery = append(recovery, frame)
			}
		}
		if hasRmut {
			token := atomic.AddUint32(&rt.castTokenCounter, 1)
			installed, ok := rt.commitCharacterEffect(division, character, rmut, token, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, nowMs)
			if !ok {
				log.Warnf("action: resurrection of %s could not install rmut skill %d", name, offer.rmut)
			}
			effects = installed
		}
		return true
	}) {
		return nil, nil
	}
	rt.bindResidentRegion(worldKey, nowMs)
	rt.ClearCombatIntent(division, name)

	correction, vitals, life := rebirthFrames(enterworld.ObjectIDForCharacter(character), at, revivedVitals)
	actor = append([]wire.Frame{correction, vitals, life}, untouchable...)
	actor = append(actor, progression...)
	actor = append(actor, recovery...)
	actor = append(actor, effects...)
	peers = append([]wire.Frame{correction, life}, untouchable...)
	peers = append(peers, effects...)
	return actor, peers
}
