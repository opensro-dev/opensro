/*
===========================================================================

check.go - does this server build accept this server game data?

A server release reads the server game-data archive installed on the host,
and the archive does not ship with the release. The 2026-10-05 release met
an archive a week older than its code ("unsupported character-authority
catalogue v2", then missing structure zones): the GameWorld crash-looped
after the fleet had stopped and the store upgraded. The release deploy now
runs this check with the candidate's own code against the archive it will
use, before any notice or stop.

Check loads every static game-data owner the GameWorld builds at startup,
through the same loaders and in the same order (sro-gameworld wiring.go,
wiring_authority.go, wiring_gameplay.go), and nothing else: no store, no
Agent, no listener. A new data owner in GameWorld startup belongs here too.

===========================================================================
*/

package gamedatacheck

import (
	"fmt"
	"path/filepath"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/world/worldarea"
	"opensro.online/server/internal/gamedata"
)

/*
================
Result

What the check opened: the archive identity it verified.
================
*/
type Result struct {
	BundleRoot     string
	ManifestDigest string
}

/*
================
Check

Resolves and verifies the archive SRO_SERVER_GAME_DATA_ROOT names, then
builds every static owner the GameWorld builds before it opens its store.
================
*/
func Check() (Result, error) {
	dataPaths, err := gamedata.Resolve()
	if err != nil {
		return Result{}, fmt.Errorf("game data: %w", err)
	}
	result := Result{BundleRoot: dataPaths.BundleRoot, ManifestDigest: dataPaths.ManifestDigest}
	authoredAreas, err := worldarea.LoadAuthority(dataPaths.WorldAuthorityDir)
	if err != nil {
		return result, fmt.Errorf("authored world areas: %w", err)
	}
	devPaths := enterworld.DevPathsFromEnv(dataPaths.CharacterAuthorityDir, dataPaths.TextdataDir)
	devPaths.AuthoredAreas = authoredAreas
	devPaths.StructureZones = filepath.Join(dataPaths.WorldAuthorityDir, "structure-zones.json")
	roster, err := enterworld.LoadRoster(devPaths.RosterPath)
	if err != nil {
		return result, fmt.Errorf("character roster: %w", err)
	}
	textdata, err := enterworld.LoadTextdataCatalogs(devPaths.TextdataDir)
	if err != nil {
		return result, fmt.Errorf("textdata: %w", err)
	}
	questCatalog := quest.NewCatalog(devPaths.TextdataDir)
	if _, err := quest.LoadDefinitions(questCatalog, enterworld.NewTextdataItems(questCatalog.Dir())); err != nil {
		return result, fmt.Errorf("quests: %w", err)
	}
	deps, err := enterworld.NewDevDepsWithRoster(devPaths, textdata, roster)
	if err != nil {
		return result, fmt.Errorf("bootstrap dependencies: %w", err)
	}
	items := action.NewRuntime(deps, deps.MonsterState)
	items.Guilds = deps.Guilds
	items.UnlimitedItems = enterworld.StarterKitCodenames(deps.StarterKit)
	if err := items.ValidateLootReferences(); err != nil {
		return result, fmt.Errorf("loot catalogue: %w", err)
	}
	for _, step := range []struct {
		name      string
		configure func(string) error
	}{
		{"portal catalogue", items.ConfigurePortals},
		{"alchemy catalogue", items.ConfigureAlchemy},
		{"gacha catalogue", items.ConfigureGacha},
		{"stall network", items.ConfigureStallNetwork},
		{"commerce catalogue", items.ConfigureCommerce},
	} {
		if err := step.configure(devPaths.TextdataDir); err != nil {
			return result, fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return result, nil
}
