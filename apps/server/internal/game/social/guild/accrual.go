/*
===========================================================================

accrual.go - guild points that accrue passively while members hunt

Native rule (issue #4051, v1.188 SR_GameServer): when a guilded member's
hunting skill EXP converts to SP (400 skill EXP = 1 SP), the same
whole-SP amount is set aside on the player object (PC +0x2184,
CGObjPC_ApplyExperienceAndSend3056 0x4E5625). At 10 (0xA) or more it is
credited to the member's current guild through the donation credit
(guild GP and the member's DonatedGP rise together) and zeroed. On exit
(CGObjPC_ReleaseSessionsOnExit 0x4DEF90) a positive remainder is
credited only while the member is still guilded.

Nothing on the guild leave, kick or disband path touches the set-aside
(an `/ad/ 0x2184]` search finds only the mint, the exit flush and the
spawn reset), so a remainder survives a leave and is credited to
whatever guild the member is in at the next credit. A guildless logout
drops it. The set-aside is memory-only, as natively.

Ownership: progression mints SP and reports the mint (SPMintedHook);
this tracker only records. The credit door (CreditGuildPoints) runs
from Drain (the simulation tick) and SessionClosed - never from
inside a character transaction.

===========================================================================
*/

package guild

import (
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
)

// gpAccrualFlushThreshold is the native flush point (0xA): the set-aside
// is sent for credit the moment it reaches this many SP.
const gpAccrualFlushThreshold int64 = 10

// gpAccrualKey names one online earner inside one division.
type gpAccrualKey struct {
	divisionID  string
	characterID int64
}

/*
================
GpAccrual

The guild GP accrual tracker: a mutex-guarded set of per-character
set-asides, keyed by the character alone (natively the field lives on
the player, not on the membership). NoteSPMinted is safe to call from
inside a character transaction (it only records); Drain and
SessionClosed open the credit door and fan the GP pushes out, so they
run from the tick and from session-close handling, never from a
character door.
================
*/
type GpAccrual struct {
	deps     Dependencies
	presence Presence

	mu      sync.Mutex
	pending map[gpAccrualKey]int64
}

/*
================
NewGpAccrual
================
*/
func NewGpAccrual(deps Dependencies, presence Presence) *GpAccrual {
	return &GpAccrual{
		deps:     deps,
		presence: presence,
		pending:  make(map[gpAccrualKey]int64),
	}
}

/*
================
NoteSPMinted - GpAccrual

Record whole SP minted by one guilded character (natively only a
character with a guild pointer adds to the set-aside, 0x4E561C). Called
from the progression mint inside the character transaction: it only
takes the tracker mutex, so it can never deadlock a store door.
================
*/
func (a *GpAccrual) NoteSPMinted(divisionID string, character *enterworld.Character, spMinted int64) {
	if a == nil || character == nil || character.GuildID == nil || spMinted <= 0 {
		return
	}
	key := gpAccrualKey{divisionID: divisionID, characterID: character.ID}
	a.mu.Lock()
	a.pending[key] += spMinted
	a.mu.Unlock()
}

/*
================
Drain - GpAccrual

Credit every set-aside that reached the native threshold to the
earner's current guild. The tick calls this once per pass. A set-aside
whose character has no guild by now (left between the mint and the
tick) goes back into the tracker, as the native remainder stays on the
player until a later credit or exit.
================
*/
func (a *GpAccrual) Drain() {
	if a == nil {
		return
	}
	a.mu.Lock()
	due := make(map[gpAccrualKey]int64)
	for key, amount := range a.pending {
		if amount >= gpAccrualFlushThreshold {
			due[key] = amount
			delete(a.pending, key)
		}
	}
	a.mu.Unlock()
	for key, amount := range due {
		if !a.credit(key, amount) {
			a.mu.Lock()
			a.pending[key] += amount
			a.mu.Unlock()
		}
	}
}

/*
================
SessionClosed - GpAccrual

The native exit flush (0x4DEF90): credit one character's whole
set-aside, whatever its size, to their current guild; a guildless
character's remainder is dropped with the session. Unknown characters
are a no-op.
================
*/
func (a *GpAccrual) SessionClosed(divisionID string, characterID int64) {
	if a == nil {
		return
	}
	key := gpAccrualKey{divisionID: divisionID, characterID: characterID}
	a.mu.Lock()
	amount, ok := a.pending[key]
	delete(a.pending, key)
	a.mu.Unlock()
	if ok && amount > 0 && !a.credit(key, amount) {
		log.Debugf("guild: GP remainder of %d for character %d dropped at exit (no guild)",
			amount, characterID)
	}
}

/*
================
credit - GpAccrual

Land one set-aside on the character's current guild through the credit
door and fan the two 0x3B29 GP deltas out to its online members (the
donation fan-out minus the donor-only ack: a passive credit has no
requesting client). Returns false only when the character has no guild,
so the caller decides whether the amount waits or is dropped. Any other
refusal (a u32 would wrap, the guild dissolved mid-credit) drops the
points with a log line, matching the native send-and-zero.
================
*/
func (a *GpAccrual) credit(key gpAccrualKey, amount int64) bool {
	if a.deps == nil || a.deps.GuildAuthority() == nil || amount < 1 {
		return true
	}
	if _, ok := a.deps.GuildAuthority().GuildOfCharacter(key.divisionID, key.characterID); !ok {
		return false
	}
	for left := amount; left > 0; {
		chunk := left
		if chunk > 0xffffffff {
			chunk = 0xffffffff
		}
		result, refusal := a.deps.GuildAuthority().CreditGuildPoints(
			key.divisionID, key.characterID, uint32(chunk))
		if refusal.Refused() {
			log.Debugf("guild: GP accrual of %d for character %d dropped (%s)",
				amount, key.characterID, guildRefusalReason(refusal))
			return true
		}
		guildID := result.Snapshot.Guild.ID
		names := make([]string, 0, len(result.Snapshot.Members))
		for _, member := range result.Snapshot.Members {
			names = append(names, member.Name)
		}
		sendToOnlineMembers(a.presence, key.divisionID, guildID, names,
			EncodeGuildGp3B29(result.Snapshot.Guild.GP))
		sendToOnlineMembers(a.presence, key.divisionID, guildID, names,
			EncodeMemberDonatedGp3B29(result.Donor.JID, result.Donor.DonatedGP))
		left -= chunk
	}
	return true
}
