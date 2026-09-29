package enterworld

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// Stable bootstrap and wire-policy values live with the assembly they govern.
const (
	// BootstrapMode is the contract tag stamped on successful responses.
	BootstrapMode = "v150-browser-local-player-race-aware-empty-object-list"
	// BootstrapProtocolVersion is the semantic EnterWorld DTO contract. It is
	// intentionally independent of both the Alpha blob wrapper version and the
	// HELLO/WELCOME transport version.
	BootstrapProtocolVersion = 2
	// DefaultDivisionID is the divisionIdFor fallback.
	DefaultDivisionID = domain.DefaultDivisionID

	nativeResultFailure       = 0x00
	nativeResultSuccess       = 0x01
	nativeErrorInvalidRequest = 0x02
	nativeErrorCharacter      = 0x10

	// Earlier _AddNewChar body in the research backup @0x53f22d0 seeds
	// 45 inventory slots, zero gold/SP and level 1. The newer customized
	// bodies grant 500000 gold/2000000 SP; do not import those server edits.
	defaultStartingGold int64 = 0
	maxObjectListRows         = 1<<16 - 1
	objectListStartMode byte  = 0x01

	goldHeapSmallCodename  = "ITEM_ETC_GOLD_01"
	goldHeapMediumCodename = "ITEM_ETC_GOLD_02"
	goldHeapLargeCodename  = "ITEM_ETC_GOLD_03"
)

// Build ports buildMissionBootstrap. The visual loadout is derived, the
// inventory seeded, and the overlay RE-APPLIED after seeding so a first-ever
// and a restored session carry the same payload shape (the Node comment
// documents why: a payload whose SHAPE depends on how many times a character
// has logged in makes the client take different branches on the same code
// path).
func Build(deps *Deps, request BootstrapRequest) *BootstrapResult {
	divisionID := request.DivisionID
	if deps.ResolveDivisionID != nil {
		divisionID = deps.ResolveDivisionID(request.DivisionID)
		if divisionID == "" {
			return Failure(nativeErrorInvalidRequest, "unknownDivision")
		}
	} else if divisionID == "" {
		divisionID = DefaultDivisionID
	}
	characterName := strings.TrimSpace(request.CharacterName)
	if characterName == "" {
		return Failure(nativeErrorCharacter, "missingCharacterName")
	}

	var liveCharacter *Character
	if deps.Characters != nil {
		for _, candidate := range deps.Characters.CharactersForDivision(divisionID) {
			if candidate != nil && strings.EqualFold(candidate.Name, characterName) {
				liveCharacter = candidate
				break
			}
		}
	}
	if liveCharacter == nil {
		return Failure(nativeErrorCharacter, "characterNotFound")
	}

	character := readCharacterSnapshot(deps, divisionID, liveCharacter)
	if character == nil {
		return Failure(nativeErrorCharacter, "characterNotFound")
	}
	if character.DeletePending {
		return Failure(nativeErrorInvalidRequest, "deletePending")
	}
	if candidateEntry := ResolveLocalPlayerEntry(character, deps.Roster); deps.CanEnterWorldRegion != nil &&
		!deps.CanEnterWorldRegion(character, uint16(candidateEntry.StartProfile.RegionID)) {
		return Failure(nativeErrorInvalidRequest, "areaAccessDenied")
	}

	if character.MissionInventory == nil {
		seedEntry := ResolveLocalPlayerEntry(character, deps.Roster)
		seedRoster := ResolveEquipRoster(
			character,
			seedEntry.VisualLoadout.ModelCodename,
			deps.Items,
			deps.EquipItemsEnabled,
		)
		deps.Mutate(liveCharacter, "bootstrap-seed", func() {
			// A concurrent enter-world can seed between the snapshot and
			// this door. Recheck under the authority write lock.
			if liveCharacter.MissionInventory != nil {
				return
			}
			EnsureMissionInventory(liveCharacter, seedRoster)
			gold := int64(0)
			if liveCharacter.Gold != nil {
				gold = coerceInt(
					liveCharacter.Gold,
					0,
					1<<53-1,
					0,
				)
			}
			if gold == 0 {
				gold = defaultStartingGold
			}
			liveCharacter.Gold = &gold
		})

		character = readCharacterSnapshot(
			deps,
			divisionID,
			liveCharacter,
		)
		if character == nil {
			return Failure(nativeErrorCharacter, "characterNotFound")
		}
		if character.DeletePending {
			return Failure(nativeErrorInvalidRequest, "deletePending")
		}
	}

	if deps.NormalizeEntryQuests != nil {
		if err := deps.NormalizeEntryQuests(character); err != nil {
			return Failure(nativeErrorInvalidRequest, "invalidQuestState: "+err.Error())
		}
	}
	if deps.PrepareEntry != nil {
		if err := deps.PrepareEntry(divisionID, character.Name); err != nil {
			return Failure(nativeErrorInvalidRequest, "worldAdmission: "+err.Error())
		}
	}
	if deps.RestoreEntryEffects != nil {
		deps.RestoreEntryEffects(divisionID, character.Name)
	}
	return buildCharacterProjection(deps, divisionID, character)
}

// buildCharacterProjection assembles an already-admitted detached actor. It
// never seeds or mutates the live character, so re-entry can be prepared before
// an authoritative relocation/revival commits.
func buildCharacterProjection(deps *Deps, divisionID string, character *Character) *BootstrapResult {
	entry := ResolveLocalPlayerEntry(character, deps.Roster)
	if deps.EntryPopulationLease != nil {
		lease, valid := deps.EntryPopulationLease(divisionID, character.Name)
		if !valid || uint32(lease.ID) != entryFortressWorld(&entry) {
			return Failure(nativeErrorInvalidRequest, "worldMembershipUnavailable")
		}
		entry.Population = lease
	}
	entry.WalkSpeed, entry.RunSpeed = 20, 50
	if deps.EntrySkills != nil {
		entry.SpawnSkills = deps.EntrySkills(divisionID, character.Name)
	}
	if deps.EntryMovementSpeeds != nil {
		entry.WalkSpeed, entry.RunSpeed = deps.EntryMovementSpeeds(divisionID, character.Name)
	}
	if !validEntrySkills(entry.SpawnSkills) {
		return Failure(nativeErrorInvalidRequest, "invalidEntrySkills")
	}
	if deps.CanEnterWorldRegion != nil &&
		!deps.CanEnterWorldRegion(character, uint16(entry.StartProfile.RegionID)) {
		return Failure(nativeErrorInvalidRequest, "areaAccessDenied")
	}
	LiftSpawnAboveTerrain(&entry, deps.SpawnTerrainHeight, deps.SpawnSurfaceHeight)
	RescueStrandedSpawn(&entry, deps.RelocateStrandedSpawn)
	eventGuideStateMask := ResolveEventGuideStateMask(character)

	// Inventory, appearance, and wire rows derive from one detached snapshot.
	inventoryRows := InventoryWireItems(character.MissionInventory)

	localPlayerPayload := BuildLocalPlayerEntryPayload(character, &entry, eventGuideStateMask, inventoryRows)
	objectID := ObjectIDForCharacter(character)

	encodeItems := func(rows []WireItem) []EquipItemRow {
		equipItems := make([]EquipItemRow, 0, len(rows))
		for _, item := range rows {
			body := BuildItemBody(item)
			bodyInts := make([]int, len(body))
			for index, value := range body {
				bodyInts[index] = int(value)
			}
			equipItems = append(equipItems, EquipItemRow{
				RefObjID: item.RefObjID,
				Slot:     item.Slot,
				Body:     bodyInts,
			})
		}
		return equipItems
	}
	equipItems := encodeItems(inventoryRows)
	var avatarItems []EquipItemRow
	if character.AvatarInventory != nil {
		avatarItems = encodeItems(InventoryWireItems(character.AvatarInventory.Rows))
	}

	var refObjSnapshot []RefObjRow
	if deps.RefObjSnapshot != nil {
		refObjSnapshot = deps.RefObjSnapshot()
	}
	if refObjSnapshot == nil {
		refObjSnapshot = []RefObjRow{}
	}

	var systemMessages interface{} = []string{}
	if deps.SystemMessages != nil {
		systemMessages = deps.SystemMessages(character)
	}

	packets, packetErr := buildBootstrapPackets(deps, divisionID, character, &entry, localPlayerPayload, objectID)
	if packetErr != nil {
		return Failure(nativeErrorInvalidRequest, "invalidCOSRestoration")
	}
	academyMember := false
	var skillSnapshot []SpawnSkillRow
	if deps.BrowserReferences == nil {
		skillSnapshot = spawnSkillSnapshot(deps.Skills)
	}
	if deps.TrainingCamps != nil {
		_, academyMember = deps.TrainingCamps.CampOfCharacter(divisionID, character.ID)
	}
	return &BootstrapResult{
		NativeResult:         nativeResultSuccess,
		DivisionID:           divisionID,
		Character:            character,
		AcademyMember:        academyMember,
		EventGuideStateMask:  eventGuideStateMask,
		RefObjSnapshot:       refObjSnapshot,
		RefSkillSnapshot:     skillSnapshot,
		RefItemSnapshot:      buildRefItemSnapshot(deps, divisionID, character),
		MagicOptionSnapshot:  buildMagicOptionSnapshot(deps, character),
		SiegeItemForgeGroups: DefaultSiegeItemForgeGroups(),
		SiegeFortressData:    DefaultSiegeFortressDataRows(),
		GameWorldData:        DefaultGameWorldDataRows(),
		AvatarItems:          avatarItems,
		EquipItems:           equipItems,
		LocalPlayerEntry:     &entry,
		ChatPermissions:      ChatPermissions{},
		ChatMessages:         []string{},
		SystemMessages:       systemMessages,
		Packets:              packets,
	}
}

// readCharacterSnapshot detaches the full record while the authority read door
// is held. Wire assembly then neither retains mutable store-owned fields nor
// holds the store lock during catalog and packet work.
func readCharacterSnapshot(
	deps *Deps,
	divisionID string,
	character *Character,
) *Character {
	var snapshot *Character
	deps.Read(divisionID, func() {
		snapshot = character.Snapshot()
	})
	return snapshot
}

// buildBootstrapPackets assembles the packet sequence in the exact Node
// order: SR_RESET_CLIENT (region LE u16 - retail sends it ahead of the
// char-data sequence and the handler reads the loading-screen rebuild key
// off it), the char-data begin/chunk/flush triplet, the server-clock gid
// latch (after the flush so the local player exists before CICPlayer_SetGID
// re-latches), then the object list.
func buildBootstrapPackets(deps *Deps, divisionID string, character *Character, entry *LocalPlayerEntry, localPlayerPayload []byte, objectID uint32) ([]Packet, error) {
	regionID := entry.StartProfile.RegionID
	packets := []Packet{
		NewPacket(OpcodeResetClient, []byte{byte(regionID & 0xff), byte((regionID >> 8) & 0xff)}),
		NewPacket(OpcodeMyCharacterData, nil),
		NewPacket(OpcodeMyCharacterChunk, localPlayerPayload),
		NewPacket(OpcodeMyCharacterFlush, nil),
		NewPacket(OpcodeServerClockGidLatch, BuildServerClockGidLatchPayload(objectID)),
	}
	var activeCOSRow *Packet
	var activeCOSRide *Packet
	if character.ActiveCOS != nil && character.ActiveCOS.Summoned {
		characters, ok := deps.Items.(CharacterRefSource)
		ref, found := (*CharacterRef)(nil), false
		if ok {
			ref, found = characters.CharacterRefByCodename(character.ActiveCOS.Codename)
		}
		expectedGID, gidOK := CosObjectIDForCharacter(character)
		cos := character.ActiveCOS
		if !found || ref == nil || ref.RefObjID != cos.RefObjID || (ref.TidWord>>11 < 2 || ref.TidWord>>11 > 4) ||
			!gidOK || cos.GID != expectedGID {
			return nil, fmt.Errorf("active COS failed authoritative media/identity validation")
		} else {
			record, recordErr := BuildCOSRecord(cos, ref, deps.Items)
			if recordErr != nil {
				return nil, recordErr
			}
			packets = append(packets, NewPacket(wire.OpCosRecordCreate, record))
			name := cos.Name
			if name == "" {
				name = ref.Name
			}
			spawnPayload := wire.EncodeCosSpawnBand2(wire.CosSpawnBand2{
				BodyStatus: cos.NativeBodyStatus,
				Band:       uint8(ref.TidWord >> 11),
				RefObjID:   cos.RefObjID,
				Gid:        cos.GID,
				Position: wire.Position{
					RegionID: uint16(entry.StartProfile.RegionID & 0xffff),
					X:        float32(entry.StartProfile.X),
					Y:        float32(entry.StartProfile.Y),
					Z:        float32(entry.StartProfile.Z),
					Heading:  uint16(entry.StartProfile.Angle & 0xffff),
				},
				Walk:      ref.WalkSpeed,
				Run:       ref.RunSpeed,
				Scale:     ref.Scale,
				Name:      name,
				OwnerName: character.Name,
				OwnerGid:  objectID,
			})
			row := NewPacket(OpcodeObjectListChunk, spawnPayload[:len(spawnPayload)-1])
			activeCOSRow = &row
			if cos.Mounted {
				ride := NewPacket(wire.OpCosRideState, wire.EncodeCosRideState(objectID, true, cos.GID))
				activeCOSRide = &ride
			}
		}
	}
	var rows []Packet
	if deps.ObjectListRows != nil {
		rows = deps.ObjectListRows(divisionID, character, entry)
	}
	if activeCOSRow != nil {
		rows = append(rows, *activeCOSRow)
	}
	// The object-list start count is a LE u16 in the native protocol (the
	// same encoding mission/monstertick.go's despawn bracket writes); the
	// old hardcoded 0x00 high byte desynced the client past 255 rows
	// (NPCs + ring monsters + unbounded ground drops can cross it). At or
	// below 255 rows the bytes are identical to before. Past the u16
	// ceiling the rows clamp BEFORE both the count and the bodies emit,
	// so the two can never disagree - no multi-frame paging contract is
	// pinned, so truncate-and-log is the honest posture.
	if len(rows) > maxObjectListRows {
		log.Errorf("bootstrap: object list for division %s truncated from %d to %d rows (u16 count ceiling)", divisionID, len(rows), maxObjectListRows)
		rows = rows[:maxObjectListRows]
	}
	packets = append(packets, NewPacket(OpcodeObjectListStart, []byte{objectListStartMode, byte(len(rows) & 0xff), byte(len(rows) >> 8)}))
	packets = append(packets, rows...)
	packets = append(packets, NewPacket(OpcodeObjectListFinalize, nil))
	if activeCOSRide != nil {
		packets = append(packets, *activeCOSRide)
	}
	// The 0x3369 reset that opens this sequence emptied the board, so every
	// live window is re-raised after it. A window lives until its deadline,
	// not while its pet is out: no native eraser of a kind-3 row is reachable
	// from a COS despawn, so the row outlives a dismissed pet in session and
	// re-entry restores it the same way. Any window, live or spent, hands the
	// character to the tick sweep, which owns retirement.
	packets = append(packets, petSkillWindowPackets(deps, character, deps.clock().UnixMilli())...)
	if len(character.PetSkillWindows) > 0 && deps.TrackTimedWindows != nil {
		deps.TrackTimedWindows(divisionID, character.Name)
	}
	return packets, nil
}

/*
================
buildRefItemSnapshot

Reference rows for every item the entered world can show: the character's
own inventories (worn, bag, COS, avatars, pet windows), every persisted
inventory in the division (peer 0x30D7 rows), ground drops from the seam and
the gold heap tiers (spawnable at any time; without their rows the client's
0x30D7 parse hits the unguarded RefObjData miss). The starter roster is a
first-boot seed only and contributes nothing here: once seeded, the
character's items are its inventory.
================
*/
func buildRefItemSnapshot(deps *Deps, divisionID string, character *Character) []RefItemRow {
	snapshot := []RefItemRow{}
	known := map[uint32]bool{}
	appendByCodename := func(codename string, typeFlags *uint16) {
		if codename == "" || deps.Items == nil {
			return
		}
		row, ok := deps.Items.ItemRefByCodename(codename)
		if !ok || row == nil || known[row.RefObjID] {
			return
		}
		known[row.RefObjID] = true
		flags := row.TypeFlags()
		if typeFlags != nil {
			flags = *typeFlags
		}
		name := row.Name
		snapshot = append(snapshot, RefItemRow{
			RefObjID:     row.RefObjID,
			Icon:         row.Icon,
			TypeFlags:    flags,
			Codename:     row.Codename,
			Kind:         "equipment",
			Name:         name,
			NativeFields: row.NativeFields,
		})
	}
	if character != nil {
		for _, row := range character.MissionInventory {
			flags := row.TypeFlags
			appendByCodename(row.Codename, &flags)
		}
		if character.ActiveCOS != nil && character.ActiveCOS.Container != nil {
			for _, row := range character.ActiveCOS.Container.Rows {
				flags := row.TypeFlags
				appendByCodename(row.Codename, &flags)
			}
		}
		// A window outlives the stack that raised it; its reference must still
		// reach the browser or the re-raised row has no icon or limit.
		for _, window := range character.PetSkillWindows {
			appendByCodename(window.Codename, nil)
		}
		if character.AvatarInventory != nil {
			// Avatar rows ride the SAME snapshot: the client's entered
			// avatar loop parses each row through the full CSOItem reader,
			// and an unseeded refObjId desyncs the stream (sub_78c830
			// L182-183) - the row's family word must be the real itemdata
			// avatar-band word ((typeFlags & 0x780) == 0x680).
			for _, row := range character.AvatarInventory.Rows {
				flags := row.TypeFlags
				appendByCodename(row.Codename, &flags)
			}
		}
	}
	// A live 0x30D7 player row can carry another character's worn items.
	// The browser has no PK2 table and seeds this lookup only at bootstrap;
	// stocking just the viewer's inventory makes the peer parser abort on
	// the first different garment/weapon. Seed every persisted inventory in
	// the division from detached snapshots. This also covers a later login
	// or equip from an already-owned bag row without shipping the full PK2.
	if deps.Characters != nil {
		for _, livePeer := range deps.Characters.CharactersForDivision(divisionID) {
			peer := readCharacterSnapshot(deps, divisionID, livePeer)
			if peer == nil {
				continue
			}
			for _, row := range peer.MissionInventory {
				flags := row.TypeFlags
				appendByCodename(row.Codename, &flags)
			}
		}
	}
	if deps.ExtraRefItemCodenames != nil {
		for _, codename := range deps.ExtraRefItemCodenames(divisionID) {
			appendByCodename(codename, nil)
		}
	}
	for _, goldCodename := range []string{
		goldHeapSmallCodename,
		goldHeapMediumCodename,
		goldHeapLargeCodename,
	} {
		appendByCodename(goldCodename, nil)
	}
	// Native 59edb0 follows the item's associated character before deciding
	// between riding and transport tutorials. Do not infer it from item names.
	if characters, ok := deps.Items.(CharacterRefSource); ok {
		for i := range snapshot {
			item, found := deps.Items.ItemRefByCodename(snapshot[i].Codename)
			if !found || item == nil || item.AssociatedCharacterCodename == "" {
				continue
			}
			if ref, found := characters.CharacterRefByCodename(item.AssociatedCharacterCodename); found && ref != nil {
				flags := ref.TidWord
				snapshot[i].SummonedCharacterTypeFlags = &flags
			}
		}
	}
	if deps.Items != nil {
		for i := range snapshot {
			if ref, ok := deps.Items.ItemRefByCodename(snapshot[i].Codename); ok && ref != nil {
				snapshot[i].DescriptionSymbol = ref.DescriptionSymbol
			}
		}
	}
	return snapshot
}
