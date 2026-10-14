/*
===========================================================================

hunting_guide.go - opt-in publication of the static hunting atlas

Port-only, not native. SRO_HUNTING_GUIDE is off by default. Disabled shards
never ask the population owner to build the guide and publish no reference.

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
newHuntingGuideReference
================
*/
func newHuntingGuideReference(source HuntingGuideSource) (*BrowserReferences, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvHuntingGuide))) {
	case "on", "1", "true":
	default:
		return nil, nil
	}
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
