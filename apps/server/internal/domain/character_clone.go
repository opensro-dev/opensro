/*
===========================================================================

character_clone.go - detached copies of the character authority graph

Copy mutable children while the caller holds the read door. Preserve nil
versus empty values because bootstrap uses absence to identify unseeded data.

===========================================================================
*/
package domain

import "reflect"

/*
================
cloneCharacter

Every mutable child added to Character must acquire an independent copy here.
================
*/
func cloneCharacter(source *Character) *Character {
	return cloneCharacterFiltered(source, rowsAll)
}

/*
================
CloneCharacter

cloneCharacter for callers outside the package (the store's committed
copies, persistence.go): a detached, value-equal copy of the whole
record. Every mutable child added to Character must acquire an
independent copy in cloneCharacterFiltered, or this copy aliases it.
================
*/
func CloneCharacter(source *Character) *Character {
	return cloneCharacter(source)
}

/*
================
CloneCharacterStaged

The store's next committed copy: the same detached, value-equal copy
CloneCharacter returns, except that a child `previous` already holds
in the same value is shared with it instead of copied again.
`previous` must be a staged copy itself (this function's or
CloneCharacter's result, or nil): nothing writes a staged copy after
staging, so the sharing keeps the new copy exactly as detached from
the live record as a full clone, and the section walk comparing the
two copies cannot see a write cross between them. Staging a whole
deep clone per save was the live tick's largest allocator (#2030);
between two saves most of a record's bulk compares equal, so most of
the copy is the record shell.
================
*/
func CloneCharacterStaged(source, previous *Character) *Character {
	return cloneCharacterFilteredFrom(source, rowsAll, previous)
}

/*
================
cloneCharacterEquipment

cloneCharacter for readers that never leave the worn equipment: the
mission inventory keeps only its equipment rows and the avatar
inventory is not copied at all. Every other child detaches exactly as
cloneCharacter detaches it. Dropping the bag rows also drops every
companion stored in them: Companions reaches the row Summon records,
so a reader of this copy must never resolve companions.
================
*/
func cloneCharacterEquipment(source *Character) *Character {
	return cloneCharacterFiltered(source, rowsEquipment)
}

/*
================
cloneCharacterCombat

cloneCharacter for the combat stat readers (#1943): the mission
inventory keeps only its equipment rows, and the avatar inventory is
copied whole, since the stat projection reads its option rows. The same
companion rule as cloneCharacterEquipment applies.
================
*/
func cloneCharacterCombat(source *Character) *Character {
	return cloneCharacterFiltered(source, rowsCombat)
}

/*
================
cloneCharacterCast

cloneCharacter for the skill-press readers (#1943): the mission
inventory keeps its equipment rows, the whole avatar inventory, and
every bag row carrying a companion. Companion resolution walks the
bag-row Summon records, and the press flow resolves the caster's
companions through its copy (the crow lane), so the kept bag rows
keep that resolution identical to the full copy's; the plain bag
rows it drops are read by no press reader.

The companions ride as the pet-owner view's copies (#3164): every
record without its rentals and, the active one excepted, its
container; the active cargo rides as values (cloneCOSCompanionActive).
The audit of the press readers (#3164): every container read reachable
from this copy goes through the active companion (caravanCargoValue,
cargoGoldValue, cargoGoodsCount, loadedTransport), the container lanes
(ownedCOSContainer and its callers) read door copies, and nothing in
the action lanes reads rentals. A new reader of a summoner row's
container or of rentals through this copy would see nil: widen the
copy with it.
================
*/
func cloneCharacterCast(source *Character) *Character {
	return cloneCharacterFiltered(source, rowsCast)
}

/*
================
cloneCharacterCompanion

The pet-owner view (#2030): the per-tick pet and brood passes copy
only what their readers read, detached exactly as cloneCharacter
detaches it - identity, world and model, the companions (every record
VisitCompanions walks, in its order, each without its rentals and -
the active one excepted - its container: the caravan verdict weighs
the active companion's cargo rows, so those ride as values), life,
body and teleport state,
the PvP and relation facts (PK, aggressions, guild, job, event
membership, visibility, transform), and of the mission inventory only
the companion rows and the relation slot (the job suit and cape row the
relation rules read). Every other field is zero.

The list is the pet chain's audited read set (#2030 comment
6475323279): the whole action suite runs on this view, and the relation
helpers (attack refusal, world enemy, guild war, caravan, free battle,
team) were checked field by field. A new reader of any other field
through this copy would silently see it zero: widen the list with it.
================
*/
func cloneCharacterCompanion(source *Character) *Character {
	if source == nil {
		return nil
	}
	clone := &Character{
		ID: source.ID, AccountID: source.AccountID, Name: source.Name, DeletePending: source.DeletePending,
		ModelCodename: source.ModelCodename, BroodGeneration: source.BroodGeneration,
		NativeTeleportMode: source.NativeTeleportMode, NativeBodyStatus: source.NativeBodyStatus,
		BattleUntilMs: source.BattleUntilMs, Job: source.Job, GMPrivilege: source.GMPrivilege,
		PublicHidden: source.PublicHidden, TransformRefObjID: source.TransformRefObjID,
		TransformMode: source.TransformMode, TransformShape: source.TransformShape,
		TransformEquipment: source.TransformEquipment,
	}
	clone.PK = clonePointer(source.PK)
	clone.EventMembership = clonePointer(source.EventMembership)
	if source.Aggressions != nil {
		clone.Aggressions = make(map[uint32]uint32, len(source.Aggressions))
		for gid, ticks := range source.Aggressions {
			clone.Aggressions[gid] = ticks
		}
	}
	clone.Level = clonePointer(source.Level)
	clone.MaxLevel = clonePointer(source.MaxLevel)
	clone.CurrentHP = clonePointer(source.CurrentHP)
	clone.CurrentMP = clonePointer(source.CurrentMP)
	clone.GuildID = clonePointer(source.GuildID)
	clone.MissionInventory = cloneCompanionRows(source.MissionInventory)
	clone.World = cloneCharacterWorld(source.World)
	clone.ActiveCOS = cloneCOSCompanionActive(source.ActiveCOS)
	clone.Mercenaries = cloneSlice(source.Mercenaries)
	for i, soldier := range clone.Mercenaries {
		clone.Mercenaries[i] = cloneCOSCompanion(soldier)
	}
	clone.CapturedCOS = cloneCOSCompanion(source.CapturedCOS)
	clone.Brood = cloneSlice(source.Brood)
	for i, pet := range source.Brood {
		clone.Brood[i] = cloneCOSCompanion(pet)
	}
	return clone
}

// cloneRows names which inventory rows one clone walk detaches.
type cloneRows int

const (
	// rowsAll detaches every mission and avatar row.
	rowsAll cloneRows = iota
	// rowsEquipment keeps the equipment rows and no avatar inventory.
	rowsEquipment
	// rowsCombat keeps the equipment rows and the whole avatar inventory.
	rowsCombat
	// rowsCast keeps the combat rows and every companion-carrying bag row.
	rowsCast
)

/*
================
cloneCharacterFiltered

The one clone walk; rows narrows the two inventory children.
================
*/
func cloneCharacterFiltered(source *Character, rows cloneRows) *Character {
	return cloneCharacterFilteredFrom(source, rows, nil)
}

/*
================
cloneCharacterFilteredFrom

cloneCharacterFiltered against a previous full copy: a child the
previous copy already holds in the same value is shared with it
instead of copied again (stagedChild). Only the full walk shares,
and only against a previous staged copy; the narrowed walks pass no
previous, and the inventory children share only in their full form -
a narrowed copy must never inherit the previous copy's whole
inventory. Every mutable child added to Character must acquire its
stagedChild line here, exactly as it once acquired a clone line.
================
*/
func cloneCharacterFilteredFrom(source *Character, rows cloneRows, previous *Character) *Character {
	if source == nil {
		return nil
	}
	previousChild := Character{}
	if previous != nil {
		previousChild = *previous
	}
	clone := *source
	clone.PK = stagedChild(source.PK, previousChild.PK, clonePointer)
	clone.Online = stagedChild(source.Online, previousChild.Online, cloneOnlineRecord)
	clone.EventMembership = stagedChild(source.EventMembership, previousChild.EventMembership, clonePointer)
	clone.Aggressions = stagedChild(source.Aggressions, previousChild.Aggressions, cloneMapValue)

	clone.RaceIndex = stagedChild(source.RaceIndex, previousChild.RaceIndex, clonePointer)
	clone.Gender = stagedChild(source.Gender, previousChild.Gender, clonePointer)
	clone.ModelRef = stagedChild(source.ModelRef, previousChild.ModelRef, clonePointer)
	clone.WeaponIndex = stagedChild(source.WeaponIndex, previousChild.WeaponIndex, clonePointer)
	clone.ProtectorIndex = stagedChild(source.ProtectorIndex, previousChild.ProtectorIndex, clonePointer)
	clone.BodyShapeByte = stagedChild(source.BodyShapeByte, previousChild.BodyShapeByte, clonePointer)
	clone.HeightIndex = stagedChild(source.HeightIndex, previousChild.HeightIndex, clonePointer)
	clone.VolumeIndex = stagedChild(source.VolumeIndex, previousChild.VolumeIndex, clonePointer)
	clone.HeightScale = stagedChild(source.HeightScale, previousChild.HeightScale, clonePointer)
	clone.VolumeScale = stagedChild(source.VolumeScale, previousChild.VolumeScale, clonePointer)

	clone.Level = stagedChild(source.Level, previousChild.Level, clonePointer)
	clone.MaxLevel = stagedChild(source.MaxLevel, previousChild.MaxLevel, clonePointer)
	clone.CurrentHP = stagedChild(source.CurrentHP, previousChild.CurrentHP, clonePointer)
	clone.CurrentMP = stagedChild(source.CurrentMP, previousChild.CurrentMP, clonePointer)
	clone.SkillPoints = stagedChild(source.SkillPoints, previousChild.SkillPoints, clonePointer)
	clone.StatPoints = stagedChild(source.StatPoints, previousChild.StatPoints, clonePointer)
	clone.Experience = stagedChild(source.Experience, previousChild.Experience, clonePointer)
	clone.SkillExp = stagedChild(source.SkillExp, previousChild.SkillExp, clonePointer)
	clone.Gold = stagedChild(source.Gold, previousChild.Gold, clonePointer)
	clone.Strength = stagedChild(source.Strength, previousChild.Strength, clonePointer)
	clone.Intellect = stagedChild(source.Intellect, previousChild.Intellect, clonePointer)
	clone.GuildID = stagedChild(source.GuildID, previousChild.GuildID, clonePointer)
	clone.ExperiencePercent = stagedChild(source.ExperiencePercent, previousChild.ExperiencePercent, clonePointer)
	clone.FigureIndex = stagedChild(source.FigureIndex, previousChild.FigureIndex, clonePointer)

	switch rows {
	case rowsAll:
		clone.MissionInventory = stagedChild(source.MissionInventory, previousChild.MissionInventory, cloneInventoryRows)
	case rowsCast:
		clone.MissionInventory = cloneCastRows(source.MissionInventory)
	default:
		clone.MissionInventory = cloneEquipmentRows(source.MissionInventory)
	}
	clone.Buyback = stagedChild(source.Buyback, previousChild.Buyback, cloneBuybackRows)
	clone.World = stagedChild(source.World, previousChild.World, cloneCharacterWorld)
	if rows == rowsCast {
		// The press readers' companion copy (#3164): the caravan and
		// trade-tier verdicts weigh the active cargo as values, and no
		// press reader reads rentals or a cargo row's children.
		clone.ActiveCOS = cloneCOSCompanionActive(source.ActiveCOS)
	} else {
		clone.ActiveCOS = stagedChild(source.ActiveCOS, previousChild.ActiveCOS, CloneCOS)
	}
	clone.Mercenaries = stagedChild(source.Mercenaries, previousChild.Mercenaries, cloneCOSSet)
	clone.CapturedCOS = stagedChild(source.CapturedCOS, previousChild.CapturedCOS, CloneCOS)
	clone.Brood = stagedChild(source.Brood, previousChild.Brood, cloneCOSSet)
	clone.PetSkillWindows = stagedChild(source.PetSkillWindows, previousChild.PetSkillWindows, cloneSlice)
	clone.ParamJobs = stagedChild(source.ParamJobs, previousChild.ParamJobs, cloneSlice)
	clone.CompositeJobs = stagedChild(source.CompositeJobs, previousChild.CompositeJobs, cloneSlice)
	clone.PremiumClock = stagedChild(source.PremiumClock, previousChild.PremiumClock, clonePointer)
	clone.LevelReachedAt = stagedChild(source.LevelReachedAt, previousChild.LevelReachedAt, cloneMapValue)
	clone.ItemGroupCooldowns = stagedChild(source.ItemGroupCooldowns, previousChild.ItemGroupCooldowns, cloneMapValue)
	clone.TimedSkillJobs = stagedChild(source.TimedSkillJobs, previousChild.TimedSkillJobs, cloneSlice)
	if rows == rowsEquipment {
		clone.AvatarInventory = nil
	} else {
		clone.AvatarInventory = stagedChild(source.AvatarInventory, previousChild.AvatarInventory, cloneAvatarInventory)
	}
	clone.Masteries = stagedChild(source.Masteries, previousChild.Masteries, cloneSlice)
	clone.Skills = stagedChild(source.Skills, previousChild.Skills, cloneSlice)
	clone.OffensiveSkillCooldowns = stagedChild(source.OffensiveSkillCooldowns, previousChild.OffensiveSkillCooldowns, cloneMapValue)
	clone.SharedSkillCooldowns = stagedChild(source.SharedSkillCooldowns, previousChild.SharedSkillCooldowns, cloneMapValue)
	clone.MostWanted = stagedChild(source.MostWanted, previousChild.MostWanted, clonePointer)
	clone.VisualFlags = stagedChild(source.VisualFlags, previousChild.VisualFlags, clonePointer)
	clone.Mission = stagedChild(source.Mission, previousChild.Mission, cloneMissionRuntime)
	clone.QuickSlots = stagedChild(source.QuickSlots, previousChild.QuickSlots, cloneSlice)
	clone.CompletedQuestIds = stagedChild(source.CompletedQuestIds, previousChild.CompletedQuestIds, cloneSlice)
	clone.EndedQuestIds = stagedChild(source.EndedQuestIds, previousChild.EndedQuestIds, cloneSlice)
	clone.QuestCompletionCounts = stagedChild(source.QuestCompletionCounts, previousChild.QuestCompletionCounts, cloneMapValue)
	clone.ActiveQuests = stagedChild(source.ActiveQuests, previousChild.ActiveQuests, cloneActiveQuests)
	clone.QuestSupplies = stagedChild(source.QuestSupplies, previousChild.QuestSupplies, cloneMapValue)
	clone.TrackedQuests = stagedChild(source.TrackedQuests, previousChild.TrackedQuests, cloneTrackedQuests)
	clone.EnterEventGroupIds = stagedChild(source.EnterEventGroupIds, previousChild.EnterEventGroupIds, cloneSlice)
	clone.BlockedWhisperers = stagedChild(source.BlockedWhisperers, previousChild.BlockedWhisperers, cloneSlice)
	clone.Friends = stagedChild(source.Friends, previousChild.Friends, cloneSlice)

	return &clone
}

/*
================
clonePointer

Retain absence while detaching one scalar or shallow record.
================
*/
func clonePointer[T any](source *T) *T {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}

/*
================
cloneSlice

Detach the backing store without converting a nil slice to an empty one.
================
*/
func cloneSlice[T any](source []T) []T {
	if source == nil {
		return nil
	}
	clone := make([]T, len(source))
	copy(clone, source)
	return clone
}

/*
================
stagedChild

One child of a staged copy: the previous copy's child, shared, when
the live child still equals it, and a fresh detach of the live child
otherwise. Equality is reflect.DeepEqual - stricter than the section
walk's per-field comparison (it also sees fields no section covers,
and keeps nil distinct from empty), so a shared child is one the
section walk also reads as unchanged and the comparison's verdicts
cannot move. The previous copy is staged output, which nothing
writes after staging, so sharing it detaches the new copy from the
live record exactly as a fresh clone would.
================
*/
func stagedChild[T any](source, previous T, clone func(T) T) T {
	if reflect.DeepEqual(source, previous) {
		return previous
	}
	return clone(source)
}

/*
================
cloneMapValue

Detach one flat map child without converting a nil map to an empty one.
================
*/
func cloneMapValue[K comparable, V any](source map[K]V) map[K]V {
	if source == nil {
		return nil
	}
	clone := make(map[K]V, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

/*
================
cloneOnlineRecord

The online record with its per-day rows detached.
================
*/
func cloneOnlineRecord(source *OnlineRecord) *OnlineRecord {
	clone := clonePointer(source)
	if clone != nil {
		clone.Days = cloneSlice(source.Days)
	}
	return clone
}

/*
================
cloneBuybackRows

Buyback rows with each row's summon and magic options detached.
================
*/
func cloneBuybackRows(source []BuybackEntry) []BuybackEntry {
	clone := cloneSlice(source)
	for i := range clone {
		clone[i].Item.Summon = CloneCOS(source[i].Item.Summon)
		clone[i].Item.MagicOptions = cloneSlice(source[i].Item.MagicOptions)
	}
	return clone
}

/*
================
cloneCOSSet

A companion slice with every companion detached.
================
*/
func cloneCOSSet(source []*CharacterCOS) []*CharacterCOS {
	clone := cloneSlice(source)
	for i := range clone {
		clone[i] = CloneCOS(source[i])
	}
	return clone
}

/*
================
cloneInventoryRows

Magic option arrays are mutable children of each inventory row.
================
*/
func cloneInventoryRows(source []InventoryRow) []InventoryRow {
	clone := cloneSlice(source)
	for index := range clone {
		clone[index].Summon = CloneCOS(source[index].Summon)
		clone[index].MagicOptions =
			cloneSlice(source[index].MagicOptions)
	}
	return clone
}

/*
================
cloneEquipmentRows

The worn half of cloneInventoryRows: rows in the equipment slots,
deep-copied in order; bag rows are dropped, and a nil inventory stays
nil so absence keeps its meaning.
================
*/
func cloneEquipmentRows(source []InventoryRow) []InventoryRow {
	if source == nil {
		return nil
	}
	clone := make([]InventoryRow, 0, EquipmentSlotEnd)
	for _, row := range source {
		if row.Slot < 0 || row.Slot >= EquipmentSlotEnd {
			continue
		}
		row.Summon = CloneCOS(row.Summon)
		row.MagicOptions = cloneSlice(row.MagicOptions)
		clone = append(clone, row)
	}
	return clone
}

/*
================
cloneCastRows

The press half of cloneInventoryRows: the equipment rows plus every
bag row carrying a companion, copied in source order; plain bag rows
are dropped, and a nil inventory stays nil so absence keeps its
meaning. Keeping the kept rows in source order keeps Companions over
this copy visiting exactly the full copy's companions. A row's
companion rides as the pet-owner view's record copy
(cloneCOSCompanion), without its rentals and container: no press
reader reads either through a summoner row (#3164 audit), and copying
a transport's cargo into every press copy was the view's bulk.
================
*/
func cloneCastRows(source []InventoryRow) []InventoryRow {
	if source == nil {
		return nil
	}
	kept := 0
	for _, row := range source {
		if row.Slot >= 0 && row.Slot < EquipmentSlotEnd || row.Summon != nil {
			kept++
		}
	}
	clone := make([]InventoryRow, 0, kept)
	for _, row := range source {
		if row.Slot >= 0 && row.Slot < EquipmentSlotEnd || row.Summon != nil {
			row.Summon = cloneCOSCompanion(row.Summon)
			row.MagicOptions = cloneSlice(row.MagicOptions)
			clone = append(clone, row)
		}
	}
	return clone
}

/*
================
cloneCompanionRows

The pet-owner view's rows: the relation slot plus every row carrying
a companion, each copied in order, its companion riding as the view's
COS copy (cloneCOSCompanion); the remaining rows are dropped, and a
nil inventory stays nil so absence keeps its meaning.
VisitCompanions over the kept rows
visits the same companions in the same order as over the full
inventory, because the dropped rows carry no Summon.
================
*/
func cloneCompanionRows(source []InventoryRow) []InventoryRow {
	if source == nil {
		return nil
	}
	kept := 0
	for _, row := range source {
		if row.Summon != nil || row.Slot == JobSuitSlot {
			kept++
		}
	}
	clone := make([]InventoryRow, 0, kept)
	for _, row := range source {
		if row.Summon == nil && row.Slot != JobSuitSlot {
			continue
		}
		row.Summon = cloneCOSCompanion(row.Summon)
		row.MagicOptions = cloneSlice(row.MagicOptions)
		clone = append(clone, row)
	}
	return clone
}

/*
================
cloneCharacterWorld

Detach saved positions and raw movement bytes from the authority snapshot.
================
*/
func cloneCharacterWorld(source *CharacterWorld) *CharacterWorld {
	if source == nil {
		return nil
	}
	clone := *source
	if source.SavedReturn != nil {
		value := *source.SavedReturn
		clone.SavedReturn = &value
	}
	if source.Spawn != nil {
		spawn := *source.Spawn
		spawn.RegionID = clonePointer(source.Spawn.RegionID)
		spawn.X = clonePointer(source.Spawn.X)
		spawn.Y = clonePointer(source.Spawn.Y)
		spawn.Z = clonePointer(source.Spawn.Z)
		spawn.Angle = clonePointer(source.Spawn.Angle)
		clone.Spawn = &spawn
	}
	clone.AuthoredAreaReturn = cloneWorldSpawn(source.AuthoredAreaReturn)
	clone.RebirthPoint = cloneWorldSpawn(source.RebirthPoint)
	clone.LastRecallPoint = cloneWorldPoint(source.LastRecallPoint)
	clone.LastDeathPoint = cloneWorldPoint(source.LastDeathPoint)
	clone.DungeonFloorIndex = clonePointer(source.DungeonFloorIndex)
	clone.PackedInstance = clonePointer(source.PackedInstance)
	if source.InstanceReturn != nil {
		value := *source.InstanceReturn
		clone.InstanceReturn = &value
	}
	clone.MovementMode = clonePointer(source.MovementMode)
	clone.MoveSegment = cloneSlice(source.MoveSegment)
	return &clone
}

/*
================
cloneAvatarInventory

Avatar rows carry the same nested item options as ordinary inventory.
================
*/
func cloneAvatarInventory(source *AvatarInventory) *AvatarInventory {
	if source == nil {
		return nil
	}
	return &AvatarInventory{
		Capacity: source.Capacity,
		Rows:     cloneInventoryRows(source.Rows),
	}
}

/*
================
cloneActiveQuests

Each mission owns its objective values independently of the journal envelope.
================
*/
func cloneActiveQuests(source []ActiveQuestRecord) []ActiveQuestRecord {
	clone := cloneSlice(source)
	for questIndex := range clone {
		clone[questIndex].TargetIds =
			cloneSlice(source[questIndex].TargetIds)
		clone[questIndex].Contents =
			cloneSlice(source[questIndex].Contents)
		for nodeIndex := range clone[questIndex].Contents {
			clone[questIndex].Contents[nodeIndex].ObjectiveValues =
				cloneSlice(
					source[questIndex].
						Contents[nodeIndex].
						ObjectiveValues,
				)
		}
	}
	return clone
}

/*
================
cloneTrackedQuests

Preserve opaque tracking bytes without sharing their mutable backing store.
================
*/
func cloneTrackedQuests(source []TrackedQuestRecord) []TrackedQuestRecord {
	clone := cloneSlice(source)
	for index := range clone {
		clone[index].Tail6 = cloneSlice(source[index].Tail6)
	}
	return clone
}

/*
================
cloneWorldSpawn

A deep copy of an optional world point.
================
*/
func cloneWorldPoint(source *WorldPoint) *WorldPoint {
	if source == nil {
		return nil
	}
	clone := *source
	clone.WorldSpawn = *cloneWorldSpawn(&source.WorldSpawn)
	return &clone
}

func cloneWorldSpawn(source *WorldSpawn) *WorldSpawn {
	if source == nil {
		return nil
	}
	clone := *source
	clone.RegionID = clonePointer(source.RegionID)
	clone.X = clonePointer(source.X)
	clone.Y = clonePointer(source.Y)
	clone.Z = clonePointer(source.Z)
	clone.Angle = clonePointer(source.Angle)
	return &clone
}

/*
================
cloneMissionRuntime
================
*/
func cloneMissionRuntime(source *MissionRuntime) *MissionRuntime {
	if source == nil {
		return nil
	}
	clone := *source
	clone.EventGuideStateMask = clonePointer(source.EventGuideStateMask)
	return &clone
}
