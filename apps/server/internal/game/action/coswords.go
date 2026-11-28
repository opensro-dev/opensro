/*
===========================================================================

coswords.go - the effect words held on a pet (COS)

A pet holds an actor's installed program words here, beside the abnormal
block registry (cosabnormal.go), in runtime state only: nothing
persists, and a word dies with the summon instance that carries it - a
replacement pet inherits nothing, as the block does not.

Two polarities share the store. The hostile words are the port of the
native arm verified in 2026-10-20: Vital Spot's terd/thrd word installs
on a COS recipient through the same mode-2 install a monster takes (no
recipient-class check), and the server's buff-modifier write lands on
the flat parameter channel (params 9 and 0xB - evasion and hit rate).
The friendly words are the fortress structures' aura programs (dru,
odar, irgc) a holder-side companion holds under #4490: the structure
skills' native target check admits a companion recipient and judges it
by its owner's guild, so the holder's pet stands in the area as a
beneficiary beside its owner.

A monster keeps its instances in its target-effect slots; a player in
the status registry; a pet keeps them here. The registry mirrors the
monster's InstallMonsterTargetEffect: a word with the same program tag
replaces the active one (the recast refreshes the skill line) and the
replaced token retires on the wire; distinct tags coexist up to the
same eight-instance bound, hostile and friendly together.

===========================================================================
*/

package action

import (
	"sync"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/world/monster"
)

// cosWordCap bounds one pet's active words, hostile and friendly together, as the
// monster's eight target-effect slots bound theirs.
const cosWordCap = 8

/*
================
cosWordEntry

One pet's active words with the identity that names it (the
block registry's key lowercases; character lookup wants the original).
================
*/
type cosWordEntry struct {
	division string
	name     string
	gid      uint32
	words    []monster.SelfEffect
}

/*
================
cosWordStore

One pet's active words, keyed as the abnormal blocks are
(division, owner name, pet GID) so the same advance pass retires both.
================
*/
type cosWordStore struct {
	mu      sync.Mutex
	entries map[cosAbnormalKey]*cosWordEntry
}

/*
================
cosWordCandidates

Every pet holding words, for the advance pass: a pet with words but no
abnormal block still needs its expiry tick.
================
*/
func (rt *Runtime) cosWordCandidates() []cosAbnormalEntry {
	rt.cosWords.mu.Lock()
	defer rt.cosWords.mu.Unlock()
	out := make([]cosAbnormalEntry, 0, len(rt.cosWords.entries))
	for _, entry := range rt.cosWords.entries {
		out = append(out, cosAbnormalEntry{division: entry.division, name: entry.name, gid: entry.gid})
	}
	return out
}

/*
================
cosWordsHeld

The pet's words whose window holds now; an expired word reads as none.
================
*/
func (rt *Runtime) cosWordsHeld(division, name string, gid uint32, now int64) []monster.SelfEffect {
	rt.cosWords.mu.Lock()
	defer rt.cosWords.mu.Unlock()
	entry := rt.cosWords.entries[cosAbnormalKeyFor(division, name, gid)]
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
installCosWord

Install one word on the pet: the word it supersedes is replaced and its
token returned for the wire, a ninth distinct word refuses. A hostile
word supersedes any word of its tag, as the monster's target-effect
slots replace one; a friendly word (dru, odar, irgc) supersedes only
its own skill's word of that tag. The owner holds one instance per
structure skill, so an aggressive flag (dru) and a defensive position
(dru, odar) overlapping both lift the pet, as they lift the owner;
replacing by tag alone would evict each other's word on every scan.
================
*/
func (rt *Runtime) installCosWord(division, name string, gid uint32, word monster.SelfEffect, now int64) (uint32, bool) {
	if word.Token == 0 || word.SkillID == 0 || word.UntilMs <= now {
		return 0, false
	}
	rt.cosWords.mu.Lock()
	defer rt.cosWords.mu.Unlock()
	key := cosAbnormalKeyFor(division, name, gid)
	entry := rt.cosWords.entries[key]
	if entry == nil {
		entry = &cosWordEntry{division: division, name: name, gid: gid}
		if rt.cosWords.entries == nil {
			rt.cosWords.entries = map[cosAbnormalKey]*cosWordEntry{}
		}
		rt.cosWords.entries[key] = entry
	}
	for i, active := range entry.words {
		if active.Token != 0 && active.Tag == word.Tag && (!cosWordFriendly(word.Tag) || active.SkillID == word.SkillID) {
			replaced := active.Token
			entry.words[i] = word
			return replaced, true
		}
	}
	if len(entry.words) >= cosWordCap {
		return 0, false
	}
	entry.words = append(entry.words, word)
	return 0, true
}

/*
================
clearCosWords

Drop every word the pet holds (death, dismissal, expiry sweep) and
return the live tokens so the caller can retire them on the wire.
================
*/
func (rt *Runtime) clearCosWords(division, name string, gid uint32, now int64) []uint32 {
	rt.cosWords.mu.Lock()
	defer rt.cosWords.mu.Unlock()
	key := cosAbnormalKeyFor(division, name, gid)
	entry := rt.cosWords.entries[key]
	if entry == nil {
		return nil
	}
	var tokens []uint32
	for _, word := range entry.words {
		if word.Active(now) && word.Token != 0 {
			tokens = append(tokens, word.Token)
		}
	}
	delete(rt.cosWords.entries, key)
	return tokens
}

/*
================
expireCosWords

The advance pass's sweep: words past their window leave, and the tokens
that were still live come back for the teardown frames.
================
*/
func (rt *Runtime) expireCosWords(division, name string, gid uint32, now int64) []uint32 {
	rt.cosWords.mu.Lock()
	defer rt.cosWords.mu.Unlock()
	key := cosAbnormalKeyFor(division, name, gid)
	entry := rt.cosWords.entries[key]
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
		delete(rt.cosWords.entries, key)
	} else {
		entry.words = kept
	}
	return tokens
}

// The terd and thrd program tags a hostile word carries (enterworld's
// tagEvasionDecrease / tagHitRateDecrease; combat's monster projection
// names the same pair for the monster slots).
const (
	cosWordEvasionTag = 0x74657264
	cosWordHitRateTag = 0x74687264
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
		case cosWordEvasionTag:
			stats.EvasionRate = max(0, stats.EvasionRate-float64(word.First))
		case cosWordHitRateTag:
			stats.HitRate = max(0, stats.HitRate-float64(word.First))
		}
	}
}

/*
================
retireCosWords

Drop one skill's words from the pet (the aura lane's tracked retire)
and return the tokens still live, so the caller can retire them on the
wire; a word already past its window leaves silently, as the advance
pass retires it itself.
================
*/
func (rt *Runtime) retireCosWords(division, name string, gid uint32, skillID uint32, now int64) []uint32 {
	rt.cosWords.mu.Lock()
	defer rt.cosWords.mu.Unlock()
	key := cosAbnormalKeyFor(division, name, gid)
	entry := rt.cosWords.entries[key]
	if entry == nil {
		return nil
	}
	var tokens []uint32
	kept := entry.words[:0]
	for _, word := range entry.words {
		if word.SkillID != skillID {
			kept = append(kept, word)
			continue
		}
		if word.Active(now) && word.Token != 0 {
			tokens = append(tokens, word.Token)
		}
	}
	if len(kept) == 0 {
		delete(rt.cosWords.entries, key)
	} else {
		entry.words = kept
	}
	return tokens
}

/*
================
cosBuffRecoveryRates

The pet's active recovery-rate words (irgc), summed on the percent-sum
channel the character keeper sums them on: word 0 the health rate, word
1 the mana rate. The pet's natural recovery pulse reads them
(petnaturalrecovery.go).
================
*/
func (rt *Runtime) cosBuffRecoveryRates(division, name string, gid uint32, now int64) (hp, mp uint32) {
	for _, word := range rt.cosWordsHeld(division, name, gid, now) {
		if word.Tag == cosWordRecoveryTag {
			hp += word.First
			mp += word.Second
		}
	}
	return hp, mp
}

// The dru, odar and irgc program tags a friendly word carries
// (enterworld's itemEffectDamage / itemEffectAbsorb / itemEffectRecovery;
// the character installer's words ride the same tags).
/*
================
cosWordFriendly

Whether a word tag is one of the friendly program words, which stack
across skills (installCosWord).
================
*/
func cosWordFriendly(tag uint32) bool {
	return tag == cosWordDamageTag || tag == cosWordAbsorbTag || tag == cosWordRecoveryTag
}

const (
	cosWordDamageTag   = 0x647275
	cosWordAbsorbTag   = 0x6f646172
	cosWordRecoveryTag = 0x69726763
)

/*
================
applyCosBuffWords

The friendly words at the same sink: a dru word's two flat words raise
the four damage-rate parameters (0x80/0x81 from word 0, 0x82/0x83 from
word 1, as the monster's dru arm writes them on its own slots), and an
odar word lowers each incoming-damage parameter its bit pairs select
(0xAE..0xB1, basic/skill crossed with physical/magical) by its word
percent on the percent-product channel - the pet's taken factor is
the keeper's product, replacing the empty factor and multiplying a
standing one, as the keeper's own read does.
================
*/
func applyCosBuffWords(stats *combat.Stats, words []monster.SelfEffect) {
	for _, word := range words {
		switch word.Tag {
		case cosWordDamageTag:
			stats.PhysicalBasicRate += float64(word.First)
			stats.PhysicalSkillRate += float64(word.First)
			stats.MagicalBasicRate += float64(word.Second)
			stats.MagicalSkillRate += float64(word.Second)
		case cosWordAbsorbTag:
			factor := float32(1) - float32(word.Second)/100
			for _, slot := range [...]struct {
				bits  uint32
				value *float32
			}{{4 | 1, &stats.PhysicalBasicTaken}, {4 | 2, &stats.PhysicalSkillTaken},
				{8 | 1, &stats.MagicalBasicTaken}, {8 | 2, &stats.MagicalSkillTaken}} {
				if word.First&slot.bits != slot.bits {
					continue
				}
				if *slot.value == 0 {
					*slot.value = factor
				} else {
					*slot.value *= factor
				}
			}
		}
	}
}
