/*
===========================================================================

strint_uniques.go - STR and INT counterparts of the field uniques

Port-only, not native. SRO_STR_INT_UNIQUES is off by default. Private
shards split a field unique into build-locked copies so a physical party
and a magical party can each hunt it; the original client and server have
no such split, so the whole feature ships behind one switch and every rule
below is tuning, not retail behaviour.

A variant is an ordinary unique reference cloned from its base: the same
model, level, hit points, country, unique grade and combat tail, with a
distinct RefObjID, codename and name. Loot parity is therefore a property
of the clone, not a second table: the unique reward prepass and the four
SRO_UNIQUE_* switches read the reference's grade, level and country, so a
variant pays exactly what its base pays.

The three builds live at the same time on the species' own spawn points,
each in a hive of its own. A species keeps every anchor, its hive limit
and its respawn timer, so all three can be up at once - three unique
encounters per species per timer instead of one. The population lets only
one live unique hold a spawn point at a time: a spawn draws among the
points no other copy stands on, so the copies never share ground without
taking an anchor away from anyone. The timer is untouched: whatever the
shard's respawn rule draws for the species, each build draws for itself.

The variant carries its own name ("Tiger Girl (STR)"), so the 0x3058
announcement takes the client's generic UNIC wording and prints the killer
(unique-notices.ts). It must never reuse one of the five name keys that
client hardcodes, or the notice would print the base name instead.

===========================================================================
*/
package monster

import (
	"fmt"
	"maps"
	"os"
	"strings"
)

// EnvStrIntUniques is the owner switch for the build-locked unique copies.
const EnvStrIntUniques = "SRO_STR_INT_UNIQUES"

// strIntRefBase is the first synthetic reference id. It sits above the
// training-dummy band (5000100+level, levels 1..140) and below nothing the
// shipped or optional populations allocate, so a collision can only mean a
// future feature took the band deliberately.
const strIntRefBase uint32 = 5001000

// strIntBuilds is how many builds share one species' anchors: the base and
// its two copies.
const strIntBuilds = 3

// The build a reference admits. The zero value is the native rule: any
// attacker may damage the unique.
const (
	UniqueBuildAny UniqueBuild = iota
	UniqueBuildSTR
	UniqueBuildINT
)

/*
================
UniqueBuild

The permanent allocation a build-locked unique variant admits.
================
*/
type UniqueBuild uint8

// strIntUniqueBases are the uniques that gain STR and INT copies, in the
// order their variants take reference ids. Every entry is a field unique
// whose whole lifecycle is a respawning outdoor nest; dungeon, tomb, Roc
// and summon-scroll rows keep their own schedules and are deliberately not
// split here.
var strIntUniqueBases = []string{
	"MOB_CH_TIGERWOMAN",
	"MOB_EU_KERBEROS",
	"MOB_AM_IVY",
	"MOB_OA_URUCHI",
	"MOB_KK_ISYUTARU",
	"MOB_TK_BONELORD",
	"MOB_RM_TAHOMET",
}

// strIntVariant is one copy of a base unique: its codename and name suffix,
// and the allocation it admits.
type strIntVariant struct {
	suffix string
	name   string
	build  UniqueBuild
}

// The two copies, in reference-id order. The base keeps its own.
var strIntVariants = []strIntVariant{
	{suffix: "_STR", name: "STR", build: UniqueBuildSTR},
	{suffix: "_INT", name: "INT", build: UniqueBuildINT},
}

/*
================
StrIntUniquesEnabled

Explicit "on", "1" or "true" admits the variants; anything else leaves the
native population exactly as the data shipped it.
================
*/
func StrIntUniquesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvStrIntUniques))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendStrIntUniques

Adds the build-locked copies and joins them to the base's spawn points. Off
is a no-op. On fails before startup when a base unique or one of its nests
is missing: a partial roster would publish notices for a unique nobody can
find, which is worse than refusing to boot.

Every build keeps the whole anchor list in a hive of its own, so all three
can be up at the same time. The shared points are marked (NestRow.
UniqueShare) and the population lets only one live unique hold a point at a
time: a spawn takes a point no other copy stands on, which keeps the copies
off each other's ground without taking any anchor away from the base.
================
*/
func AppendStrIntUniques(template Template, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	refs := maps.Clone(template.Refs)
	if refs == nil {
		refs = make(map[uint32]MonsterRef, len(template.Refs))
	}
	ids := make(map[string][strIntBuilds - 1]uint32, len(strIntUniqueBases))
	next := strIntRefBase
	for _, codename := range strIntUniqueBases {
		baseID, ok := refIDByCodename(template.Refs, codename)
		if !ok {
			return Template{}, fmt.Errorf("%s: unique %s is not in the installed template", EnvStrIntUniques, codename)
		}
		if len(nestsOfRef(template.Nests, baseID)) == 0 {
			return Template{}, fmt.Errorf("%s: unique %s has no nest to copy", EnvStrIntUniques, codename)
		}
		base := template.Refs[baseID]
		var variantIDs [strIntBuilds - 1]uint32
		for index, variant := range strIntVariants {
			id := next
			next++
			if existing, exists := refs[id]; exists {
				return Template{}, fmt.Errorf("%s: reference %d is already %s", EnvStrIntUniques, id, existing.Codename)
			}
			refs[id] = strIntRef(base, id, variant)
			variantIDs[index] = id
		}
		ids[codename] = variantIDs
	}

	nests := make([]NestRow, 0, len(template.Nests))
	for _, nest := range template.Nests {
		slot, shared := strIntSlotOf(template.Refs, nest.RefObjID)
		if !shared {
			nests = append(nests, nest)
			continue
		}
		// The base keeps every anchor; marking it makes the base take part in
		// the point exclusion as well, so a copy never stands on its ground.
		nest.UniqueShare = slot
		nests = append(nests, nest)
		for index, variant := range strIntVariants {
			clone := nest
			clone.RefObjID = ids[slot][index]
			// A hive of its own over the same anchors: each build keeps the
			// species' one-live-unique rule without sharing its slot.
			clone.HiveKey = nest.HiveKey + ":" + variant.suffix
			nests = append(nests, clone)
		}
	}

	result := TemplateFromParts(refs, nests)
	result.EvidenceMatches = template.EvidenceMatches
	result.SummonRefs = append([]uint32(nil), template.SummonRefs...)
	return result, nil
}

/*
================
strIntRef

The cloned reference. Everything the reward path and the client read stays
the base's, so a variant is the same monster under another identity; only
the identity, the display name and the build lock differ.
================
*/
func strIntRef(base MonsterRef, id uint32, variant strIntVariant) MonsterRef {
	ref := base
	ref.RefObjID = id
	ref.Codename = base.Codename + variant.suffix
	// A name key of its own keeps the notice off the client's five
	// hardcoded lines; the value never has to exist in the text table,
	// because the client reads the row's explicit name. The parenthesised
	// tag avoids the double bracket the generic notice would print around a
	// bracketed name ("[Tiger Girl [STR]]has appeared.").
	ref.NameStrID = base.NameStrID + variant.suffix
	ref.Name = base.DisplayName() + " (" + variant.name + ")"
	ref.VariantOf = base.Codename
	ref.UniqueBuild = variant.build
	return ref
}

/*
================
strIntSlotOf

The species a reference belongs to: its own codename when it is one of the
roster's shipped uniques, and empty for everything else.
================
*/
func strIntSlotOf(refs map[uint32]MonsterRef, id uint32) (string, bool) {
	ref, ok := refs[id]
	if !ok {
		return "", false
	}
	for _, codename := range strIntUniqueBases {
		if ref.Codename == codename {
			return codename, true
		}
	}
	return "", false
}

/*
================
refIDByCodename
================
*/
func refIDByCodename(refs map[uint32]MonsterRef, codename string) (uint32, bool) {
	for id, ref := range refs {
		if ref.Codename == codename {
			return id, true
		}
	}
	return 0, false
}

/*
================
nestsOfRef
================
*/
func nestsOfRef(nests []NestRow, refID uint32) []NestRow {
	var out []NestRow
	for _, nest := range nests {
		if nest.RefObjID == refID {
			out = append(out, nest)
		}
	}
	return out
}
