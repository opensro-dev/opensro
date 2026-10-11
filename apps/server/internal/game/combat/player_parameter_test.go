/*
===========================================================================

player_parameter_test.go - one unlinked parameter equals the full projection

PlayerUnlinkedParameter skips equipment, passives and magic options for a
parameter no other parameter feeds. On shipped data that must change
nothing: for random characters, gear, worn blessed avatars (#656), learned
skills, effect modifiers and abnormal blocks, it answers what
PlayerStatsWithModifiers does.

===========================================================================
*/
package combat

import (
	"math/rand"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

const (
	// unlinkedActionSpeed is the parameter the entry snapshot reads (0x8C).
	unlinkedActionSpeed = 0x8c
	// unlinkedSamples random characters, of which at least unlinkedCompared
	// must project (random writes to undefined parameters do not);
	// unlinkedSkillProbe the skill ids probed for shipped rows.
	unlinkedSamples    = 1500
	unlinkedCompared   = 400
	unlinkedSkillProbe = 40000
	// unlinkedOptionProbe the magic option ids probed for avatar options.
	unlinkedOptionProbe = 4096
	// unlinkedAvatarSockets is the avatar container's capacity.
	unlinkedAvatarSockets = 4
	// unlinkedEffectSource is where installed effect sources start.
	unlinkedEffectSource = 0x80000000
)

/*
================
unlinkedCatalog

The shipped item and skill references the samples draw from: equipment
(item type 3/1) and skill ids, passives listed apart.
================
*/
type unlinkedCatalog struct {
	equipment []enterworld.ItemCommandReference
	avatars   []enterworld.ItemCommandReference
	options   []uint32
	skills    []uint32
	passives  []uint32
}

/*
================
loadUnlinkedCatalog
================
*/
func loadUnlinkedCatalog(t *testing.T, items *enterworld.TextdataItems, skills *enterworld.TextdataSkills, options enterworld.MagicOptionSource) unlinkedCatalog {
	t.Helper()
	var out unlinkedCatalog
	for _, ref := range items.ItemCommandReferences() {
		if ref.TypeFlags&0x7c == 0x2c {
			out.equipment = append(out.equipment, ref)
		}
		if full, ok := items.ItemRefByCodename(ref.Codename); ok && full.TypeIDs[0] == avatarTypeID1 &&
			full.TypeIDs[1] == avatarTypeID2 && full.TypeIDs[2] == avatarTypeID3 {
			out.avatars = append(out.avatars, ref)
		}
	}
	for id := uint32(1); id < unlinkedOptionProbe; id++ {
		if row, ok := options.MagicOptionByParamID(id); ok && strings.HasPrefix(row.OptionName, "MATTR_AVATAR_") {
			out.options = append(out.options, id)
		}
	}
	for id := uint32(1); id < unlinkedSkillProbe; id++ {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		out.skills = append(out.skills, id)
		if row.PassiveParameters.Pinned {
			out.passives = append(out.passives, id)
		}
	}
	if len(out.equipment) == 0 || len(out.passives) == 0 || len(out.avatars) == 0 || len(out.options) == 0 {
		t.Fatalf("shipped catalog: %d equipment, %d passives, %d avatars, %d avatar options",
			len(out.equipment), len(out.passives), len(out.avatars), len(out.options))
	}
	return out
}

/*
================
randomUnlinkedCharacter

A character with random level and stats, up to 13 equipped items, up to
four worn avatars with up to three blessings each, and 40 learned skills,
half of them passives.
================
*/
func randomUnlinkedCharacter(rng *rand.Rand, catalog unlinkedCatalog) *domain.Character {
	level := int64(1 + rng.Intn(90))
	strength, intellect := int64(20+rng.Intn(500)), int64(20+rng.Intn(500))
	c := &domain.Character{Level: &level, Strength: &strength, Intellect: &intellect}
	for slot := int64(0); slot < 13; slot++ {
		if rng.Intn(3) == 0 {
			continue
		}
		ref := catalog.equipment[rng.Intn(len(catalog.equipment))]
		c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{
			Slot: slot, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags,
			Plus: int64(rng.Intn(10)), VarianceBits: "0", Durability: int64(rng.Intn(100)),
		})
	}
	if rng.Intn(2) == 0 {
		c.AvatarInventory = &domain.AvatarInventory{Capacity: unlinkedAvatarSockets}
		for slot := int64(0); slot < unlinkedAvatarSockets; slot++ {
			if rng.Intn(2) == 0 {
				continue
			}
			ref := catalog.avatars[rng.Intn(len(catalog.avatars))]
			row := domain.InventoryRow{Slot: slot, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags, VarianceBits: "0"}
			for n := rng.Intn(4); n > 0; n-- {
				option := catalog.options[rng.Intn(len(catalog.options))]
				row.MagicOptions = append(row.MagicOptions, uint64(1+rng.Intn(10))<<32|uint64(option))
			}
			c.AvatarInventory.Rows = append(c.AvatarInventory.Rows, row)
		}
	}
	for i := 0; i < 40; i++ {
		pool := catalog.skills
		if i%2 == 0 {
			pool = catalog.passives
		}
		c.Skills = append(c.Skills, pool[rng.Intn(len(pool))])
	}
	return c
}

/*
================
randomUnlinkedModifiers

Installed effect writes, some to action speed, some to other parameters,
each with its own source identity.
================
*/
func randomUnlinkedModifiers(rng *rand.Rand) []paramkeeper.Write {
	var out []paramkeeper.Write
	for i := 0; i < rng.Intn(6); i++ {
		parameter := uint16(rng.Intn(0x97))
		if rng.Intn(3) == 0 {
			parameter = unlinkedActionSpeed
		}
		out = append(out, paramkeeper.Write{
			Parameter: parameter,
			Channel:   paramkeeper.Channel(rng.Intn(4)),
			Source:    unlinkedEffectSource + uint32(i),
			Value:     float32(rng.Intn(300)),
		})
	}
	return out
}

/*
================
randomUnlinkedBlock

An abnormal block as the callbacks leave it: frostbite's 8C base
replacement, slow's 8C factor, and writes to other parameters.
================
*/
func randomUnlinkedBlock(rng *rand.Rand) *abnormal.Block {
	if rng.Intn(4) == 0 {
		return nil
	}
	block := &abnormal.Block{}
	next := 0
	add := func(m abnormal.Modifier) {
		if next < len(block.Modifiers) {
			block.Modifiers[next] = m
			next++
		}
	}
	if rng.Intn(2) == 0 {
		add(abnormal.Modifier{Used: true, Param: unlinkedActionSpeed, Channel: 0, Source: 0, Value: 200})
	}
	if rng.Intn(2) == 0 {
		add(abnormal.Modifier{Used: true, Param: unlinkedActionSpeed, Channel: 3, Source: 5, Value: 125})
	}
	if rng.Intn(2) == 0 {
		add(abnormal.Modifier{Used: true, Param: 0xb4, Channel: 2, Source: 5, Value: float32(rng.Intn(50))})
	}
	return block
}

/*
================
TestUnlinkedParameterMatchesFullProjection
================
*/
func TestUnlinkedParameterMatchesFullProjection(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items, skills := enterworld.NewTextdataItems(dir), enterworld.NewTextdataSkills(dir)
	options := enterworld.NewTextdataMagicOptions(dir)
	catalog := loadUnlinkedCatalog(t, items, skills, options)
	catalogs := Catalogs{Items: items, Skills: skills, MagicOptions: options}
	rng := rand.New(rand.NewSource(20261011))
	compared := 0
	for sample := 0; sample < unlinkedSamples; sample++ {
		c := randomUnlinkedCharacter(rng, catalog)
		modifiers, block := randomUnlinkedModifiers(rng), randomUnlinkedBlock(rng)
		stats, _, err := PlayerStatsWithModifiers(c, catalogs, modifiers, block)
		if err != nil {
			continue
		}
		want, _ := stats.Param(unlinkedActionSpeed)
		got, err := PlayerUnlinkedParameter(c, modifiers, block, unlinkedActionSpeed)
		if err != nil || got != want {
			t.Fatalf("sample %d: unlinked %v (%v), full projection %v\ncharacter %+v\nmodifiers %+v\nblock %+v",
				sample, got, err, want, c, modifiers, block)
		}
		compared++
	}
	// Enough random loadouts must project, or the comparison proves little.
	if compared < unlinkedCompared {
		t.Fatalf("only %d of %d samples projected", compared, unlinkedSamples)
	}
}

/*
================
TestUnlinkedParameterRefusesALinkedOne

Max HP (3) is fed by strength through 54/52: it cannot be read alone.
================
*/
func TestUnlinkedParameterRefusesALinkedOne(t *testing.T) {
	c := &domain.Character{Level: pointer(20), Strength: pointer(60), Intellect: pointer(60)}
	if _, err := PlayerUnlinkedParameter(c, nil, nil, 3); err == nil {
		t.Fatal("a linked parameter was evaluated alone")
	}
	if got, err := PlayerUnlinkedParameter(c, nil, nil, unlinkedActionSpeed); err != nil || got != 100 {
		t.Fatalf("plain action speed %v (%v), want the built-in 100", got, err)
	}
}
