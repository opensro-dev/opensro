/*
===========================================================================

textdata.go - loading monster references from characterdata

Package monster owns immutable monster catalogs and behavior policy:
characterdata classification, npcpos/Nest population inputs, tactics, and
the pure mover transition model. Mutable division populations, identities,
HP, respawns, and mover commits live exclusively in simulation.MonsterState.

ROLE: the v1.150 client parses npcpos into a map that nothing reads back;
it is reference data, not a native spawn producer. The server population
keeps those v1.150 codenames and anchors, then enriches exact natural-key
matches with Nest/Hive/Tactics evidence recovered from the v1.188 shard
backup. Numeric ids never cross the version boundary.

===========================================================================
*/
package monster

import (
	"encoding/binary"
	"math"
	"opensro.online/server/internal/data/texttable"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

/*
==================
MonsterRef

MonsterRef is one characterdata row classified as a monster: the
reference identity plus the per-monster stats the create row and the
wander mover need.
==================
*/
type MonsterRef struct {
	RefObjID uint32
	// TidWord is the packed RefObj TypeID word the client's sub_851420
	// cascade classifies on. ARBITER-CLOSED (RZ seq42 gate + seq234
	// column ruling): the binary selects CLASS_CICMONSTER iff
	// (W & 0x7FE) == 0x0C6, W = word[RefObj+0] via
	// CGlobalDataManager::GetTIDFromObjectID (sub_7efef0). Every row this
	// loader classifies as a monster packs to exactly 0x00C6 (bit0 and
	// bits11-15 are never set by the packer), the canonical value for
	// create-row/refObjSnapshot emission.
	TidWord uint16
	// Structure is a fortress structure (structureTidGate).
	Structure bool
	// TypeID4 is characterdata column 12. The client classification gates
	// ignore it, but the GameServer spawn routine reads the full TypeID word:
	// monster TID4 4 (the MOB_QT_* quest monsters) never becomes a party
	// monster (5607B0 -> 561020). NativeTypeWord packs it into bits 11-15.
	TypeID4  uint8
	Codename string
	// Own characterdata OrgObjCodeName128, used by native default-tactics fallback.
	OriginalCodename string
	// NameStrID is characterdata+column 5 (the SN_MOB_* localization key);
	// Name is its English textdataname column-8 value. Native
	// CICharactor_DeserializeNameInfo stores the spawn row's explicit name
	// at CIGIDObject+0x108, while target-window fallback resolves the same
	// NameStrID from RefObjData+0x60. Keep both lanes sourced from the same
	// retail data rather than exposing the internal MOB_* codename as UI.
	NameStrID string
	Name      string
	// ModelPath / Level / MaxHP are the characterdata RefObj fields consumed
	// by the browser's +0x11c/+0x1a8/+0x1b0 mirrors. They are snapshot
	// metadata, not values inferred from a spawned instance.
	ModelPath string
	// RideModelPath / RiderTransformMode are the skilleffect.txt
	// #section characterInfo columns "ride" and "Ride Type". Native
	// SkillEffectCharacterInfo_ParseRow stores them at action-effect context
	// +0x24/+0x18; CICMonster_DeserializeSpawn constructs a linked CICRide
	// whenever +0x24 is non-null. This is a second visual resource, not a
	// characterdata RefObj link (Tiger Girl is tigerwoman.bsr riding
	// bluetiger.bsr).
	RideModelPath      string
	RiderTransformMode uint8
	Level              uint8
	MaxHP              uint32
	// Country is RefObjCommon.Country (characterdata column 14): 0 China,
	// 1 Europe, 2 Islam, 3 random China/Europe. The native drop-reference builder reads this from
	// the defeated monster, not from the killer, to choose its country bucket.
	// Keeping it on the immutable monster ref prevents loot generation from
	// guessing by codename or leaking the player's race across the boundary.
	Country uint8
	// The eight RefObjChar combat parameters are columns 77..84 in the
	// shipped v1.150 characterdata. The v1.188 producer sub_4c3730 writes
	// the homologous fields to ParamKeeper 5..12 in exactly this order.
	// CombatPinned distinguishes a real all-column snapshot from a
	// synthetic/admin ref that omitted the combat tail.
	CombatPinned    bool
	PhysicalDefense float64
	MagicalDefense  float64
	ParryRate       float64
	MagicalParry    float64
	EvasionRate     float64
	BlockRate       float64
	HitRate         float64
	CriticalRate    float64
	// ElementResist is characterdata columns 61..66 (RefObjChar +1A4..+1B8:
	// frozen, frostbite, burn, electric shock, poison, zombie). 4CEFE0 stores
	// them as parameters 1B, 1C, 1E, 1D, 1F and 20; the abnormal roll reads
	// them in its own order (freeze, frostbite, shock, burn, poison, zombie).
	ElementResist [6]uint8
	// Reward/default-action tail, from the same shipped v1.150
	// characterdata row. ExpToGive is column 85. Columns 89..98 are the ten
	// default skill ids the retail GameServer chooses from for autonomous
	// attacks. RewardActionPinned is all-or-none so combat never silently
	// manufactures a reward or animation skill when a row is truncated.
	RewardActionPinned bool
	ExpToGive          uint32
	CreepType          uint32
	Knockdown          uint32
	KORecoverMs        uint32
	DefaultSkillIDs    [10]uint32
	// MonsterType is characterdata column 15, the static RefObjChar
	// monster-type byte. v1.150 contains 5,967 normal (0) and 19 unique
	// (3) rows. The v1.188 GameServer reads the homologous RefObj byte
	// before applying per-instance Nest/Hive promotion.
	MonsterType uint8
	// WalkSpeed / RunSpeed are the per-monster +0x24c/+0x250 sources.
	// Columns 46/47 are identified by the v1.150 shipped rows (1933 =
	// 8/22, immobile NPC 7495 = 0/0) and cross-checked against the labelled
	// v1.188 server data.
	WalkSpeed float64
	RunSpeed  float64
	// ScaleDenom is the third scalar consumed by sub_85fb20 (column 48).
	// It varies across the shipped roster and must remain per monster.
	ScaleDenom float64
	// BodyRadius is RefObjChar BCRadius (column 50, record+0xEE). The retail
	// GameServer reads it through actor vtable+0x560 and combines both actors'
	// radii with action reach before approach/range admission.
	BodyRadius float64
}

// characterdata column indices (0-based). Column->bit map ARBITER-CLOSED
// (RZ seq234, retracting seq42; the A8 dispute resolved in WIP seq99 /
// SRV seq26's favour): col 8 = char/bionic bit; cols 9/10/11 feed the
// 0x1C / 0x60 / 0x780 gate bands; col 11 is the monster(1)/NPC(2)
// discriminator, UNIQUELY forced by the two binary gates (monster
// @0x851484 wants 0x080, NPC @0x808a9b wants 0x100) crossed with the two
// shipped rows. col 12 (TID4) is NOT consumed by the classification
// gates.
const (
	colService       = 0
	colRefObjID      = 1
	colCodename      = 2
	colNameStrID     = 5
	colCharBit       = 8
	colTid1          = 9
	colTid2          = 10
	colTid3          = 11
	colTid4          = 12
	colCountry       = 14
	colMonsterType   = 15
	colWalk          = 46
	colRun           = 47
	colScale         = 48
	colBodyRadius    = 50
	colModel         = 52
	colLevel         = 57
	colResistFrozen  = 61
	colMaxHP         = 59
	colPhysicalDef   = 77
	colMagicalDef    = 78
	colParryRate     = 79
	colMagicalParry  = 80
	colEvasionRate   = 81
	colBlockRate     = 82
	colHitRate       = 83
	colCriticalRate  = 84
	colExpToGive     = 85
	colCreepType     = 86
	colKnockdown     = 87
	colKORecoverMs   = 88
	colDefaultSkill1 = 89
	colDefaultSkillN = 98
)

/*
==================
tidWordFromColumns

tidWordFromColumns packs the characterdata TypeID columns into the
RefObj TypeID word W. CITED-RZ-seq234 (arbiter ruling, triangulated: 2
binary gates x 2 shipped rows = a unique forcing):

	W = 0x02[charBit col8] | (TID1 col9)<<2 | (TID2 col10)<<5 | (TID3 col11)<<7
	MANGNYANG (1,1,2,1) -> 0x00C6 (monster); SMITH (1,1,2,2) -> 0x0146 (NPC).

==================
*/
func tidWordFromColumns(charBit, tid1, tid2, tid3 uint16) uint16 {
	word := (tid1&7)<<2 | (tid2&3)<<5 | (tid3&0xF)<<7
	if charBit != 0 {
		word |= 0x0002
	}
	return word
}

/*
==================
monsterTidGate

monsterTidGate is the exact sub_851420 CLASS_CICMONSTER selection,
binary-proven and arbiter-confirmed (RZ seq42 gate + seq234 re-read):
four cmp gates @0x851460/0x85146b/0x851475/0x851484 combine to
(W & 0x7FE) == 0x0C6 (bit0 and bits11-15 are don't-care). Over the
confirmed packing this admits rows with (TID1=1, TID2=2, TID3=1) - the
admit-set RZ seq234 names as binding for the SRV filter.

KNOWN BAND MAP on the bits7-10 discriminator axis (A16, board
seq284/287/306 - the taxonomy is NOT complete, do not assume other
bands are empty): TID3=1 monster 0x00C6 (5,986 shipped rows) /
TID3=2 NPC 0x0146 / TID3=3 -> 0x01C6 (3,335 rows, COS-majority by
codename, class UNPINNED - no binary pin classifies them; they are
inventory, not admissible). Admission is by THIS gate only - never by
codename prefix (four MOB_*_COS rows live in the 0x01C6 band).
==================
*/
func monsterTidGate(w uint16) bool {
	return w&0x7FE == 0x00C6
}

/*
==================
structureTidGate

The fortress structures' band: TID1=1, TID2=2, TID3=5 packs to 0x02C6, the
word RefObjTypeFlags_IsATStruct (4F3A50) selects CLASS_CICATStruct by.
v1.188 keeps them as CGObjChar subclasses beside the monsters, so they
share the population owner, its targeting and its damage; Structure marks
the rows whose spawn row, behavior and damage rules differ.
==================
*/
func structureTidGate(w uint16) bool {
	return w&0x7FE == 0x02C6
}

/*
==================
LoadMonsterRefs

LoadMonsterRefs scans every characterdata*.txt under textdataDir and
returns the rows whose packed TypeID word passes the binary monster
gate, keyed by RefObjID. Rows with service flag 0 are skipped (disabled
content). Missing directory or unreadable files degrade to an empty map
- simulation state then has no catalog and emission stays empty, never a
fault (the TextdataItems loader posture).
==================
*/
func LoadMonsterRefs(textdataDir string) map[uint32]MonsterRef {
	refs := make(map[uint32]MonsterRef)
	rideMetadata := loadMonsterRideMetadata(textdataDir)
	// textdataname.txt is the shipped symbol table: fields[1] is the
	// SN_* symbol and fields[8] is English. This is the same column contract
	// used by bootstrap's item loader; keeping the tiny reader local avoids
	// a monster -> bootstrap package cycle.
	names := make(map[string]string)
	for _, cols := range readTabbedFile(filepath.Join(textdataDir, "textdataname.txt")) {
		if len(cols) < 9 {
			continue
		}
		symbol := strings.TrimSpace(cols[1])
		english := strings.TrimSpace(cols[8])
		if strings.HasPrefix(symbol, "SN_") && english != "" {
			names[symbol] = english
		}
	}
	matches, err := filepath.Glob(filepath.Join(textdataDir, "characterdata*.txt"))
	if err != nil {
		return refs
	}
	sort.Strings(matches)
	for _, path := range matches {
		for _, cols := range readTabbedFile(path) {
			if len(cols) <= colMaxHP {
				continue
			}
			if strings.TrimSpace(cols[colService]) != "1" {
				continue
			}
			charBit, ok0 := columnUint(cols, colCharBit)
			tid1, ok1 := columnUint(cols, colTid1)
			tid2, ok2 := columnUint(cols, colTid2)
			tid3, ok3 := columnUint(cols, colTid3)
			tid4, ok4 := columnUint(cols, colTid4)
			if !ok0 || !ok1 || !ok2 || !ok3 || !ok4 || tid4 > 0x1f {
				continue
			}
			word := tidWordFromColumns(charBit, tid1, tid2, tid3)
			structure := structureTidGate(word)
			if !monsterTidGate(word) && !structure {
				continue
			}
			refObjID, ok := columnUint32(cols, colRefObjID)
			if !ok {
				continue
			}
			walk, walkOK := columnFloat(cols, colWalk)
			run, runOK := columnFloat(cols, colRun)
			scale, scaleOK := columnFloat(cols, colScale)
			bodyRadius, bodyRadiusOK := columnFloat(cols, colBodyRadius)
			if !walkOK || !runOK || !scaleOK || !bodyRadiusOK ||
				math.IsNaN(walk) || math.IsInf(walk, 0) || walk < 0 ||
				math.IsNaN(run) || math.IsInf(run, 0) || run < 0 ||
				math.IsNaN(scale) || math.IsInf(scale, 0) || scale <= 0 ||
				math.IsNaN(bodyRadius) || math.IsInf(bodyRadius, 0) || bodyRadius < 0 ||
				// A structure's body is its model's collision (its record
				// radius is zero); every character has a radius.
				bodyRadius == 0 && !structure {
				continue
			}
			level, levelOK := columnUint(cols, colLevel)
			maxHP, maxHPOK := columnUint32(cols, colMaxHP)
			country, countryOK := columnUint(cols, colCountry)
			monsterType, monsterTypeOK := columnUint(cols, colMonsterType)
			if !levelOK || level > 255 || !maxHPOK || maxHP == 0 ||
				!countryOK || country > 3 ||
				!monsterTypeOK || monsterType > 0x0f {
				continue
			}
			parameters := CharacterParameters(cols)
			var expToGive, creepType, knockdown, koRecoverMs uint32
			var defaultSkillIDs [10]uint32
			rewardActionPinned := len(cols) > colDefaultSkillN
			if rewardActionPinned {
				var ok bool
				expToGive, ok = columnUint32(cols, colExpToGive)
				rewardActionPinned = rewardActionPinned && ok
				creepType, ok = columnUint32(cols, colCreepType)
				rewardActionPinned = rewardActionPinned && ok
				knockdown, ok = columnUint32(cols, colKnockdown)
				rewardActionPinned = rewardActionPinned && ok
				koRecoverMs, ok = columnUint32(cols, colKORecoverMs)
				rewardActionPinned = rewardActionPinned && ok
				defaultSkillIDs, ok = characterDefaultSkills(cols)
				rewardActionPinned = rewardActionPinned && ok
			}
			nameStrID := strings.TrimSpace(cols[colNameStrID])
			displayName := names[nameStrID]
			if displayName == "" {
				// A corrupt/incomplete localization table must not produce an
				// empty length-prefixed spawn name (which suppresses the
				// native name board). Codename is a visible degradation and
				// preserves packet alignment; it is not the normal UI source.
				displayName = strings.TrimSpace(cols[colCodename])
			}
			codename := strings.TrimSpace(cols[colCodename])
			ride := rideMetadata[codename]
			refs[refObjID] = MonsterRef{
				RefObjID:           refObjID,
				TidWord:            word,
				Structure:          structure,
				TypeID4:            uint8(tid4),
				Codename:           codename,
				OriginalCodename:   strings.TrimSpace(cols[4]),
				NameStrID:          nameStrID,
				Name:               displayName,
				ModelPath:          strings.TrimSpace(cols[colModel]),
				RideModelPath:      ride.modelPath,
				RiderTransformMode: ride.transformMode,
				Level:              uint8(level),
				MaxHP:              maxHP,
				Country:            uint8(country),
				CombatPinned:       parameters.CombatPinned,
				PhysicalDefense:    parameters.PhysicalDefense,
				MagicalDefense:     parameters.MagicalDefense,
				ParryRate:          parameters.ParryRate,
				MagicalParry:       parameters.MagicalParry,
				EvasionRate:        parameters.EvasionRate,
				BlockRate:          parameters.BlockRate,
				HitRate:            parameters.HitRate,
				CriticalRate:       parameters.CriticalRate,
				ElementResist:      parameters.ElementResist,
				RewardActionPinned: rewardActionPinned,
				ExpToGive:          expToGive,
				CreepType:          creepType,
				Knockdown:          knockdown,
				KORecoverMs:        koRecoverMs,
				DefaultSkillIDs:    defaultSkillIDs,
				MonsterType:        uint8(monsterType),
				WalkSpeed:          walk,
				RunSpeed:           run,
				ScaleDenom:         scale,
				BodyRadius:         bodyRadius,
			}
		}
	}
	return refs
}

/*
================
monsterRideMetadata
================
*/
type monsterRideMetadata struct {
	modelPath     string
	transformMode uint8
}

/*
==================
loadMonsterRideMetadata

loadMonsterRideMetadata projects the complete native ride contract from
skilleffect.txt. The enum values are executable-authored (the static table
consumed by sub_916700 at 0xf091fc): none=0, RT_FIXED=1, RT_DUMMY=2.
Unknown tokens are rejected rather than silently inventing a transform.
==================
*/
func loadMonsterRideMetadata(textdataDir string) map[string]monsterRideMetadata {
	result := make(map[string]monsterRideMetadata)
	inCharacterInfo := false
	for _, cols := range readTabbedFile(filepath.Join(textdataDir, "skilleffect.txt")) {
		if len(cols) == 0 {
			continue
		}
		first := strings.TrimSpace(cols[0])
		if section, ok := textdataSection(cols); ok {
			inCharacterInfo = strings.EqualFold(section, "characterInfo")
			continue
		}
		if !inCharacterInfo || len(cols) < 5 {
			continue
		}
		codename := first
		ridePath := strings.TrimSpace(cols[4])
		if codename == "" || ridePath == "" || strings.EqualFold(ridePath, "none") {
			continue
		}
		var mode uint8
		switch strings.ToUpper(strings.TrimSpace(cols[3])) {
		case "NONE":
			mode = 0
		case "RT_FIXED":
			mode = 1
		case "RT_DUMMY":
			mode = 2
		default:
			continue
		}
		result[codename] = monsterRideMetadata{modelPath: ridePath, transformMode: mode}
	}
	return result
}

/*
==================
textdataSection

textdataSection recognizes both section spellings shipped by the media:
"#section name" and the tabular "#section<TAB>name" form. Callers have
already split the record into columns, so inspecting cols[0] alone loses
the name in the latter (the form used by the retail skilleffect.txt).
==================
*/
func textdataSection(cols []string) (string, bool) {
	fields := strings.Fields(strings.Join(cols, "\t"))
	if len(fields) == 0 || !strings.EqualFold(fields[0], "#section") {
		return "", false
	}
	if len(fields) < 2 {
		return "", true
	}
	return fields[1], true
}

/*
==================
DisplayName

DisplayName returns the explicit retail name carried by monster spawn
rows. Synthetic tests and administratively injected refs may omit Name;
codename is the last-resort nonempty fallback for those rows.
==================
*/
func (ref MonsterRef) DisplayName() string {
	if name := strings.TrimSpace(ref.Name); name != "" {
		return name
	}
	return strings.TrimSpace(ref.Codename)
}

/*
================
columnUint
================
*/
func columnUint(cols []string, index int) (uint16, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(cols[index]), 10, 16)
	if err != nil {
		return 0, false
	}
	return uint16(v), true
}

/*
================
columnUint32
================
*/
func columnUint32(cols []string, index int) (uint32, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(cols[index]), 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

/*
================
columnFloat
================
*/
func columnFloat(cols []string, index int) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(cols[index]), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

/*
================
nonNegativeColumnFloat
================
*/
func nonNegativeColumnFloat(cols []string, index int) (float64, bool) {
	value, ok := columnFloat(cols, index)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	return value, true
}

/*
==================
readTabbedFile

readTabbedFile reads one tab-separated textdata file, tolerant of the
encodings the v1.150 media ships (UTF-16LE with BOM, null-heavy UTF-16LE,
UTF-8 with or without BOM), skipping blank and // comment lines. Same
semantics as the bootstrap package's private readTextdataFile; duplicated
so this package stays a leaf (bootstrap imports monster).
==================
*/
func readTabbedFile(path string) [][]string {
	buffer, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := decodeTextdata(buffer)
	cells := make(texttable.Cells)
	text = strings.TrimPrefix(text, "\ufeff")
	var rows [][]string
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimRight(rawLine, "\r\t ")
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		rows = append(rows, cells.Split(line))
	}
	return rows
}

/*
================
decodeTextdata
================
*/
func decodeTextdata(buffer []byte) string {
	if len(buffer) >= 2 && buffer[0] == 0xff && buffer[1] == 0xfe {
		return decodeUTF16LE(buffer[2:])
	}
	sampled := len(buffer)
	if sampled > 512 {
		sampled = 512
	}
	nulls := 0
	for i := 0; i < sampled; i++ {
		if buffer[i] == 0x00 {
			nulls++
		}
	}
	if sampled > 0 && float64(nulls)/float64(sampled) > 0.2 {
		return decodeUTF16LE(buffer)
	}
	return string(buffer)
}

/*
================
decodeUTF16LE
================
*/
func decodeUTF16LE(buffer []byte) string {
	units := make([]uint16, 0, len(buffer)/2)
	for i := 0; i+1 < len(buffer); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(buffer[i:i+2]))
	}
	return string(utf16.Decode(units))
}

// elementResist returns the roll-ordered element resists. Columns are
// frozen, frostbite, burn, shock, poison, zombie; burn and shock swap.
/*
================
elementResist
================
*/
func elementResist(cols []string) [6]uint8 {
	var out [6]uint8
	if len(cols) <= colResistFrozen+5 {
		return out
	}
	for i, column := range [6]int{0, 1, 3, 2, 4, 5} {
		v, err := strconv.Atoi(strings.TrimSpace(cols[colResistFrozen+column]))
		if err == nil && v > 0 && v <= 255 {
			out[i] = uint8(v)
		}
	}
	return out
}
