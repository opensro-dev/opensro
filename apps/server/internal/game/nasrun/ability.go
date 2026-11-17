/*
===========================================================================

ability.go - what a worn, awake Devil's Spirit grants at each plus

Port-only, not native to v1.150 (#3456): the rule is the later official
server's. Its _RefAbilityByItemOptLevel ties each Spirit codename and plus
(0..10) to a set of skills (_RefSkillByItemOptLevel) and of magic options
(_RefMagicOptByItemOptLevel). The iSRO-R shard (2025-07) authors every
colour completely, and its rows reduce to one rule:

	plus 0..2   the colour's Eccentric Demon
	plus 3..5   the colour's Devil's Confusion and Monster Defence Ignore 1
	plus 6..10  the colour's Destroy the Devil and Monster Defence Ignore 2
	HP and MP   +15 % + plus (the Unique: +20 % + plus), MATTR_NASRUN_HPNA/MPNA
	block       0, 1, 2 by the same tiers (the Unique: 1, 2, 3), MATTR_NASRUN_BLOCKRATE

The Unique grants the red line's skills. The v1.188 shard export has
another boundary (its top tier starts at +5) and smaller HP/MP numbers;
it is a private-server set, so the official shard wins. The rule lives here
as code rather than as the retail tables, which never enter the tree.

How the v1.188 server applies it: its item option pass writes hpna to
parameter 0x34 and mpna to 0x35 in percent-sum mode, and brna to 0x0A
flat. The granted skills are active ones (its reader of the skill table
refuses a passive), and each carries reqi 14 1: the equipment check's
avatar-socket case casts it only while avatar slot 4 holds an item that
has not expired.

===========================================================================
*/
package nasrun

import "strings"

const (
	// MaxAbilityPlus is the last plus the reference table authors; a
	// higher plus has no row and grants nothing.
	MaxAbilityPlus = 10

	// The tier boundaries (iSRO-R _RefAbilityByItemOptLevel).
	midTierPlus = 3
	topTierPlus = 6

	// The HP/MP percent at +0, and the Unique's.
	baseGaugePercent   = 15
	uniqueGaugePercent = 20

	// The Unique's block runs one tier ahead of the colours'.
	uniqueBlockBonus = 1

	spiritCodenamePrefixM = "ITEM_MALL_AVATAR_M_NASRUN"
	spiritCodenamePrefixW = "ITEM_MALL_AVATAR_W_NASRUN"
	skillCodenamePrefix   = "SKILL_MALL_NASRUN_"
)

/*
================
Ability

What one Spirit at one plus grants: the skill codenames it lets its wearer
cast, the percent its wearer's HP and MP grow by, and its flat block rate.
================
*/
type Ability struct {
	Skills    []string
	HPPercent uint32
	MPPercent uint32
	Block     uint32
}

/*
================
SpiritAbility

The ability of the Spirit with this codename at this plus. ok is false for
a codename that is not one of the mall Spirits the import stages
(nasrun_import.py ROWS) and for a plus outside 0..MaxAbilityPlus.
================
*/
func SpiritAbility(codename string, plus int64) (Ability, bool) {
	if plus < 0 || plus > MaxAbilityPlus {
		return Ability{}, false
	}
	rest, found := strings.CutPrefix(codename, spiritCodenamePrefixM)
	if !found {
		rest, found = strings.CutPrefix(codename, spiritCodenamePrefixW)
	}
	if !found {
		return Ability{}, false
	}
	// colour is the skill codename's suffix; the red line has none.
	colour, unique := "", false
	switch rest {
	case "":
	case "_YELLOW", "_BLUE":
		colour = rest
	case "_UNIQUE":
		unique = true
	default:
		return Ability{}, false
	}
	tier := uint32(0)
	switch {
	case plus >= topTierPlus:
		tier = 2
	case plus >= midTierPlus:
		tier = 1
	}
	var skills []string
	switch tier {
	case 0:
		skills = []string{skillCodenamePrefix + "ECCENTRICDEMON" + colour + "_01"}
	case 1:
		skills = []string{skillCodenamePrefix + "THEDEVILCONFUSION" + colour + "_01", skillCodenamePrefix + "MOBDEFENCEIGNORE_01"}
	default:
		skills = []string{skillCodenamePrefix + "DESTROYTHEDEVIL" + colour + "_01", skillCodenamePrefix + "MOBDEFENCEIGNORE_02"}
	}
	gauge, block := uint32(baseGaugePercent), tier
	if unique {
		gauge, block = uniqueGaugePercent, tier+uniqueBlockBonus
	}
	gauge += uint32(plus)
	return Ability{Skills: skills, HPPercent: gauge, MPPercent: gauge, Block: block}, true
}

/*
================
AbilitySkills

Every skill codename some Spirit can grant, for the import and its checks.
================
*/
func AbilitySkills() []string {
	var out []string
	for _, colour := range []string{"", "_YELLOW", "_BLUE"} {
		for _, name := range []string{"ECCENTRICDEMON", "THEDEVILCONFUSION", "DESTROYTHEDEVIL"} {
			out = append(out, skillCodenamePrefix+name+colour+"_01")
		}
	}
	return append(out, skillCodenamePrefix+"MOBDEFENCEIGNORE_01", skillCodenamePrefix+"MOBDEFENCEIGNORE_02")
}
