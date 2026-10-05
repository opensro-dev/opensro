package simulation

// This file owns the shipped-data half of the static NPC world. npcpos.txt is
// a mixed object-position table; characterdata's exact NPC TID band decides
// which rows are NPCs. Keeping that classification beside the NPC wire owner
// prevents bootstrap from maintaining a second, hand-curated world roster.

import (
	"encoding/binary"
	"fmt"
	"math"
	"opensro.online/server/internal/data/texttable"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"opensro.online/server/internal/domain"
)

const (
	npcColService   = 0
	npcColRefObjID  = 1
	npcColCodename  = 2
	npcColNameStrID = 5
	npcColCharBit   = 8
	npcColTid1      = 9
	npcColTid2      = 10
	npcColTid3      = 11
	npcColWalk      = 46
	npcColRun       = 47
	npcColScale     = 48
	npcColModel     = 52
	npcColLevel     = 57
	npcColMaxHP     = 59
)

type npcWorldRef struct {
	refObjID uint32
	tidWord  uint16
	codename string
	nameID   string
	name     string
	model    string
	level    uint8
	maxHP    uint32
	walk     float64
	run      float64
	scale    float64
	base     string
	quest    string
}

// LoadNpcWorldRoster projects every enabled NPC position in the shipped
// v1.150 data. Identities occupy the dedicated [200001,299999] band and are
// stable because npcpos file order is authored and deterministic. Only rows
// whose packed TID passes the native NPC class gate are admitted.
func LoadNpcWorldRoster(textdataDir string) []NpcDef {
	refs := loadNpcWorldRefs(textdataDir)
	if len(refs) == 0 {
		return nil
	}
	storeGroupsByNpc := loadNpcStoreGroupIndex(textdataDir, refs)
	fixtureByCode := make(map[string]NpcDef)
	for _, row := range DefaultNpcRoster() {
		fixtureByCode[row.Codename] = row
	}

	positions := readNpcTabbed(filepath.Join(textdataDir, "npcpos.txt"))
	roster := make([]NpcDef, 0, len(positions))
	for _, cols := range positions {
		if len(cols) < 5 {
			continue
		}
		refID, ok := npcUint32(cols[0])
		if !ok {
			continue
		}
		ref, ok := refs[refID]
		if !ok {
			continue
		}
		region, ok := npcRegion(cols[1])
		x, okX := npcFloat(cols[2])
		y, okY := npcFloat(cols[3])
		z, okZ := npcFloat(cols[4])
		if !ok || !okX || !okY || !okZ {
			continue
		}
		index := len(roster)
		if index >= int(domain.GroundItemGIDBase-domain.NPCGIDBase-1) {
			break
		}
		heading, _ := npcPlacementHeading(ref.codename, region, x, y, z)
		row := NpcDef{
			ObjectID:          domain.NPCGIDBase + uint32(index) + 1,
			RefObjID:          ref.refObjID,
			TidWord:           ref.tidWord,
			Codename:          ref.codename,
			NameStrID:         ref.nameID,
			Name:              ref.name,
			ModelPath:         ref.model,
			Level:             ref.level,
			MaxHP:             ref.maxHP,
			BaseSpeechSymbol:  ref.base,
			QuestSpeechSymbol: ref.quest,
			AuthoredSpawn:     true,
			Spawn:             Spawn{RegionID: region, X: x, Y: y, Z: z, Angle: heading},
			WalkSpeed:         ref.walk,
			RunSpeed:          ref.run,
			ScaleDenom:        ref.scale,
		}
		row.NpcTalkStoreGroups = cloneNpcStoreGroups(storeGroupsByNpc[row.Codename])
		// Rebirth destinations are server gameplay policy and are not present in
		// the four shipped refshop tables. Preserve the pinned guide policy only;
		// shop rows are always rebuilt from authored data above.
		if pinned, found := fixtureByCode[row.Codename]; found {
			row.RebirthPoint = pinned.RebirthPoint
		}
		row.Services = ResolveNpcServices(row)
		row.TalkFlags = ResolveNpcTalkFlags(row)
		roster = append(roster, row)
	}
	return roster
}

// loadNpcStoreGroupIndex projects the exact native lookup chain used by
// sub_7d3b70 -> sub_5d5be0:
//
//	NPC codename -> refshopgroup group -> store -> tab group -> tab rows.
//
// The menu row's storeGroupId0c is characterRefId24 (the NPC RefObjID), not
// refshopgroup column 2. Rizin pins the read at 0x005d5dca as [record+0x24],
// and sub_7d3b70 proves that field is the lookup's character RefObjID.
func loadNpcStoreGroupIndex(textdataDir string, refs map[uint32]npcWorldRef) map[string][]NpcTalkStoreGroup {
	groupCodesByNpc := make(map[string][]string)
	for _, cols := range readNpcTabbed(filepath.Join(textdataDir, "refshopgroup.txt")) {
		if len(cols) >= 5 && strings.TrimSpace(cols[0]) == "1" {
			npcCode := strings.TrimSpace(cols[4])
			groupCodesByNpc[npcCode] = append(groupCodesByNpc[npcCode], strings.TrimSpace(cols[3]))
		}
	}
	storesByGroup := make(map[string][]string)
	for _, cols := range readNpcTabbed(filepath.Join(textdataDir, "refmappingshopgroup.txt")) {
		if len(cols) >= 4 && strings.TrimSpace(cols[0]) == "1" {
			storesByGroup[strings.TrimSpace(cols[2])] = append(
				storesByGroup[strings.TrimSpace(cols[2])], strings.TrimSpace(cols[3]))
		}
	}
	tabGroupsByStore := make(map[string][]string)
	for _, cols := range readNpcTabbed(filepath.Join(textdataDir, "refmappingshopwithtab.txt")) {
		if len(cols) >= 4 && strings.TrimSpace(cols[0]) == "1" {
			tabGroupsByStore[strings.TrimSpace(cols[2])] = append(
				tabGroupsByStore[strings.TrimSpace(cols[2])], strings.TrimSpace(cols[3]))
		}
	}
	tabsByGroup := make(map[string][]NpcTalkStoreTab)
	groupMeta := make(map[string]NpcTalkStoreTab)
	// 5D5BE0 creates one conversation row per refshoptabgroup, not per
	// refshoptab. Preserve that parent identity through the browser adapter.
	for _, cols := range readNpcTabbed(filepath.Join(textdataDir, "refshoptabgroup.txt")) {
		if len(cols) >= 5 && strings.TrimSpace(cols[0]) == "1" {
			id, ok := npcUint32(cols[2])
			if ok && id > 0 && id <= 0x7fffffff {
				groupMeta[strings.TrimSpace(cols[3])] = NpcTalkStoreTab{GroupID: int32(id), GroupLabelSymbol: strings.TrimSpace(cols[4])}
			}
		}
	}
	for _, cols := range readNpcTabbed(filepath.Join(textdataDir, "refshoptab.txt")) {
		if len(cols) < 6 || strings.TrimSpace(cols[0]) != "1" {
			continue
		}
		tabID, ok := npcUint32(cols[2])
		if !ok || tabID > 0x7fffffff {
			continue
		}
		tabGroup := strings.TrimSpace(cols[4])
		tabsByGroup[tabGroup] = append(tabsByGroup[tabGroup], NpcTalkStoreTab{
			TabID: int32(tabID), LabelSymbol: strings.TrimSpace(cols[5]),
			GroupID: groupMeta[tabGroup].GroupID, GroupLabelSymbol: groupMeta[tabGroup].GroupLabelSymbol,
		})
	}
	refIdByCode := make(map[string]uint32, len(refs))
	for _, ref := range refs {
		refIdByCode[ref.codename] = ref.refObjID
	}
	result := make(map[string][]NpcTalkStoreGroup)
	for npcCode, groupCodes := range groupCodesByNpc {
		var tabs []NpcTalkStoreTab
		for _, groupCode := range groupCodes {
			for _, storeCode := range storesByGroup[groupCode] {
				for _, tabGroup := range tabGroupsByStore[storeCode] {
					tabs = append(tabs, tabsByGroup[tabGroup]...)
				}
			}
		}
		if len(tabs) != 0 {
			result[npcCode] = []NpcTalkStoreGroup{{
				StoreGroupID: int32(refIdByCode[npcCode]), Tabs: tabs,
			}}
		}
	}
	return result
}

func cloneNpcStoreGroups(groups []NpcTalkStoreGroup) []NpcTalkStoreGroup {
	out := append([]NpcTalkStoreGroup(nil), groups...)
	for index := range out {
		out[index].Tabs = append([]NpcTalkStoreTab(nil), out[index].Tabs...)
	}
	return out
}

func loadNpcWorldRefs(textdataDir string) map[uint32]npcWorldRef {
	names := make(map[string]string)
	for _, cols := range readNpcTabbed(filepath.Join(textdataDir, "textdataname.txt")) {
		if len(cols) >= 9 {
			names[strings.TrimSpace(cols[1])] = strings.TrimSpace(cols[8])
		}
	}
	// npcchat's fourth column is NOT a quest greeting. Native quest scripts
	// install SN_<NPC>_QS separately (e.g. 8829E5..8829F9). Admit only
	// actual catalog symbols; absent greetings use the existing base speech.
	questGreetings := make(map[string]bool)
	for _, cols := range readNpcTabbed(filepath.Join(textdataDir, "textquest.txt")) {
		if len(cols) >= 3 && strings.TrimSpace(cols[0]) == "1" {
			questGreetings[strings.TrimSpace(cols[1])] = true
		}
	}
	speech := make(map[string][2]string)
	for _, cols := range readNpcTabbed(filepath.Join(textdataDir, "npcchat.txt")) {
		if len(cols) >= 4 && strings.TrimSpace(cols[0]) == "1" {
			code := strings.TrimSpace(cols[1])
			quest := "SN_" + code + "_QS"
			if !questGreetings[quest] {
				quest = ""
			}
			speech[code] = [2]string{strings.TrimSpace(cols[2]), quest}
		}
	}
	paths, _ := filepath.Glob(filepath.Join(textdataDir, "characterdata*.txt"))
	sort.Strings(paths)
	refs := make(map[uint32]npcWorldRef)
	for _, path := range paths {
		for _, cols := range readNpcTabbed(path) {
			if len(cols) <= npcColMaxHP || strings.TrimSpace(cols[npcColService]) != "1" {
				continue
			}
			charBit, ok0 := npcUint16(cols[npcColCharBit])
			tid1, ok1 := npcUint16(cols[npcColTid1])
			tid2, ok2 := npcUint16(cols[npcColTid2])
			tid3, ok3 := npcUint16(cols[npcColTid3])
			if !ok0 || !ok1 || !ok2 || !ok3 {
				continue
			}
			word := (tid1&7)<<2 | (tid2&3)<<5 | (tid3&0xf)<<7
			if charBit != 0 {
				word |= 2
			}
			// Exact CICNPC branch of sub_851420: TID1=1,TID2=2,TID3=2.
			if word&0x7fe != 0x0146 {
				continue
			}
			refID, ok := npcUint32(cols[npcColRefObjID])
			walk, walkOK := npcFloat(cols[npcColWalk])
			run, runOK := npcFloat(cols[npcColRun])
			scale, scaleOK := npcFloat(cols[npcColScale])
			level, levelOK := npcUint16(cols[npcColLevel])
			maxHP, hpOK := npcUint32(cols[npcColMaxHP])
			if !ok || !walkOK || !runOK || !scaleOK || scale <= 0 ||
				!levelOK || level > 0xff || !hpOK {
				continue
			}
			codename := strings.TrimSpace(cols[npcColCodename])
			nameID := strings.TrimSpace(cols[npcColNameStrID])
			name := names[nameID]
			if name == "" {
				name = codename
			}
			chat := speech[codename]
			refs[refID] = npcWorldRef{
				refObjID: refID, tidWord: word, codename: codename,
				nameID: nameID, name: name, model: strings.TrimSpace(cols[npcColModel]),
				level: uint8(level), maxHP: maxHP, walk: walk, run: run, scale: scale,
				base: chat[0], quest: chat[1],
			}
		}
	}
	return refs
}

func npcUint16(value string) (uint16, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 16)
	return uint16(n), err == nil
}

func npcUint32(value string) (uint32, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	return uint32(n), err == nil
}

func npcFloat(value string) (float64, bool) {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return n, err == nil
}

func npcRegion(value string) (uint16, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 17)
	if err != nil || n < -0x8000 || n > 0xffff {
		return 0, false
	}
	return uint16(n), true
}

func readNpcTabbed(path string) [][]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(data)
	cells := make(texttable.Cells)
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		units := make([]uint16, 0, (len(data)-2)/2)
		for index := 2; index+1 < len(data); index += 2 {
			units = append(units, binary.LittleEndian.Uint16(data[index:index+2]))
		}
		text = string(utf16.Decode(units))
	}
	text = strings.TrimPrefix(text, "\ufeff")
	var rows [][]string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r\t ")
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		rows = append(rows, cells.Split(line))
	}
	return rows
}

// 8762E0/852F80: fixed city structures have independent identities and box
// picking; their visible architecture is already in the map. Never fabricate
// a character model or use teleportdata's arrival point as the source position.
func AppendTeleportGates(dir string, roster []NpcDef) ([]NpcDef, error) {
	names := map[string]string{}
	for _, c := range readNpcTabbed(filepath.Join(dir, "textdataname.txt")) {
		if len(c) > 8 {
			names[c[1]] = c[8]
		}
	}
	fortresses := map[string]uint32{}
	for _, c := range readNpcTabbed(filepath.Join(dir, "siegefortress.txt")) {
		if len(c) > 2 && c[0] == "1" {
			id, ok := npcUint32(c[1])
			if ok {
				fortresses[c[2]] = id
			}
		}
	}
	rows := readNpcTabbed(filepath.Join(dir, "teleportbuilding.txt"))
	if len(rows) == 0 {
		return nil, fmt.Errorf("missing teleportbuilding table")
	}
	result := append([]NpcDef(nil), roster...)
	for index, c := range rows {
		if len(c) == 0 || c[0] != "1" {
			continue
		}
		if len(c) != 58 {
			return nil, fmt.Errorf("teleportbuilding row %d shape", index+1)
		}
		ref, ok := npcUint32(c[1])
		region, okRegion := npcRegion(c[41])
		if !ok || !okRegion || ref == 0 {
			return nil, fmt.Errorf("teleportbuilding row %d identity", index+1)
		}
		// Region zero denotes an event/instance-created gate, not a static spawn.
		if region == 0 {
			continue
		}
		values := [5]float64{}
		for j, col := range []int{43, 44, 45, 49, 50} {
			v, valid := npcFloat(c[col])
			if !valid || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("teleportbuilding row %d coordinate", index+1)
			}
			values[j] = v
		}
		if values[3] <= 0 || values[4] <= 0 {
			return nil, fmt.Errorf("teleportbuilding row %d bounds", index+1)
		}
		tid := uint16(0)
		for j, col := range []int{7, 8, 9, 10, 11, 12} {
			v, valid := npcUint16(c[col])
			if !valid {
				return nil, fmt.Errorf("teleportbuilding TID")
			}
			shift := []uint{0, 1, 2, 5, 7, 11}[j]
			tid |= v << shift
		}
		if tid&0x1e != 0x10 {
			return nil, fmt.Errorf("non-gate building %d", ref)
		}
		// Dedicated high portion of the existing static-world band. Ref identities
		// remain stable when NPC positions or other gate table rows are added.
		gid := uint32(250000) + ref
		if gid >= domain.GroundItemGIDBase {
			return nil, fmt.Errorf("teleport identity overflow")
		}
		result = append(result, NpcDef{ObjectID: gid, RefObjID: ref, TidWord: tid, Codename: c[2], NameStrID: c[5], Name: names[c[5]], AuthoredSpawn: true, Spawn: Spawn{RegionID: region, X: values[0], Y: values[1], Z: values[2]}, Teleport: &TeleportGateBounds{Radius: values[4], Height: values[3], FortressID: fortresses[c[55]]}})
	}
	return result, ValidateNpcRoster(result)
}
