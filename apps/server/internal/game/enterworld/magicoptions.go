/*
===========================================================================

magicoptions.go - magic option definitions (magicoption.txt)

===========================================================================
*/

package enterworld

import (
	"path/filepath"
	"sort"
	"sync"

	log "github.com/sirupsen/logrus"
)

// The magic-option definition plane: the v1.150 magicoption.txt rows the
// client's CSOItem magic-option parse resolves per wire param (sub_78b1b0
// REINFORCE leg: the encoded u64's low u16 is a param id that MUST exist in
// the client's magicOptionDefsByParamId3e4 table - a miss throws "missing
// required magic-option definition"). The server therefore ships a
// magicOptionSnapshot next to the refItemSnapshot: the definition rows for
// every param id the enter payload's item bodies reference, so the client
// seeds its table from the same authority that authored the wire.
//
// Content discipline: param ids are v1.150-NATIVE magicoption.txt rows
// (extracted/Media_extracted/server_dep/silkroad/textdata/magicoption.txt,
// ~255 rows), resolved by MATTR_* CODENAME + degree - never transplanted
// from the v1.188 shard dump.

/*
==================
MagicOptionRow

MagicOptionRow is one magicOptionSnapshot row: exactly the
MagicOptionDefRecord fields the client's sub_78b1b0 reads - optionName
(the MATTR_* identity; equality with MATTR_REINFORCE_ITEM sets the +0x30
reinforce flag) and paramName (a '-' in it decrements the visible-option
count +0x85).
==================
*/
type MagicOptionRow struct {
	// Native body+0x4c..0x54, consumed by 563700/55b540.
	RangeWords *[3]uint32 `json:"rangeWords,omitempty"`
	ParamID    uint32     `json:"paramId"`
	OptionName string     `json:"optionName"`
	ParamName  string     `json:"paramName"`
	// Degree is the option's level column (magicoption.txt column 4);
	// snapshot metadata for humans reading the JSON, not a client input.
	Degree int64 `json:"degree"`
	// Tag is column 7, the packed ASCII code the server's option switch
	// (498690 / 496A70) dispatches on ('str', 'fz', 'hr', ...). Server-only.
	Tag uint32 `json:"-"`
}

/*
==================
MagicOptionSource

MagicOptionSource resolves magicoption.txt rows by param id. A nil source
behaves like a missing table: the snapshot stays empty and every emitted
option logs a loud resolve miss (the client then throws its own
missing-definition error - absence is loud on both sides, never silent).
==================
*/
type MagicOptionSource interface {
	MagicOptionByParamID(paramID uint32) (*MagicOptionRow, bool)
}

/*
==================
TextdataMagicOptions

TextdataMagicOptions is the MagicOptionSource over the extracted
magicoption.txt (lazy + cached, the TextdataItems posture: degrades to
empty when the textdata is absent so the server still boots).
==================
*/
type TextdataMagicOptions struct {
	dir string

	once      sync.Once
	byParamID map[uint32]*MagicOptionRow
}

// NewTextdataMagicOptions returns a lazy loader over dir (magicoption.txt).
/*
================
NewTextdataMagicOptions
================
*/
func NewTextdataMagicOptions(dir string) *TextdataMagicOptions {
	return &TextdataMagicOptions{dir: dir}
}

// MagicOptionByParamID implements MagicOptionSource.
/*
================
MagicOptionByParamID
================
*/
func (t *TextdataMagicOptions) MagicOptionByParamID(paramID uint32) (*MagicOptionRow, bool) {
	t.once.Do(t.load)
	row, ok := t.byParamID[paramID]
	return row, ok
}

/*
==================
MagicOptionByCodenameDegree

MagicOptionByCodenameDegree resolves a row by its MATTR_* codename +
degree pair - the content-pick door (tests and seeds name options the
way the media names them, never by raw id).
==================
*/
func (t *TextdataMagicOptions) MagicOptionByCodenameDegree(codename string, degree int64) (*MagicOptionRow, bool) {
	t.once.Do(t.load)
	for _, row := range t.byParamID {
		if row.OptionName == codename && row.Degree == degree {
			return row, true
		}
	}
	return nil, false
}

/*
==================
AllMagicOptions

AllMagicOptions publishes the bounded immutable reference catalogue once.
Future loot and degree-specific tooltip brackets need definitions even when
the character did not own an instance of that option at login.
==================
*/
func (t *TextdataMagicOptions) AllMagicOptions() []MagicOptionRow {
	t.once.Do(t.load)
	rows := make([]MagicOptionRow, 0, len(t.byParamID))
	for _, row := range t.byParamID {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ParamID < rows[j].ParamID })
	return rows
}

// Len reports how many magicoption rows loaded (0 = textdata absent).
/*
================
Len
================
*/
func (t *TextdataMagicOptions) Len() int {
	t.once.Do(t.load)
	return len(t.byParamID)
}

/*
==================
load

load parses magicoption.txt: column 0 service flag, column 1 the param id
(the u16 the wire's encoded low half carries), column 2 the MATTR_*
codename, column 3 the param-name string ("+" visible / "-@" hidden),
column 4 the degree.
==================
*/
func (t *TextdataMagicOptions) load() {
	t.byParamID = map[uint32]*MagicOptionRow{}
	rows := readTextdataFile(filepath.Join(t.dir, "magicoption.txt"))
	if len(rows) == 0 {
		log.Warnf("bootstrap: magicoption.txt not found under verified projection %s; magic-option snapshots will be empty", t.dir)
		return
	}
	for _, fields := range rows {
		if len(fields) < 5 {
			continue
		}
		service, ok := textdataInt(fields[0])
		if !ok || service != 1 {
			continue
		}
		paramID, ok := textdataInt(fields[1])
		if !ok || paramID <= 0 || paramID > 0xffff {
			continue
		}
		degree, ok := textdataInt(fields[4])
		if !ok {
			degree = 0
		}
		var words *[3]uint32
		if len(fields) >= 11 {
			var parsed [3]uint32
			valid := true
			for i := range parsed {
				n, ok := textdataInt(fields[8+i])
				if !ok || n < 0 || n > int64(^uint32(0)) {
					valid = false
					break
				}
				parsed[i] = uint32(n)
			}
			if valid {
				words = &parsed
			}
		}
		var tag uint32
		if len(fields) > 7 {
			if n, ok := textdataInt(fields[7]); ok && n >= 0 && n <= int64(^uint32(0)) {
				tag = uint32(n)
			}
		}
		t.byParamID[uint32(paramID)] = &MagicOptionRow{
			Tag:        tag,
			RangeWords: words,
			ParamID:    uint32(paramID),
			OptionName: fields[2],
			ParamName:  fields[3],
			Degree:     degree,
		}
	}
	log.Infof("bootstrap: magicoption.txt loaded from %s (%d row(s))", t.dir, len(t.byParamID))
}

/*
==================
EncodeMagicOption

EncodeMagicOption packs one reinforce-leg wire param: low u16 = the
magicoption.txt param id, high u32 = the modifier magnitude (the client
stores the high dword as the map value, sub_78b1b0 @0x0078b29f).
==================
*/
func EncodeMagicOption(paramID uint32, magnitude uint32) uint64 {
	return uint64(magnitude)<<32 | uint64(paramID&0xffff)
}

/*
==================
buildMagicOptionSnapshot

buildMagicOptionSnapshot collects the definition rows for every magic
option the character's emitted item bodies reference (worn + bag +
avatar rows). Deterministic order (ascending param id); an unresolvable
param id logs loud and ships no row - the client's own
missing-definition throw then names the same id.
==================
*/
func buildMagicOptionSnapshot(deps *Deps, character *Character) []MagicOptionRow {
	if character == nil {
		return nil
	}
	if catalog, ok := deps.MagicOptions.(interface{ AllMagicOptions() []MagicOptionRow }); ok {
		return catalog.AllMagicOptions()
	}
	referenced := map[uint32]bool{}
	if deps.ExtraMagicOptionIDs != nil {
		for _, id := range deps.ExtraMagicOptionIDs() {
			referenced[id] = true
		}
	}
	collect := func(rows []InventoryRow) {
		for _, row := range rows {
			for _, encoded := range row.MagicOptions {
				referenced[uint32(encoded&0xffff)] = true
			}
		}
	}
	collect(character.MissionInventory)
	for _, pet := range character.Companions() {
		if pet.Container != nil {
			collect(pet.Container.Rows)
		}
	}
	if character.AvatarInventory != nil {
		collect(character.AvatarInventory.Rows)
	}
	if len(referenced) == 0 {
		return nil
	}
	ids := make([]uint32, 0, len(referenced))
	for id := range referenced {
		ids = append(ids, id)
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	snapshot := make([]MagicOptionRow, 0, len(ids))
	for _, id := range ids {
		if deps.MagicOptions == nil {
			log.Warnf("bootstrap: magic-option param id %d referenced with no MagicOptionSource wired; the client will throw its missing-definition error", id)
			continue
		}
		row, ok := deps.MagicOptions.MagicOptionByParamID(id)
		if !ok || row == nil {
			log.Warnf("bootstrap: magic-option param id %d does not resolve in magicoption.txt; the client will throw its missing-definition error", id)
			continue
		}
		snapshot = append(snapshot, *row)
	}
	return snapshot
}

/*
==================
AvatarMagicOptionRow

One avatar part's grantable options: the TID4 (1 hat, 2 dress,
3 attachment) and the codenames of its magicoptionassign.txt row. The
grant window (CSOItem_FillAvatarMagicOptionCandidates 78C720) lists
these, resolved through the magicOptionSnapshot definitions.
==================
*/
type AvatarMagicOptionRow struct {
	Part    uint8    `json:"part"`
	Options []string `json:"options"`
}

/*
==================
buildAvatarMagicOptions
==================
*/
func buildAvatarMagicOptions(deps *Deps) []AvatarMagicOptionRow {
	if deps.AvatarMagicOptions == nil {
		return nil
	}
	return deps.AvatarMagicOptions()
}
