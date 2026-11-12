package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestCharacterSnapshotDetachesMutableRecordGraph(t *testing.T) {
	level := int64(10)
	spawnX := 20.0
	returnX := 30.0
	deathX := 40.0
	floor := int64(3)
	instance := uint32(0x20001)
	character := &Character{
		Name:                  "snapshotOwner",
		QuestCompletionCounts: map[uint32]uint32{42: 2},
		Level:                 &level,
		BlockedWhisperers:     []string{"whisper-a"},
		CompletedQuestIds:     []uint32{},
		EndedQuestIds:         []uint32{43},
		MissionInventory: []InventoryRow{{
			Slot:         6,
			MagicOptions: []uint64{11},
		}},
		World: &CharacterWorld{
			PackedInstance:     &instance,
			InstanceReturn:     &InstanceReturnAnchor{PackedInstance: 0x20001, RegionID: 25000, X: 12.5, Y: 3, Z: -7, Angle: 90},
			Spawn:              &WorldSpawn{X: &spawnX},
			AuthoredAreaReturn: &WorldSpawn{X: &returnX},
			LastDeathPoint:     &WorldPoint{WorldSpawn: WorldSpawn{X: &deathX}, World: 2},
			DungeonFloorIndex:  &floor,
			MoveSegment:        []byte(`{"x":1}`),
		},
		ActiveQuests: []ActiveQuestRecord{{
			Contents: []ActiveQuestContentsNode{{
				ObjectiveValues: []uint32{7},
			}},
			TargetIds: []uint32{8},
		}},
		TrackedQuests: []TrackedQuestRecord{{
			Tail6: []uint8{1, 2, 3},
		}},
		Friends:    []FriendRecord{{ID: 9, Name: "friend"}},
		QuickSlots: []QuickSlotBinding{{Slot: 4, Kind: 0x49, Payload: 123}},
		PetSkillWindows: []PetSkillWindow{{
			ItemRefObjID: 24001, Codename: "ITEM_MALL_PET_SKILL_COLD", EndUnixMs: 5000,
		}},
	}

	var noCharacter *Character
	if noCharacter.SnapshotEquipment() != nil {
		t.Fatal("nil character produced an equipment copy")
	}

	snapshot := character.Snapshot()
	character.QuestCompletionCounts[42] = 3
	if snapshot.QuestCompletionCounts[42] != 2 {
		t.Fatal("snapshot shared quest completion counts")
	}
	character.EndedQuestIds[0] = 44
	if snapshot.EndedQuestIds[0] != 43 {
		t.Fatal("snapshot shared ended quests")
	}

	*character.Level = 11
	character.BlockedWhisperers[0] = "whisper-b"
	character.MissionInventory[0].MagicOptions[0] = 12
	*character.World.Spawn.X = 21
	*character.World.AuthoredAreaReturn.X = 31
	*character.World.LastDeathPoint.X = 41
	*character.World.DungeonFloorIndex = 4
	*character.World.PackedInstance = 0x30001
	character.World.InstanceReturn.PackedInstance = 0x30001
	character.World.InstanceReturn.X = 99
	character.World.MoveSegment[5] = '2'
	character.ActiveQuests[0].Contents[0].ObjectiveValues[0] = 10
	character.ActiveQuests[0].TargetIds[0] = 11
	character.TrackedQuests[0].Tail6[0] = 9
	character.Friends[0].Name = "changed"
	character.QuickSlots[0].Payload = 456
	// The tick sweep compacts this slice in place under the division lock
	// while a bootstrap reads its snapshot outside it.
	character.PetSkillWindows[0].EndUnixMs = 0

	if *snapshot.Level != 10 {
		t.Errorf("snapshot level = %d, want 10", *snapshot.Level)
	}
	if snapshot.BlockedWhisperers[0] != "whisper-a" {
		t.Errorf("snapshot blocked whisperer = %q, want whisper-a", snapshot.BlockedWhisperers[0])
	}
	if snapshot.MissionInventory[0].MagicOptions[0] != 11 {
		t.Errorf(
			"snapshot magic option = %d, want 11",
			snapshot.MissionInventory[0].MagicOptions[0],
		)
	}
	if *snapshot.World.Spawn.X != 20 || *snapshot.World.AuthoredAreaReturn.X != 30 || *snapshot.World.LastDeathPoint.X != 40 ||
		*snapshot.World.DungeonFloorIndex != 3 || *snapshot.World.PackedInstance != 0x20001 {
		t.Errorf("snapshot world retained mutable pointers: %+v", snapshot.World)
	}
	if snapshot.World.InstanceReturn == nil || snapshot.World.InstanceReturn.PackedInstance != 0x20001 ||
		snapshot.World.InstanceReturn.RegionID != 25000 || snapshot.World.InstanceReturn.X != 12.5 ||
		snapshot.World.InstanceReturn.Y != 3 || snapshot.World.InstanceReturn.Z != -7 || snapshot.World.InstanceReturn.Angle != 90 {
		t.Errorf("snapshot instance return retained mutable pointer: %+v", snapshot.World.InstanceReturn)
	}
	if string(snapshot.World.MoveSegment) != `{"x":1}` {
		t.Errorf(
			"snapshot move segment = %s, want original bytes",
			snapshot.World.MoveSegment,
		)
	}
	if snapshot.ActiveQuests[0].Contents[0].ObjectiveValues[0] != 7 ||
		snapshot.ActiveQuests[0].TargetIds[0] != 8 {
		t.Errorf(
			"snapshot quest retained mutable slices: %+v",
			snapshot.ActiveQuests[0],
		)
	}
	if snapshot.TrackedQuests[0].Tail6[0] != 1 {
		t.Errorf(
			"snapshot tracker tail = %v, want original",
			snapshot.TrackedQuests[0].Tail6,
		)
	}
	if snapshot.Friends[0].Name != "friend" {
		t.Errorf("snapshot friend = %+v, want original", snapshot.Friends[0])
	}
	if snapshot.QuickSlots[0].Payload != 123 {
		t.Errorf("snapshot quickslot = %+v, want detached payload 123", snapshot.QuickSlots[0])
	}
	if snapshot.PetSkillWindows[0].EndUnixMs != 5000 {
		t.Errorf("snapshot pet skill window = %+v, want detached deadline 5000", snapshot.PetSkillWindows[0])
	}
	if snapshot.CompletedQuestIds == nil || len(snapshot.CompletedQuestIds) != 0 {
		t.Errorf(
			"snapshot collapsed non-nil empty slice: %#v",
			snapshot.CompletedQuestIds,
		)
	}
}

/*
================
TestSnapshotEquipmentKeepsOnlyWornRows

The narrow copy: equipment rows deep-copied in order, bag rows gone,
the avatar inventory absent, and every other child Snapshot's. A
record with no worn rows narrows to no rows; a nil inventory stays nil.
================
*/
func TestSnapshotEquipmentKeepsOnlyWornRows(t *testing.T) {
	level := int64(10)
	character := &Character{
		Name:  "narrowOwner",
		Level: &level,
		MissionInventory: []InventoryRow{
			{Slot: 6, Codename: "WEAPON", MagicOptions: []uint64{11}},
			{Slot: 20, Codename: "BAG_A", MagicOptions: []uint64{21}},
			{Slot: 8, Codename: "SUIT"},
			{Slot: 44, Codename: "BAG_B"},
		},
		AvatarInventory: &AvatarInventory{Capacity: 4, Rows: []InventoryRow{{Slot: 0, Codename: "AVATAR"}}},
		Skills:          []uint32{7},
	}

	narrow := character.SnapshotEquipment()
	if narrow.AvatarInventory != nil {
		t.Fatal("narrow copy kept the avatar inventory")
	}
	if len(narrow.MissionInventory) != 2 ||
		narrow.MissionInventory[0].Slot != 6 || narrow.MissionInventory[0].Codename != "WEAPON" ||
		narrow.MissionInventory[1].Slot != 8 || narrow.MissionInventory[1].Codename != "SUIT" {
		t.Fatalf("narrow inventory = %+v", narrow.MissionInventory)
	}
	if narrow.MissionInventory[0].MagicOptions[0] != 11 {
		t.Fatal("narrow copy lost the worn row's magic options")
	}
	// The full copy is untouched by the narrowing.
	full := character.Snapshot()
	if len(full.MissionInventory) != 4 || full.AvatarInventory == nil || len(full.AvatarInventory.Rows) != 1 {
		t.Fatal("the full copy narrowed too")
	}
	// Detachment and normalization match Snapshot's.
	narrow.MissionInventory[0].MagicOptions[0] = 99
	if character.MissionInventory[0].MagicOptions[0] != 11 {
		t.Fatal("narrow copy aliases the record")
	}
	if narrow.StatPoints == nil || *narrow.Level != 10 || len(narrow.Skills) != 1 || narrow.Skills[0] != 7 {
		t.Fatal("narrow copy lost Snapshot's normalized fields")
	}

	bagOnly := &Character{MissionInventory: []InventoryRow{{Slot: 30, Codename: "BAG"}}}
	if got := bagOnly.SnapshotEquipment(); len(got.MissionInventory) != 0 {
		t.Fatalf("bag-only copy kept rows: %+v", got.MissionInventory)
	}
	empty := &Character{}
	if got := empty.SnapshotEquipment(); got.MissionInventory != nil {
		t.Fatal("nil inventory became non-nil")
	}
}

/*
================
TestSnapshotEquipmentDropsBagRowCompanions

A persistent pet's record rides in the mission row of its own item:
companion resolution walks bag rows (VisitCompanions reaches the row
Summon records), so the narrow copy cannot see a summoned pet stored
in the bag. This pins the trap - a SnapshotEquipment reader must never
resolve companions.
================
*/
func TestSnapshotEquipmentDropsBagRowCompanions(t *testing.T) {
	crow := &CharacterCOS{GID: 7001, Codename: "COS_P_RAVEN_090", Summoned: true, CurrentHP: 100}
	character := &Character{
		Name: "bagRowOwner",
		MissionInventory: []InventoryRow{
			{Slot: 6, Codename: "WEAPON"},
			{Slot: 20, Codename: "PET_ITEM", Summon: crow},
		},
	}

	if got := character.Snapshot().CompanionByGID(7001); got == nil {
		t.Fatal("the full copy lost the bag-row crow")
	}
	narrow := character.SnapshotEquipment()
	if got := narrow.CompanionByGID(7001); got != nil {
		t.Fatal("the narrow copy resolved a bag-row companion")
	}
	if len(narrow.Companions()) != 0 {
		t.Fatalf("the narrow copy carries companions: %v", narrow.Companions())
	}
	if len(narrow.MissionInventory) != 1 || narrow.MissionInventory[0].Slot != 6 {
		t.Fatalf("narrow inventory = %+v", narrow.MissionInventory)
	}
}

/*
================
TestSnapshotCastKeepsCompanionsAndDropsPlainBagRows

The press copy: equipment rows and companion-carrying bag rows in
source order, plain bag rows gone, the avatar inventory copied whole,
and a bag-row companion still resolvable through the copy - the trap
TestSnapshotEquipmentDropsBagRowCompanions pins from the other side.
================
*/
func TestSnapshotCastKeepsCompanionsAndDropsPlainBagRows(t *testing.T) {
	crow := &CharacterCOS{GID: 7001, Codename: "COS_P_RAVEN_090", Summoned: true, CurrentHP: 100}
	character := &Character{
		Name: "castOwner",
		MissionInventory: []InventoryRow{
			{Slot: 6, Codename: "WEAPON", MagicOptions: []uint64{11}},
			{Slot: 20, Codename: "POTION", StackCount: 50},
			{Slot: 21, Codename: "PET_ITEM", Summon: crow},
			{Slot: 22, Codename: "ITEM_QSP_TEST_01", StackCount: 1},
		},
		AvatarInventory: &AvatarInventory{Capacity: 4, Rows: []InventoryRow{{Slot: 0, Codename: "AVATAR"}}},
	}

	cast := character.SnapshotCast()
	if len(cast.MissionInventory) != 2 ||
		cast.MissionInventory[0].Slot != 6 || cast.MissionInventory[0].Codename != "WEAPON" ||
		cast.MissionInventory[1].Slot != 21 || cast.MissionInventory[1].Codename != "PET_ITEM" {
		t.Fatalf("cast inventory = %+v", cast.MissionInventory)
	}
	if cast.AvatarInventory == nil || len(cast.AvatarInventory.Rows) != 1 {
		t.Fatalf("cast avatar inventory = %+v, want the one row", cast.AvatarInventory)
	}
	if got := cast.CompanionByGID(7001); got == nil {
		t.Fatal("the cast copy lost the bag-row crow")
	}
	// Detachment matches Snapshot's: the kept companion row is a copy.
	cast.MissionInventory[1].Summon.CurrentHP = 1
	if crow.CurrentHP != 100 {
		t.Fatal("cast copy aliases the record's companion")
	}
	if len(character.Snapshot().MissionInventory) != 4 {
		t.Fatal("the full copy narrowed too")
	}

	bagOnly := &Character{MissionInventory: []InventoryRow{{Slot: 30, Codename: "BAG"}}}
	if got := bagOnly.SnapshotCast(); len(got.MissionInventory) != 0 {
		t.Fatalf("bag-only cast copy kept rows: %+v", got.MissionInventory)
	}
	empty := &Character{}
	if got := empty.SnapshotCast(); got.MissionInventory != nil {
		t.Fatal("nil inventory became non-nil")
	}
}

/*
================
castCargoCharacter

A transport owner for the cast-copy tests: the active transport carries
cargo rows with children and rentals, and a summoner row carries a
pickup pet whose bag holds petBagRows rows.
================
*/
func castCargoCharacter(petBagRows int) *Character {
	transport := &CharacterCOS{
		GID: 8001, Codename: "COS_C_DHORSE1", Summoned: true, CurrentHP: 900,
		Rentals: []COSRental{{ID: 1, RemainingSeconds: 60}},
		Container: &COSContainer{Capacity: 4, Rows: []InventoryRow{
			{Slot: 0, Codename: "ITEM_ETC_TRADE_01", StackCount: 5, TypeFlags: 3, MagicOptions: []uint64{7}},
			{Slot: 1, Codename: "ITEM_ETC_TRADE_02", StackCount: 2},
		}},
	}
	bag := make([]InventoryRow, petBagRows)
	for i := range bag {
		bag[i] = InventoryRow{Slot: int64(i), Codename: "ITEM_ETC_LOOT", StackCount: 1, MagicOptions: []uint64{uint64(i)}}
	}
	pet := &CharacterCOS{
		GID: 8002, Codename: "COS_P_RABBIT", Summoned: true, CurrentHP: 100,
		Rentals:   []COSRental{{ID: 2, RemainingSeconds: 60}},
		Container: &COSContainer{Capacity: 196, Rows: bag},
	}
	return &Character{
		Name:      "cargoOwner",
		ActiveCOS: transport,
		MissionInventory: []InventoryRow{
			{Slot: 6, Codename: "WEAPON"},
			{Slot: 30, Codename: "PET_ITEM", Summon: pet},
		},
	}
}

/*
================
TestSnapshotCastCarriesCompanionsWithoutTheirBags

The press copy's companions (#3164): both still resolve, the active
transport keeps its cargo as detached values (the caravan and trade
tier verdicts weigh codename, stack and type), and nothing else of a
companion's deep children rides along - no rentals, no cargo row
children, no summoner-row bag. The allocation count must not grow
with a summoner-row pet's bag: copying it into every press copy was
the bulk this view drops, and restoring CloneCOS in cloneCastRows
fails here.
================
*/
func TestSnapshotCastCarriesCompanionsWithoutTheirBags(t *testing.T) {
	character := castCargoCharacter(3)
	cast := character.SnapshotCast()

	if cast.CompanionByGID(8001) == nil || cast.CompanionByGID(8002) == nil {
		t.Fatal("the cast copy lost a companion")
	}
	active := cast.ActiveCOS
	if active == character.ActiveCOS || active.Container == nil || active.Container == character.ActiveCOS.Container {
		t.Fatal("the cast copy's active transport is not a detached copy with cargo")
	}
	rows := active.Container.Rows
	if len(rows) != 2 || rows[0].Codename != "ITEM_ETC_TRADE_01" || rows[0].StackCount != 5 || rows[0].TypeFlags != 3 {
		t.Fatalf("cast cargo = %+v, want the two rows as values", rows)
	}
	if rows[0].MagicOptions != nil || active.Rentals != nil {
		t.Fatal("the cast copy carries cargo row children or rentals")
	}
	rows[0].StackCount = 1
	if character.ActiveCOS.Container.Rows[0].StackCount != 5 {
		t.Fatal("the cast cargo aliases the live cargo")
	}
	pet := cast.MissionInventory[1].Summon
	if pet == character.MissionInventory[1].Summon || pet.Container != nil || pet.Rentals != nil {
		t.Fatal("the summoner-row pet rides with its bag or rentals, or is not a copy")
	}

	small := castCargoCharacter(1)
	large := castCargoCharacter(150)
	smallAllocs := testing.AllocsPerRun(50, func() { small.SnapshotCast() })
	largeAllocs := testing.AllocsPerRun(50, func() { large.SnapshotCast() })
	if largeAllocs != smallAllocs {
		t.Fatalf("cast copy allocates %v with a 150-row pet bag, %v with a 1-row bag; want no growth", largeAllocs, smallAllocs)
	}
}

/*
================
TestCloneCharacterStagedSharesFrozenChildren

The store stages one committed copy per save and compares the next
record against it (store, character_sections.go); the staged copy of
a quiet record must cost no more than the record shell, so every
child the previous staged copy already holds in the same value is
shared with it. The sharing must stay invisible: the copy is still
value-equal to a full clone and detached from the live record, and a
child the record moved is copied again, never shared.
================
*/
func TestCloneCharacterStagedSharesFrozenChildren(t *testing.T) {
	source := clonePopulatedCharacter()
	previous := CloneCharacter(source)

	staged := CloneCharacterStaged(source, previous)
	if !reflect.DeepEqual(staged, CloneCharacter(source)) {
		t.Fatal("the staged copy differs from the full clone")
	}
	if staged.MissionInventory == nil || &staged.MissionInventory[0] != &previous.MissionInventory[0] {
		t.Fatal("the staged copy re-copied the quiet inventory")
	}
	if staged.ActiveCOS != previous.ActiveCOS {
		t.Fatal("the staged copy re-copied the quiet companion")
	}
	if staged.Skills == nil || &staged.Skills[0] != &previous.Skills[0] {
		t.Fatal("the staged copy re-copied the quiet skills")
	}
	var shared []string
	sharedMutableNodes(reflect.ValueOf(source), reflect.ValueOf(staged), "Character", 0, &shared)
	if len(shared) > 0 {
		t.Fatalf("the staged copy shares mutable nodes with the record: %s", strings.Join(shared, ", "))
	}

	// A write through the live record does not cross into the copy
	// that shares the previous copy's children.
	source.MissionInventory[0].MagicOptions[0] = 99
	*source.Level = 99
	if staged.MissionInventory[0].MagicOptions[0] == 99 || *staged.Level == 99 {
		t.Fatal("a record write crossed into the staged copy")
	}

	// The moved children are copied again; the quiet ones stay shared.
	source.Skills[0] = 77
	next := CloneCharacterStaged(source, previous)
	if next.Skills[0] != 77 || &next.Skills[0] == &previous.Skills[0] {
		t.Fatal("the staged copy shared the moved skills")
	}
	if next.MissionInventory[0].MagicOptions[0] != 99 ||
		&next.MissionInventory[0] == &previous.MissionInventory[0] {
		t.Fatal("the staged copy shared the moved inventory")
	}
	if &next.PetSkillWindows[0] != &previous.PetSkillWindows[0] {
		t.Fatal("the staged copy lost a quiet child's sharing")
	}

	// No previous copy stages a plain full clone.
	fresh := CloneCharacterStaged(source, nil)
	if !reflect.DeepEqual(fresh, CloneCharacter(source)) {
		t.Fatal("the previous-less staged copy differs from the full clone")
	}
}

/*
================
BenchmarkCloneCharacterStaged

One staged copy of a quiet record against its previous staged copy;
BenchmarkSnapshotFull is the same record's full clone (#2030: most
saves move a scalar while the record's bulk compares equal).
================
*/
func BenchmarkCloneCharacterStaged(b *testing.B) {
	c := benchmarkCombatCharacter()
	previous := CloneCharacter(c)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = CloneCharacterStaged(c, previous)
	}
}

/*
================
benchmarkTransportCharacter

The combat character riding a full trade transport: a companion
whose container holds 177 trade rows, the shape that made the
staging clone the live tick's largest allocator (#2030).
================
*/
func benchmarkTransportCharacter() *Character {
	c := benchmarkCombatCharacter()
	container := &COSContainer{}
	for slot := int64(0); slot < 177; slot++ {
		container.Rows = append(container.Rows, InventoryRow{Slot: slot, RefObjID: uint32(2000 + slot), Codename: "ITEM_TRADE_BENCH", StackCount: 40, MagicOptions: []uint64{1}})
	}
	c.ActiveCOS = &CharacterCOS{GID: 42, Codename: "COS_TRANSPORT_BENCH", Container: container}
	return c
}

/*
================
BenchmarkCloneCharacterStagedTransport
================
*/
func BenchmarkCloneCharacterStagedTransport(b *testing.B) {
	c := benchmarkTransportCharacter()
	previous := CloneCharacter(c)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = CloneCharacterStaged(c, previous)
	}
}

/*
================
BenchmarkSnapshotTransport
================
*/
func BenchmarkSnapshotTransport(b *testing.B) {
	c := benchmarkTransportCharacter()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.Snapshot()
	}
}

/*
================
benchmarkCombatCharacter

A character shaped like the tick guard's players (#1943): 13 equipment
rows and a 35-row bag, 48 rows in all, an avatar row and 20 skills.
================
*/
func benchmarkCombatCharacter() *Character {
	level, hp := int64(80), int64(5000)
	c := &Character{ID: 1, Name: "bench", Level: &level, CurrentHP: &hp}
	for slot := int64(0); slot < 48; slot++ {
		c.MissionInventory = append(c.MissionInventory, InventoryRow{Slot: slot, RefObjID: uint32(1000 + slot), Codename: "ITEM_BENCH", StackCount: 1, MagicOptions: []uint64{1, 2, 3}})
	}
	c.AvatarInventory = &AvatarInventory{Rows: []InventoryRow{{Slot: 0, RefObjID: 7, Codename: "ITEM_AVATAR_BENCH"}}}
	for skill := uint32(1); skill <= 20; skill++ {
		c.Skills = append(c.Skills, skill)
	}
	return c
}

/*
================
BenchmarkSnapshotFull
================
*/
func BenchmarkSnapshotFull(b *testing.B) {
	c := benchmarkCombatCharacter()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.Snapshot()
	}
}

/*
================
clonePopulatedCharacter

A record with every mutable child present, so the detachment walk
below has a node to check at each reference. When Character gains a
pointer, slice or map child, populate it here: the test refuses a
fixture that leaves one of the record's own reference fields unset.
================
*/
func clonePopulatedCharacter() *Character {
	i64p := func(v int64) *int64 { return &v }
	u32p := func(v uint32) *uint32 { return &v }
	f64p := func(v float64) *float64 { return &v }
	spawn := func() *WorldSpawn {
		return &WorldSpawn{RegionID: i64p(1), X: f64p(1), Y: f64p(2), Z: f64p(3), Angle: i64p(4)}
	}
	row := func() InventoryRow {
		return InventoryRow{
			MagicOptions: []uint64{1},
			Summon:       &CharacterCOS{GID: 5, Container: &COSContainer{Rows: []InventoryRow{{Slot: 1}}}},
		}
	}
	cos := func() *CharacterCOS {
		return &CharacterCOS{
			GID:       6,
			Rentals:   []COSRental{{ID: 1}},
			Container: &COSContainer{Rows: []InventoryRow{row()}},
		}
	}
	mask := int64(7)
	return &Character{
		PK:              &PKRecord{DailyCount: 1},
		Online:          &OnlineRecord{TotalSeconds: 60, Days: []OnlineDay{{Day: 20260101, Seconds: 60}}},
		EventMembership: &EventMembership{ID: 1},
		Aggressions:     map[uint32]uint32{1: 1},
		Mission:         &MissionRuntime{EventGuideStateMask: &mask, EventGuideStateMaskUpdatedAt: "t"},
		MostWanted:      &MostWanted{Streak: 1},
		World: &CharacterWorld{
			SavedReturn:        &SavedReturnLocation{Definition: 1},
			PackedInstance:     u32p(1),
			InstanceReturn:     &InstanceReturnAnchor{PackedInstance: 1},
			Spawn:              spawn(),
			AuthoredAreaReturn: spawn(),
			RebirthPoint:       spawn(),
			LastRecallPoint:    &WorldPoint{WorldSpawn: *spawn()},
			LastDeathPoint:     &WorldPoint{WorldSpawn: *spawn()},
			MovementMode:       i64p(3),
			DungeonFloorIndex:  i64p(1),
			MoveSegment:        []byte{1},
		},
		ActiveCOS:               cos(),
		CapturedCOS:             cos(),
		Mercenaries:             []*CharacterCOS{cos()},
		Brood:                   []*CharacterCOS{cos()},
		MissionInventory:        []InventoryRow{row()},
		Buyback:                 []BuybackEntry{{Item: row()}},
		AvatarInventory:         &AvatarInventory{Capacity: 1, Rows: []InventoryRow{row()}},
		PetSkillWindows:         []PetSkillWindow{{ItemRefObjID: 1}},
		ParamJobs:               []ParamJob{{ItemRefObjID: 1}},
		CompositeJobs:           []CompositeJob{{Kind: "UIL1"}},
		PremiumClock:            &PremiumClock{ItemRefObjID: 1},
		LevelReachedAt:          map[uint8]int64{1: 1},
		ItemGroupCooldowns:      map[uint32]int64{1: 1},
		TimedSkillJobs:          []TimedSkillJob{{SkillID: 1}},
		Masteries:               []CharacterMastery{{ID: 1}},
		Skills:                  []uint32{1},
		OffensiveSkillCooldowns: map[uint32]int64{1: 1},
		SharedSkillCooldowns:    map[uint8]int64{1: 1},
		QuickSlots:              []QuickSlotBinding{{Slot: 1}},
		CompletedQuestIds:       []uint32{1},
		QuestCompletionCounts:   map[uint32]uint32{1: 1},
		QuestSupplies:           map[uint32]QuestSupplyState{1: {Day: 1}},
		EndedQuestIds:           []uint32{1},
		ActiveQuests: []ActiveQuestRecord{{
			RefID:     1,
			Contents:  []ActiveQuestContentsNode{{Tag: 1, ObjectiveValues: []uint32{1}}},
			TargetIds: []uint32{1},
		}},
		TrackedQuests:      []TrackedQuestRecord{{RefID: 1, Tail6: []uint8{1}}},
		EnterEventGroupIds: []uint32{1},
		BlockedWhisperers:  []string{"a"},
		Friends:            []FriendRecord{{ID: 1}},
		RaceIndex:          i64p(1),
		Gender:             i64p(1),
		ModelRef:           i64p(1),
		WeaponIndex:        i64p(1),
		ProtectorIndex:     i64p(1),
		BodyShapeByte:      i64p(1),
		HeightIndex:        i64p(1),
		VolumeIndex:        i64p(1),
		HeightScale:        f64p(1),
		VolumeScale:        f64p(1),
		Level:              i64p(1),
		MaxLevel:           i64p(1),
		CurrentHP:          i64p(1),
		CurrentMP:          i64p(1),
		SkillPoints:        i64p(1),
		StatPoints:         i64p(1),
		Experience:         i64p(1),
		SkillExp:           i64p(1),
		Gold:               i64p(1),
		VisualFlags:        i64p(1),
		Strength:           i64p(1),
		Intellect:          i64p(1),
		GuildID:            i64p(1),
		ExperiencePercent:  f64p(1),
		FigureIndex:        i64p(1),
	}
}

/*
================
sharedMutableNodes

Walk a record and its clone in parallel, collecting the paths of the
mutable nodes they still share: the same pointer, the same slice
backing, the same map. Scalars and empty slices share nothing a write
can cross.
================
*/
func sharedMutableNodes(a, b reflect.Value, path string, depth int, out *[]string) {
	if depth > 16 || !a.IsValid() || !b.IsValid() || a.Type() != b.Type() {
		return
	}
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return
		}
		if a.Pointer() == b.Pointer() {
			*out = append(*out, path)
			return
		}
		sharedMutableNodes(a.Elem(), b.Elem(), path, depth+1, out)
	case reflect.Slice:
		if a.Len() == 0 {
			return
		}
		if a.Pointer() == b.Pointer() {
			*out = append(*out, path)
			return
		}
		for i := 0; i < a.Len() && i < b.Len(); i++ {
			sharedMutableNodes(a.Index(i), b.Index(i), path+"[]", depth+1, out)
		}
	case reflect.Map:
		if a.IsNil() || b.IsNil() {
			return
		}
		if a.Pointer() == b.Pointer() {
			*out = append(*out, path)
			return
		}
		iter := a.MapRange()
		for iter.Next() {
			sharedMutableNodes(iter.Value(), b.MapIndex(iter.Key()), path+"{}", depth+1, out)
		}
	case reflect.Struct:
		fields := a.Type()
		for i := 0; i < a.NumField(); i++ {
			sharedMutableNodes(a.Field(i), b.Field(i), path+"."+fields.Field(i).Name, depth+1, out)
		}
	case reflect.Array:
		for i := 0; i < a.Len(); i++ {
			sharedMutableNodes(a.Index(i), b.Index(i), path+"[]", depth+1, out)
		}
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return
		}
		sharedMutableNodes(a.Elem(), b.Elem(), path, depth+1, out)
	}
}

/*
================
TestCloneCharacterDetachesEveryMutableChild

CloneCharacter's copies are the section skip's ground truth (store,
character_sections.go): a mutable node the clone still shares with
the record is one an in-place write can cross without the comparison
noticing, and the save is lost. Refuse every shared node. The
fixture must populate each of the record's own reference fields, so
a child added later fails here first instead of slipping past empty.
================
*/
func TestCloneCharacterDetachesEveryMutableChild(t *testing.T) {
	source := clonePopulatedCharacter()
	characterType := reflect.TypeOf(Character{})
	sourceValue := reflect.ValueOf(source).Elem()
	for i := 0; i < characterType.NumField(); i++ {
		field := sourceValue.Field(i)
		switch field.Kind() {
		case reflect.Pointer, reflect.Map:
			if field.IsNil() {
				t.Fatalf("fixture leaves Character.%s unset", characterType.Field(i).Name)
			}
		case reflect.Slice:
			if field.Len() == 0 {
				t.Fatalf("fixture leaves Character.%s empty", characterType.Field(i).Name)
			}
		}
	}
	var shared []string
	sharedMutableNodes(reflect.ValueOf(source), reflect.ValueOf(CloneCharacter(source)), "Character", 0, &shared)
	if len(shared) > 0 {
		t.Fatalf("the clone shares mutable nodes with the record: %s", strings.Join(shared, ", "))
	}
}

/*
================
BenchmarkSnapshotCombat
================
*/
func BenchmarkSnapshotCombat(b *testing.B) {
	c := benchmarkCombatCharacter()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.SnapshotCombat()
	}
}

/*
================
BenchmarkSnapshotCast
================
*/
func BenchmarkSnapshotCast(b *testing.B) {
	c := benchmarkCombatCharacter()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.SnapshotCast()
	}
}

/*
================
TestCloneCharacterStagedSharesQuietInventoryRows

One touched row in a wide bag: the next staged copy re-clones that row
alone and shares every quiet row's children with the previous copy
(#3164). The staged copy still equals a full clone, and a write through
the live record crosses into neither the shared nor the cloned rows.
================
*/
func TestCloneCharacterStagedSharesQuietInventoryRows(t *testing.T) {
	source := clonePopulatedCharacter()
	for slot := len(source.MissionInventory); slot < 40; slot++ {
		source.MissionInventory = append(source.MissionInventory, InventoryRow{
			Slot:         int64(slot),
			StackCount:   1,
			MagicOptions: []uint64{uint64(slot)},
		})
	}
	previous := CloneCharacterStaged(source, nil)

	source.MissionInventory[20].StackCount = 7
	staged := CloneCharacterStaged(source, previous)
	if !reflect.DeepEqual(staged, CloneCharacter(source)) {
		t.Fatal("the staged copy differs from the full clone")
	}
	quiet := staged.MissionInventory[10]
	before := previous.MissionInventory[10]
	if len(quiet.MagicOptions) == 0 || &quiet.MagicOptions[0] != &before.MagicOptions[0] {
		t.Fatal("a quiet row was re-cloned instead of shared")
	}
	touched := staged.MissionInventory[20]
	if touched.StackCount != 7 || &touched.MagicOptions[0] == &previous.MissionInventory[20].MagicOptions[0] {
		t.Fatal("the touched row was shared instead of re-cloned")
	}
	source.MissionInventory[10].MagicOptions[0] = 99
	source.MissionInventory[20].MagicOptions[0] = 99
	if staged.MissionInventory[10].MagicOptions[0] == 99 || staged.MissionInventory[20].MagicOptions[0] == 99 {
		t.Fatal("a record write crossed into the staged copy")
	}
}

/*
================
BenchmarkCloneCharacterStagedInventoryChurn

The staged copy of a 120-row bag when one row moved since the previous
stage: the live shape behind #3164's inventory clone bytes.
================
*/
func BenchmarkCloneCharacterStagedInventoryChurn(b *testing.B) {
	source := clonePopulatedCharacter()
	for slot := len(source.MissionInventory); slot < 120; slot++ {
		source.MissionInventory = append(source.MissionInventory, InventoryRow{
			Slot:         int64(slot),
			StackCount:   1,
			MagicOptions: []uint64{uint64(slot)},
			Summon:       &CharacterCOS{GID: uint32(slot), Container: &COSContainer{Rows: []InventoryRow{{Slot: 1}}}},
		})
	}
	previous := CloneCharacterStaged(source, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		source.MissionInventory[60].StackCount = int64(i)
		previous = CloneCharacterStaged(source, previous)
	}
}
