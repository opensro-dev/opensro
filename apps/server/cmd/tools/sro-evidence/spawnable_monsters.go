/*
===========================================================================

spawnable_monsters.go - the monster roster the asset bake must cover

Prints, as JSON, every monster reference the GameWorld seeds into the
refObjSnapshot: the npcpos nests behind the native CICMonster TypeID gate
plus the unique encounter summon closure (enterworld.
WithMonsterSummonReferences), so asset builders need no classifier of
their own and the bake matches the runtime (#369).

===========================================================================
*/
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
monsterRosterRef
================
*/
type monsterRosterRef struct {
	RefObjID           uint32 `json:"refObjId"`
	Codename           string `json:"codename"`
	RideModelPath      string `json:"rideModelPath,omitempty"`
	RiderTransformMode uint8  `json:"riderTransformMode,omitempty"`
}

/*
================
monsterRosterOutput
================
*/
type monsterRosterOutput struct {
	Format      string             `json:"format"`
	Source      string             `json:"source"`
	TextdataDir string             `json:"textdataDir"`
	Count       int                `json:"count"`
	Refs        []monsterRosterRef `json:"refs"`
}

/*
================
runSpawnableMonsters
================
*/
func runSpawnableMonsters(args []string) error {
	dir, err := resolveEvidenceTextdataDir("spawnable-monsters", args)
	if err != nil {
		return fmt.Errorf("textdata: %w", err)
	}
	spawnable, err := spawnableMonsterRefs(dir)
	if err != nil {
		return err
	}
	output := monsterRosterOutput{
		Format:      "sro-spawnable-monster-roster",
		Source:      "monster.LoadTemplate: npcpos.txt spawn points joined with the binary CICMonster TypeID gate ((W & 0x7FE) == 0x0C6), plus enterworld.WithMonsterSummonReferences",
		TextdataDir: dir,
		Count:       len(spawnable),
		Refs:        make([]monsterRosterRef, 0, len(spawnable)),
	}
	for _, ref := range spawnable {
		output.Refs = append(output.Refs, monsterRosterRef{
			RefObjID:           ref.RefObjID,
			Codename:           ref.Codename,
			RideModelPath:      ref.RideModelPath,
			RiderTransformMode: ref.RiderTransformMode,
		})
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return nil
}

/*
================
spawnableMonsterRefs

The runtime's spawnable set for textdata dir: the nest references and the
summon closure the GameWorld adds at boot.
================
*/
func spawnableMonsterRefs(dir string) ([]monster.MonsterRef, error) {
	skills := enterworld.NewTextdataSkills(dir)
	if err := skills.Load(); err != nil {
		return nil, fmt.Errorf("skilldata: %w", err)
	}
	template, err := enterworld.WithMonsterSummonReferences(monster.LoadTemplate(dir), skills)
	if err != nil {
		return nil, fmt.Errorf("summon references: %w", err)
	}
	return template.SpawnableRefs(), nil
}
