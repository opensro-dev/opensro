/*
===========================================================================

setgroups.go - the dormant degree-10/11 equipment set catalogue

Owns the refsetitemgroup -> refmagicoptgroup chain the cap-110 import
(#1791) writes into the overlay textdata, plus the SRO_CAP110_SET_BONUSES
gate. Port-only, not native: the v1.150 client ships no degree-10/11 set
rows, so this data exists only inside the port-only overlay and the flag
ships off. While the flag is off the combat owner is handed a nil source
(Deps.SetCatalogue) and no set logic runs at all.

===========================================================================
*/

package enterworld

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

// EnvCap110SetBonuses gates the degree-10/11 equipment set bonuses.
// Port-only, not native; the off value is the native behaviour because
// the v1.150 client has no set rows at all.
const EnvCap110SetBonuses = "SRO_CAP110_SET_BONUSES"

// The two set magic options, later-client magicoption param ids carried
// by the refmagicoptgroup rows; the #1791 import validated both against
// the later magicoption table (389 MATTR_REINFORCE_ITEM_SET, 390
// MATTR_RESIST_ALL_SET).
const (
	SetOptionReinforceItem = 389
	SetOptionResistAll     = 390
)

// setMagicGroupPrefix selects the dormant degree-11 set rows of
// refmagicoptgroup.txt; every other magic group keeps its native owner.
const setMagicGroupPrefix = "MAGIC_11_SET_"

// refsetitemgroup link cells pair a family figure with a magic group id
// at (7,8), (9,10), (11,12), (13,14), (15,16); a zero group cell is an
// empty slot. The figure is
// a family code (21 armour, 5 accessory), never a piece count: the tier
// requirement is the trailing _2/_4/_6 of the magic group's codename.
const (
	setLinkFirstGroupCell = 8
	setLinkLastGroupCell  = 16
	setLinkCellStride     = 2
)

// The tier requirement is a worn-piece count; no set can ask for more
// pieces than there are equipment slots.
const (
	setTierMinPieces = 2
	setTierMaxPieces = 13
)

/*
================
setGroupIDOf

setGroupIDOf reads an itemdata row's set group: column 62
(RefItem+0x1ac), zero in every v1.150 row and authored only by the
cap-110 overlay. Short rows and malformed cells read as no set.
================
*/
func setGroupIDOf(fields []string) int64 {
	if len(fields) <= 62 {
		return 0
	}
	value, _ := textdataInt(fields[62])
	return value
}

/*
================
SetBonusesEnabled

SetBonusesEnabled reports the port-only SRO_CAP110_SET_BONUSES flag.
================
*/
func SetBonusesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvCap110SetBonuses))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
SetTier

SetTier is one piece-count threshold of a set group: wearing at least
RequiredCount pieces of the group applies MagicGroupID's options.
================
*/
type SetTier struct {
	RequiredCount int
	MagicGroupID  int
}

/*
================
SetOption

SetOption is one option of a magic group: a magicoption param id and
the value its refmagicoptgroup row carries.
================
*/
type SetOption struct {
	OptionID int
	Value    int
}

/*
================
SetCatalogueSource

SetCatalogueSource resolves the dormant set chain: the tiers of a set
group and the options of a magic group. A nil source is the native
posture - no set ever applies.
================
*/
type SetCatalogueSource interface {
	SetGroupTiers(setID int64) []SetTier
	MagicGroupOptions(magicGroupID int) []SetOption
}

/*
================
TextdataSetGroups

TextdataSetGroups is the SetCatalogueSource over the overlay
refsetitemgroup.txt and refmagicoptgroup.txt (lazy + cached, the
TextdataMagicOptions posture). Both files are port-only overlay output,
so absence degrades to empty rather than failing the boot.
================
*/
type TextdataSetGroups struct {
	dir string

	once    sync.Once
	tiers   map[int64][]SetTier
	options map[int][]SetOption
}

/*
================
NewTextdataSetGroups
================
*/
func NewTextdataSetGroups(dir string) *TextdataSetGroups {
	return &TextdataSetGroups{dir: dir}
}

/*
================
SetGroupTiers

SetGroupTiers answers the group's tiers ascending by required count.
Absent groups answer empty.
================
*/
func (t *TextdataSetGroups) SetGroupTiers(setID int64) []SetTier {
	t.once.Do(t.load)
	return t.tiers[setID]
}

/*
================
MagicGroupOptions

MagicGroupOptions answers one magic group's options in row order.
Absent groups answer empty.
================
*/
func (t *TextdataSetGroups) MagicGroupOptions(magicGroupID int) []SetOption {
	t.once.Do(t.load)
	return t.options[magicGroupID]
}

/*
================
Len

Len counts the loaded set groups; tests and boot logs read it, the
runtime never gates on it.
================
*/
func (t *TextdataSetGroups) Len() int {
	t.once.Do(t.load)
	return len(t.tiers)
}

/*
================
load

load parses refmagicoptgroup.txt first (only the dormant degree-11 set
rows: column 0 service flag, column 1 magic group id, column 3 the
codename, column 4 the option id, column 5 its value), then the
refsetitemgroup.txt links (column 1 the group id, link group cells at
8/10/12/14/16). Links naming an unknown magic group are dropped - the
build-time import validates the whole chain, so a dropped link here
means the overlay changed after the build and the tier simply never
applies, never a wrong bonus.
================
*/
func (t *TextdataSetGroups) load() {
	t.tiers = map[int64][]SetTier{}
	t.options = map[int][]SetOption{}
	codenames := map[int]string{}

	magicRows := readTextdataFile(filepath.Join(t.dir, "refmagicoptgroup.txt"))
	if len(magicRows) == 0 {
		log.Warnf("bootstrap: refmagicoptgroup.txt not found under %s; equipment sets will not apply", t.dir)
		return
	}
	for _, fields := range magicRows {
		if len(fields) < 6 {
			continue
		}
		if service, ok := textdataInt(fields[0]); !ok || service != 1 {
			continue
		}
		codename := strings.TrimSpace(fields[3])
		if !strings.HasPrefix(codename, setMagicGroupPrefix) {
			continue
		}
		group, ok := textdataInt(fields[1])
		if !ok || group <= 0 {
			continue
		}
		option, ok := textdataInt(fields[4])
		if !ok || option <= 0 {
			continue
		}
		value, ok := textdataInt(fields[5])
		if !ok {
			continue
		}
		codenames[int(group)] = codename
		row := SetOption{OptionID: int(option), Value: int(value)}
		rows := t.options[int(group)]
		replaced := false
		for i := range rows {
			if rows[i].OptionID == row.OptionID {
				rows[i] = row
				replaced = true
				break
			}
		}
		if !replaced {
			t.options[int(group)] = append(rows, row)
		}
	}

	groupRows := readTextdataFile(filepath.Join(t.dir, "refsetitemgroup.txt"))
	if len(groupRows) == 0 {
		log.Warnf("bootstrap: refsetitemgroup.txt not found under %s; equipment sets will not apply", t.dir)
		return
	}
	for _, fields := range groupRows {
		if len(fields) <= setLinkLastGroupCell {
			continue
		}
		if service, ok := textdataInt(fields[0]); !ok || service != 1 {
			continue
		}
		group, ok := textdataInt(fields[1])
		if !ok || group <= 0 {
			continue
		}
		var tiers []SetTier
		for cell := setLinkFirstGroupCell; cell <= setLinkLastGroupCell; cell += setLinkCellStride {
			magicGroup, ok := textdataInt(fields[cell])
			if !ok || magicGroup <= 0 {
				continue
			}
			codename, known := codenames[int(magicGroup)]
			if !known {
				continue
			}
			required, ok := setTierRequirement(codename)
			if !ok {
				continue
			}
			tier := SetTier{RequiredCount: required, MagicGroupID: int(magicGroup)}
			duplicate := false
			for _, existing := range tiers {
				if existing.RequiredCount == required {
					duplicate = true
					break
				}
			}
			if !duplicate {
				tiers = append(tiers, tier)
			}
		}
		if len(tiers) == 0 {
			continue
		}
		sort.Slice(tiers, func(i, j int) bool {
			return tiers[i].RequiredCount < tiers[j].RequiredCount
		})
		t.tiers[group] = tiers
	}
	log.Infof("bootstrap: equipment set catalogue loaded from %s (%d group(s), %d magic group(s))", t.dir, len(t.tiers), len(t.options))
}

/*
================
setTierRequirement

setTierRequirement reads the worn-piece requirement off a magic group
codename (MAGIC_11_SET_A_DEFENSE_6 -> 6). A codename without a sane
trailing count names no tier.
================
*/
func setTierRequirement(codename string) (int, bool) {
	cut := strings.LastIndex(codename, "_")
	if cut < 0 || cut == len(codename)-1 {
		return 0, false
	}
	required, ok := textdataInt(codename[cut+1:])
	if !ok || required < setTierMinPieces || required > setTierMaxPieces {
		return 0, false
	}
	return int(required), true
}
