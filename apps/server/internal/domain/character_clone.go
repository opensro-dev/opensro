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

	clone.MissionInventory = cloneInventoryRows(source.MissionInventory)
	clone.Buyback = cloneSlice(source.Buyback)
	for i := range clone.Buyback {
		clone.Buyback[i].Item.MagicOptions = cloneSlice(source.Buyback[i].Item.MagicOptions)
	}
	clone.World = cloneCharacterWorld(source.World)
	clone.ActiveCOS = clonePointer(source.ActiveCOS)
	if clone.ActiveCOS != nil && source.ActiveCOS.Container != nil {
		clone.ActiveCOS.Container = clonePointer(source.ActiveCOS.Container)
		clone.ActiveCOS.Container.Rows = cloneInventoryRows(source.ActiveCOS.Container.Rows)
	}
	clone.PetSkillWindows = cloneSlice(source.PetSkillWindows)
	clone.ParamJobs = cloneSlice(source.ParamJobs)
	if source.ItemGroupCooldowns != nil {
		clone.ItemGroupCooldowns = make(map[uint32]int64, len(source.ItemGroupCooldowns))
		for group, until := range source.ItemGroupCooldowns {
			clone.ItemGroupCooldowns[group] = until
		}
	}
	clone.TimedSkillJobs = cloneSlice(source.TimedSkillJobs)
	clone.AvatarInventory = cloneAvatarInventory(source.AvatarInventory)
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
		clone[index].MagicOptions =
			cloneSlice(source[index].MagicOptions)
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
	if source.AuthoredAreaReturn != nil {
		authoredAreaReturn := *source.AuthoredAreaReturn
		authoredAreaReturn.RegionID = clonePointer(source.AuthoredAreaReturn.RegionID)
		authoredAreaReturn.X = clonePointer(source.AuthoredAreaReturn.X)
		authoredAreaReturn.Y = clonePointer(source.AuthoredAreaReturn.Y)
		authoredAreaReturn.Z = clonePointer(source.AuthoredAreaReturn.Z)
		authoredAreaReturn.Angle = clonePointer(source.AuthoredAreaReturn.Angle)
		clone.AuthoredAreaReturn = &authoredAreaReturn
	}
	if source.RebirthPoint != nil {
		rebirthPoint := *source.RebirthPoint
		rebirthPoint.RegionID = clonePointer(source.RebirthPoint.RegionID)
		rebirthPoint.X = clonePointer(source.RebirthPoint.X)
		rebirthPoint.Y = clonePointer(source.RebirthPoint.Y)
		rebirthPoint.Z = clonePointer(source.RebirthPoint.Z)
		rebirthPoint.Angle = clonePointer(source.RebirthPoint.Angle)
		clone.RebirthPoint = &rebirthPoint
	}
	clone.DungeonFloorIndex = clonePointer(source.DungeonFloorIndex)
	clone.PackedInstance = clonePointer(source.PackedInstance)
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
