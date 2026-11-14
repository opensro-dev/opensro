/*
===========================================================================

hunting_guide.go - publication of the static hunting atlas

The atlas (each monster's spawn anchors, from the population template)
is published on every shard as spawnAtlas: the M window's monster list,
level filter and region pages (#3167, the later client's world map
guide, native by the later-client rule) read it (#3489). The same file
is also published as huntingGuide only while the port-only
SRO_HUNTING_GUIDE is on (off by default): that key is what offers the
portrait overlay, the Experimental "Monster hunting guide" row. A client
older than spawnAtlas reads only huntingGuide, so it keeps seeing no
atlas on a shard with the flag off.

===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"opensro.online/server/internal/game/world/monster"
)

const EnvHuntingGuide = "SRO_HUNTING_GUIDE"
const maxHuntingGuideBytes = 2 << 20

/*
================
HuntingGuideSource

Lazy access to the immutable population projection, never live creatures.
================
*/
type HuntingGuideSource interface {
	HuntingGuide() []monster.HuntingGuideEntry
}

/*
================
HuntingGuideFromEnv

Whether SRO_HUNTING_GUIDE publishes the guide: "on", "1" or "true". Anything
else, unset included, is the native default (no guide).
================
*/
func HuntingGuideFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvHuntingGuide))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
newSpawnAtlasReference

The atlas file every shard publishes, or nil when the population has no
rows (a test world).
================
*/
func newSpawnAtlasReference(source HuntingGuideSource) (*BrowserReferences, error) {
	if source == nil {
		return nil, nil
	}
	rows := source.HuntingGuide()
	if len(rows) == 0 {
		return nil, nil
	}
	guide, err := json.Marshal(struct {
		Format  string                      `json:"format"`
		Version int                         `json:"version"`
		Rows    []monster.HuntingGuideEntry `json:"rows"`
	}{"sro-hunting-guide", 1, rows})
	if err != nil {
		return nil, err
	}
	if len(guide) > maxHuntingGuideBytes {
		return nil, fmt.Errorf("hunting guide exceeds resource budget")
	}
	return newBrowserReferenceFile(guide)
}
