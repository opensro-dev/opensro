/*
===========================================================================

quest_npc_identities_test.go - every quest NPC is a character the release ships

The GameWorld refuses to boot when a quest's start, end or delivery NPC
has no single authored placement (validateQuestMarkerRoster). That check
runs only on the release data, so a misspelt NPC in an authored spec
passed CI and stopped the r204k boot (the Arabia farmers). This test
holds every authored spec to the published character identities
(scripts/data/published-identities.json, refreshed from each published
catalogue), which name every character the release ships.

===========================================================================
*/
package quest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// publishedIdentitiesPath is the committed identity pin, from this package.
const publishedIdentitiesPath = "../../../../../scripts/data/published-identities.json"

/*
================
publishedCharacters

The character codenames the published identity pin carries.
================
*/
func publishedCharacters(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(publishedIdentitiesPath))
	if err != nil {
		t.Fatal(err)
	}
	var pin struct {
		Characters map[string]uint32 `json:"characters"`
	}
	if err := json.Unmarshal(raw, &pin); err != nil {
		t.Fatal(err)
	}
	if len(pin.Characters) == 0 {
		t.Fatal("the published identity pin carries no characters")
	}
	known := make(map[string]bool, len(pin.Characters))
	for code := range pin.Characters {
		known[code] = true
	}
	return known
}

/*
================
TestEveryQuestNpcIsAPublishedCharacter
================
*/
func TestEveryQuestNpcIsAPublishedCharacter(t *testing.T) {
	known := publishedCharacters(t)
	curated, additional, err := authoredQuestSpecs()
	if err != nil {
		t.Fatal(err)
	}
	missing := map[string][]string{}
	check := func(quest string, codes ...string) {
		for _, code := range codes {
			if code != "" && !known[code] {
				missing[code] = append(missing[code], quest)
			}
		}
	}
	for _, spec := range append(curated, additional...) {
		check(spec.Codename, spec.StartNpcCodename, spec.EndNpcCodename, spec.DeliveryNpcCodename)
		for _, stage := range spec.Stages {
			check(spec.Codename, stage.StartNpcCodename, stage.EndNpcCodename, stage.DeliveryNpcCodename)
		}
	}
	codes := make([]string, 0, len(missing))
	for code := range missing {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		t.Errorf("%s (named by %v) is no published character", code, missing[code])
	}
}
