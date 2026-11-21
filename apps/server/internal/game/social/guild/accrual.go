/*
===========================================================================

accrual.go - guild points that accrue passively while members hunt

Native rule (issue #4051): when a guilded member's hunting skill EXP
converts to SP (400 skill EXP = 1 SP, progression's SkillExpPerSP), the
same whole-SP amount is set aside for the guild in a per-character
accumulator. At 10 (0xA) or more the set-aside is credited through the
same credit a manual SP donation lands (guild GP and the member's
DonatedGP rise together), and any smaller remainder is credited when
the member leaves the guild or their session closes, so nothing earned
is lost. The accumulator is memory-only, as natively: a crash loses
only the sub-threshold remainder of one online session.

Ownership: progression mints SP and reports the mint (SPMintedHook);
this tracker only records. The credit door (CreditGuildPoints) runs
from Drain (the simulation tick) and FlushCharacter (leave / session
close) - never from inside a character transaction.

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

// gpAccrualEntry is one character's un-credited set-aside and the guild
// it was earned in (a membership change must not re-home points).
type gpAccrualEntry struct {
	guildID int64
	amount  int64
}

/*
================
GpAccrual

The guild GP accrual tracker: a mutex-guarded set of per-character
set-asides. NoteSPMinted is safe to call from inside a character
transaction (it only records); Drain and FlushCharacter open the
credit door and fan the GP pushes out, so they run from the tick and
from packet/session-close handling, never from a character door.
================
*/
type GpAccrual struct {
	deps     Dependencies
	presence Presence

	mu      sync.Mutex
	pending map[gpAccrualKey]gpAccrualEntry
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
		pending:  make(map[gpAccrualKey]gpAccrualEntry),
	}
}

/*
================
NoteSPMinted - GpAccrual

Record whole SP minted by one guilded character. Called from the
progression mint inside the character transaction: it only takes the
tracker mutex, so it can never deadlock a store door.
================
*/
func (a *GpAccrual) NoteSPMinted(divisionID string, character *enterworld.Character, spMinted int64) {
	if a == nil || character == nil || character.GuildID == nil || spMinted <= 0 {
		return
	}
	key := gpAccrualKey{divisionID: divisionID, characterID: character.ID}
	a.mu.Lock()
	entry := a.pending[key]
	entry.guildID = *character.GuildID
	entry.amount += spMinted
	a.pending[key] = entry
	a.mu.Unlock()
}

/*
================
Drain - GpAccrual

Credit every set-aside that reached the native flush threshold. The
tick calls this once per pass; sub-threshold remainders keep waiting
for more hunting, a leave, or a session close.
================
*/
func (a *GpAccrual) Drain() {
	if a == nil {
		return
	}
	a.mu.Lock()
	due := make(map[gpAccrualKey]gpAccrualEntry)
	for key, entry := range a.pending {
		if entry.amount >= gpAccrualFlushThreshold {
			due[key] = entry
			delete(a.pending, key)
		}
	}
	a.mu.Unlock()
	for key, entry := range due {
		a.credit(key, entry)
	}
}

/*
================
FlushCharacter - GpAccrual

Credit one character's whole set-aside whatever its size - the native
remainder flush when the guild pointer goes away (guild leave) or the
session ends. Unknown characters are a no-op.
================
*/
func (a *GpAccrual) FlushCharacter(divisionID string, characterID int64) {
	if a == nil {
		return
	}
	key := gpAccrualKey{divisionID: divisionID, characterID: characterID}
	a.mu.Lock()
	entry, ok := a.pending[key]
	if ok {
		delete(a.pending, key)
	}
	a.mu.Unlock()
	if ok && entry.amount > 0 {
		a.credit(key, entry)
	}
}

/*
================
credit - GpAccrual

Land one set-aside through the credit door and fan the two 0x3B29 GP
deltas out to the guild's online members (the donation fan-out minus
the donor-only ack: a passive credit has no requesting client). The
entry is already out of the tracker, matching the native send-and-zero:
a refusal (the member left, the guild dissolved, a u32 would wrap)
drops the points with a log line instead of retrying forever.
================
*/
func (a *GpAccrual) credit(key gpAccrualKey, entry gpAccrualEntry) {
	if a.deps == nil || a.deps.GuildAuthority() == nil || entry.amount < 1 {
		return
	}
	// The points belong to the guild they were earned in: if the
	// character has since left or changed guilds (a detach flush was
	// missed, e.g. a kick), drop them rather than re-home them onto
	// the new guild. The native flush leaves with the guild pointer,
	// before any new membership exists.
	if guildID, ok := a.deps.GuildAuthority().GuildOfCharacter(key.divisionID, key.characterID); !ok || guildID != entry.guildID {
		log.Debugf("guild: GP accrual of %d for character %d dropped (earned in guild %d, no longer a member)",
			entry.amount, key.characterID, entry.guildID)
		return
	}
	amount := entry.amount
	for amount > 0 {
		chunk := amount
		if chunk > 0xffffffff {
			chunk = 0xffffffff
		}
		result, refusal := a.deps.GuildAuthority().CreditGuildPoints(
			key.divisionID, key.characterID, uint32(chunk))
		if refusal.Refused() {
			log.Debugf("guild: GP accrual of %d for character %d dropped (%s)",
				entry.amount, key.characterID, guildRefusalReason(refusal))
			return
		}
		if result.Snapshot.Guild.ID != entry.guildID {
			// The character changed guilds after earning these points;
			// the credit landed on the guild of record at earn time or
			// not at all - never re-home them onto a new guild.
			log.Debugf("guild: GP accrual of %d for character %d dropped (earned in guild %d, credited guild %d)",
				entry.amount, key.characterID, entry.guildID, result.Snapshot.Guild.ID)
			return
		}
		names := make([]string, 0, len(result.Snapshot.Members))
		for _, member := range result.Snapshot.Members {
			names = append(names, member.Name)
		}
		sendToOnlineMembers(a.presence, key.divisionID, entry.guildID, names,
			EncodeGuildGp3B29(result.Snapshot.Guild.GP))
		sendToOnlineMembers(a.presence, key.divisionID, entry.guildID, names,
			EncodeMemberDonatedGp3B29(result.Donor.JID, result.Donor.DonatedGP))
		amount -= chunk
	}
}
