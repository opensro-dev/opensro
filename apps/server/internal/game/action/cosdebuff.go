/*
===========================================================================

cosdebuff.go - a hostile debuff word held on a pet (COS)

Port of the native arm Binary Ninja verified (opensro2, 2026-10-20):
Vital Spot's terd/thrd word installs on a COS recipient through the same
mode-2 install a monster takes (no recipient-class check), and the
server's buff-modifier write lands on the flat parameter channel (params
9 and 0xB - evasion and hit rate). A monster keeps the pair in its target
effect slots; a player in the status registry; a pet keeps it here,
beside the abnormal block registry (cosabnormal.go), in runtime state
only: nothing persists, and the word dies with the summon instance that
carries it - a replacement pet inherits nothing, as the block does not.

The registry mirrors the monster's InstallMonsterTargetEffect: a word
with the same program tag replaces the active one (the recast refreshes
the skill line) and the replaced token retires on the wire; distinct
tags coexist up to the same eight-instance bound.

===========================================================================
*/

package action

import (
	"sync"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/world/monster"
)

// cosDebuffWordCap bounds one pet's active hostile words, as the
// monster's eight target-effect slots bound theirs.
const cosDebuffWordCap = 8

/*
================
cosDebuffEntry

One pet's active hostile words with the identity that names it (the
block registry's key lowercases; character lookup wants the original).
================
*/
type cosDebuffEntry struct {
	division string
	name     string
	gid      uint32
	words    []monster.SelfEffect
}

/*
================
cosDebuffStore

One pet's active hostile words, keyed as the abnormal blocks are
(division, owner name, pet GID) so the same advance pass retires both.
================
*/
type cosDebuffStore struct {
	mu      sync.Mutex
	entries map[string]*cosDebuffEntry
}

/*
================
cosDebuffCandidates

Every pet holding words, for the advance pass: a pet with words but no
abnormal block still needs its expiry tick.
================
*/
func (rt *Runtime) cosDebuffCandidates() []cosAbnormalEntry {
	rt.cosDebuffs.mu.Lock()
	defer rt.cosDebuffs.mu.Unlock()
	out := make([]cosAbnormalEntry, 0, len(rt.cosDebuffs.entries))
	for _, entry := range rt.cosDebuffs.entries {
		out = append(out, cosAbnormalEntry{division: entry.division, name: entry.name, gid: entry.gid})
	}
	return out
}

/*
================
cosDebuffWords

The pet's words whose window holds now; an expired word reads as none.
================
*/
func (rt *Runtime) cosDebuffWords(division, name string, gid uint32, now int64) []monster.SelfEffect {
	rt.cosDebuffs.mu.Lock()
	defer rt.cosDebuffs.mu.Unlock()
	entry := rt.cosDebuffs.entries[cosAbnormalKey(division, name, gid)]
	if entry == nil {
		return nil
	}
	var out []monster.SelfEffect
	for _, word := range entry.words {
		if word.Active(now) {
			out = append(out, word)
		}
	}
	return out
}

/*
================
installCosDebuffWord

Install one word on the pet: the same-tag word is replaced and its
token returned for the wire, a ninth distinct word refuses.
================
*/
func (rt *Runtime) installCosDebuffWord(division, name string, gid uint32, word monster.SelfEffect, now int64) (uint32, bool) {
	if word.Token == 0 || word.SkillID == 0 || word.UntilMs <= now {
		return 0, false
	}
	rt.cosDebuffs.mu.Lock()
	defer rt.cosDebuffs.mu.Unlock()
	key := cosAbnormalKey(division, name, gid)
	entry := rt.cosDebuffs.entries[key]
	if entry == nil {
		entry = &cosDebuffEntry{division: division, name: name, gid: gid}
		if rt.cosDebuffs.entries == nil {
			rt.cosDebuffs.entries = map[string]*cosDebuffEntry{}
		}
		rt.cosDebuffs.entries[key] = entry
	}
	for i, active := range entry.words {
		if active.Token != 0 && active.Tag == word.Tag {
			replaced := active.Token
			entry.words[i] = word
			return replaced, true
		}
	}
	if len(entry.words) >= cosDebuffWordCap {
		return 0, false
	}
	entry.words = append(entry.words, word)
	return 0, true
}

/*
================
clearCosDebuffWords

Drop every word the pet holds (death, dismissal, expiry sweep) and
return the live tokens so the caller can retire them on the wire.
================
*/
func (rt *Runtime) clearCosDebuffWords(division, name string, gid uint32, now int64) []uint32 {
	rt.cosDebuffs.mu.Lock()
	defer rt.cosDebuffs.mu.Unlock()
	key := cosAbnormalKey(division, name, gid)
	entry := rt.cosDebuffs.entries[key]
	if entry == nil {
		return nil
	}
	var tokens []uint32
	for _, word := range entry.words {
		if word.Active(now) && word.Token != 0 {
			tokens = append(tokens, word.Token)
		}
	}
	delete(rt.cosDebuffs.entries, key)
	return tokens
}

/*
================
expireCosDebuffWords

The advance pass's sweep: words past their window leave, and the tokens
that were still live come back for the teardown frames.
================
*/
func (rt *Runtime) expireCosDebuffWords(division, name string, gid uint32, now int64) []uint32 {
	rt.cosDebuffs.mu.Lock()
	defer rt.cosDebuffs.mu.Unlock()
	key := cosAbnormalKey(division, name, gid)
	entry := rt.cosDebuffs.entries[key]
	if entry == nil || len(entry.words) == 0 {
		return nil
	}
	var tokens []uint32
	kept := entry.words[:0]
	for _, word := range entry.words {
		if word.Active(now) {
			kept = append(kept, word)
		} else if word.Token != 0 {
			tokens = append(tokens, word.Token)
		}
	}
	if len(kept) == 0 {
		delete(rt.cosDebuffs.entries, key)
	} else {
		entry.words = kept
	}
	return tokens
}

// The terd and thrd program tags a hostile word carries (enterworld's
// tagEvasionDecrease / tagHitRateDecrease; combat's monster projection
// names the same pair for the monster slots).
const (
	cosDebuffEvasionTag = 0x74657264
	cosDebuffHitRateTag = 0x74687264
)

/*
================
applyCosDebuffWords

The flat subtraction at the stat projection's sink (cosCombatStats):
each active word lowers its parameter - terd evasion, thrd hit rate -
floored at zero, the paramkeeper's own minimum for both parameters,
which is the subtraction the monster's stat owner performs natively.
================
*/
func applyCosDebuffWords(stats *combat.Stats, words []monster.SelfEffect) {
	for _, word := range words {
		switch word.Tag {
		case cosDebuffEvasionTag:
			stats.EvasionRate = max(0, stats.EvasionRate-float64(word.First))
		case cosDebuffHitRateTag:
			stats.HitRate = max(0, stats.HitRate-float64(word.First))
		}
	}
}
