/*
===========================================================================

character_clone.go - detached copies of the character authority graph

Copy mutable children while the caller holds the read door. Preserve nil
versus empty values because bootstrap uses absence to identify unseeded data.

===========================================================================
*/
package domain

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
cloneCharacterCompanion

The pet-owner view (#2030): the per-tick pet and brood passes copy
only what their readers read, detached exactly as cloneCharacter
detaches it - identity, world and model, the companions (every record
VisitCompanions walks, in its order), life, body and teleport state,
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
	clone.ActiveCOS = CloneCOS(source.ActiveCOS)
	clone.Mercenaries = cloneSlice(source.Mercenaries)
	for i, soldier := range clone.Mercenaries {
		clone.Mercenaries[i] = CloneCOS(soldier)
	}
	clone.CapturedCOS = CloneCOS(source.CapturedCOS)
	clone.Brood = cloneSlice(source.Brood)
	for i, pet := range source.Brood {
		clone.Brood[i] = CloneCOS(pet)
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
)

/*
================
cloneCharacterFiltered

The one clone walk; rows narrows the two inventory children.
================
*/
func cloneCharacterFiltered(source *Character, rows cloneRows) *Character {
	if source == nil {
		return nil
	}
	clone := *source
	clone.PK = clonePointer(source.PK)
	clone.EventMembership = clonePointer(source.EventMembership)
	if source.Aggressions != nil {
		clone.Aggressions = make(map[uint32]uint32, len(source.Aggressions))
		for gid, ticks := range source.Aggressions {
			clone.Aggressions[gid] = ticks
		}
	}

	clone.RaceIndex = clonePointer(source.RaceIndex)
	clone.Gender = clonePointer(source.Gender)
	clone.ModelRef = clonePointer(source.ModelRef)
	clone.WeaponIndex = clonePointer(source.WeaponIndex)
	clone.ProtectorIndex = clonePointer(source.ProtectorIndex)
	clone.BodyShapeByte = clonePointer(source.BodyShapeByte)
	clone.HeightIndex = clonePointer(source.HeightIndex)
	clone.VolumeIndex = clonePointer(source.VolumeIndex)
	clone.HeightScale = clonePointer(source.HeightScale)
	clone.VolumeScale = clonePointer(source.VolumeScale)

	clone.Level = clonePointer(source.Level)
	clone.MaxLevel = clonePointer(source.MaxLevel)
	clone.CurrentHP = clonePointer(source.CurrentHP)
	clone.CurrentMP = clonePointer(source.CurrentMP)
	clone.SkillPoints = clonePointer(source.SkillPoints)
	clone.StatPoints = clonePointer(source.StatPoints)
	clone.Experience = clonePointer(source.Experience)
	clone.SkillExp = clonePointer(source.SkillExp)
	clone.Gold = clonePointer(source.Gold)
	clone.Strength = clonePointer(source.Strength)
	clone.Intellect = clonePointer(source.Intellect)
	clone.GuildID = clonePointer(source.GuildID)
	clone.ExperiencePercent = clonePointer(source.ExperiencePercent)
	clone.FigureIndex = clonePointer(source.FigureIndex)

	if rows == rowsAll {
		clone.MissionInventory = cloneInventoryRows(source.MissionInventory)
	} else {
		clone.MissionInventory = cloneEquipmentRows(source.MissionInventory)
	}
	clone.Buyback = cloneSlice(source.Buyback)
	for i := range clone.Buyback {
		clone.Buyback[i].Item.Summon = CloneCOS(source.Buyback[i].Item.Summon)
		clone.Buyback[i].Item.MagicOptions = cloneSlice(source.Buyback[i].Item.MagicOptions)
	}
	clone.World = cloneCharacterWorld(source.World)
	clone.ActiveCOS = CloneCOS(source.ActiveCOS)
	clone.Mercenaries = cloneSlice(source.Mercenaries)
	for i, soldier := range clone.Mercenaries {
		clone.Mercenaries[i] = CloneCOS(soldier)
	}
	clone.CapturedCOS = CloneCOS(source.CapturedCOS)
	clone.Brood = cloneSlice(source.Brood)
	for i, pet := range source.Brood {
		clone.Brood[i] = CloneCOS(pet)
	}
	clone.PetSkillWindows = cloneSlice(source.PetSkillWindows)
	clone.ParamJobs = cloneSlice(source.ParamJobs)
	clone.CompositeJobs = cloneSlice(source.CompositeJobs)
	if source.PremiumClock != nil {
		clock := *source.PremiumClock
		clone.PremiumClock = &clock
	}
	if source.LevelReachedAt != nil {
		clone.LevelReachedAt = make(map[uint8]int64, len(source.LevelReachedAt))
		for level, at := range source.LevelReachedAt {
			clone.LevelReachedAt[level] = at
		}
	}
	if source.ItemGroupCooldowns != nil {
		clone.ItemGroupCooldowns = make(map[uint32]int64, len(source.ItemGroupCooldowns))
		for group, until := range source.ItemGroupCooldowns {
			clone.ItemGroupCooldowns[group] = until
		}
	}
	clone.TimedSkillJobs = cloneSlice(source.TimedSkillJobs)
	if rows == rowsEquipment {
		clone.AvatarInventory = nil
	} else {
		clone.AvatarInventory = cloneAvatarInventory(source.AvatarInventory)
	}
	clone.Masteries = cloneSlice(source.Masteries)
	clone.Skills = cloneSlice(source.Skills)
	if source.OffensiveSkillCooldowns != nil {
		clone.OffensiveSkillCooldowns = make(map[uint32]int64, len(source.OffensiveSkillCooldowns))
		for group, until := range source.OffensiveSkillCooldowns {
			clone.OffensiveSkillCooldowns[group] = until
		}
	}
	if source.SharedSkillCooldowns != nil {
		clone.SharedSkillCooldowns = make(map[uint8]int64, len(source.SharedSkillCooldowns))
		for group, until := range source.SharedSkillCooldowns {
			clone.SharedSkillCooldowns[group] = until
		}
	}
	clone.QuickSlots = cloneSlice(source.QuickSlots)
	clone.CompletedQuestIds = cloneSlice(source.CompletedQuestIds)
	clone.EndedQuestIds = cloneSlice(source.EndedQuestIds)
	if source.QuestCompletionCounts != nil {
		clone.QuestCompletionCounts = make(map[uint32]uint32, len(source.QuestCompletionCounts))
		for id, count := range source.QuestCompletionCounts {
			clone.QuestCompletionCounts[id] = count
		}
	}
	clone.ActiveQuests = cloneActiveQuests(source.ActiveQuests)
	if source.QuestSupplies != nil {
		clone.QuestSupplies = make(map[uint32]QuestSupplyState, len(source.QuestSupplies))
		for id, supply := range source.QuestSupplies {
			clone.QuestSupplies[id] = supply
		}
	}
	clone.TrackedQuests = cloneTrackedQuests(source.TrackedQuests)
	clone.EnterEventGroupIds = cloneSlice(source.EnterEventGroupIds)
	clone.BlockedWhisperers = cloneSlice(source.BlockedWhisperers)
	clone.Friends = cloneSlice(source.Friends)

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
cloneCompanionRows

The pet-owner view's rows: the relation slot plus every row carrying
a companion, each deep-copied in order exactly as cloneInventoryRows
copies it; the remaining rows are dropped, and a nil inventory stays
nil so absence keeps its meaning. VisitCompanions over the kept rows
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
		row.Summon = CloneCOS(row.Summon)
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
