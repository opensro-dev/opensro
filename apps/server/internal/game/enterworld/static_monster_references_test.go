/*
===========================================================================

static_monster_references_test.go - every creatable monster in the reference file

The browser knows a monster only through a refObjSnapshot row. The native
client holds the whole characterdata, so a GM's LOADMONSTER or a quest
script spawn draws there; the port publishes every creatable monster in
the cached reference file instead (#369), and a login repeats none of them.

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestPublishedMonstersLeaveTheLogin

A login's own rows keep everything the file lacks and drop what it holds;
together they are the detached snapshot.
================
*/
func TestPublishedMonstersLeaveTheLogin(t *testing.T) {
	character := chinaSpearman()
	deps := testDeps(character)
	published := RefObjRow{RefObjID: 9001, TidWord: 0xc6, Codename: "MOB_PUBLISHED", Kind: "monster"}
	own := RefObjRow{RefObjID: 9002, TidWord: 0x2c6, Codename: "STRUCTURE_OWN", Kind: "structure"}
	deps.RefObjSnapshot = func() []RefObjRow { return []RefObjRow{published, own} }
	detached := Build(deps, BootstrapRequest{CharacterName: character.Name})
	if len(detached.RefObjSnapshot) != 2 {
		t.Fatalf("detached snapshot %+v", detached.RefObjSnapshot)
	}
	refs, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{}, Monsters: []RefObjRow{published}})
	if err != nil {
		t.Fatal(err)
	}
	deps.BrowserReferences = refs
	login := Build(deps, BootstrapRequest{CharacterName: character.Name})
	if len(login.RefObjSnapshot) != 1 || login.RefObjSnapshot[0].RefObjID != own.RefObjID {
		t.Fatalf("login rows %+v, want only the unpublished structure", login.RefObjSnapshot)
	}
	if _, err := NewBrowserReferences(BrowserReferenceSources{
		Skills: oneSkillCatalogue{}, Monsters: []RefObjRow{published, published},
	}); err == nil {
		t.Fatal("a repeated monster row was published")
	}
}

/*
================
TestEveryCreatableMonsterIsPublished

On the shipped data: script spawns and GM-only rows are published with the
name fields the browser's unique announcements require, every populated
monster is among them, and every thief and hunter carries the country byte
its trade appearance (861720) picks a skin pool by.
================
*/
func TestEveryCreatableMonsterIsPublished(t *testing.T) {
	licensed.RequireGameData(t)
	template := monster.LoadTemplate(gamedatatest.TextdataDir(t))
	rows := PublicMonsterRefObjRows(simulation.NewMonsterState(template))
	byName := map[string]RefObjRow{}
	byID := map[uint32]bool{}
	for _, row := range rows {
		if row.Kind != "monster" || row.Name == "" || row.NameStrID == "" {
			t.Fatalf("published row %+v lacks the browser's monster fields", row)
		}
		byName[row.Codename] = row
		byID[row.RefObjID] = true
	}
	for _, codename := range []string{"MOB_QT_01_ONG", "MOB_QT_02_PUNISHER_CLON", "MOB_QT_01_LADON", "MOB_CH_STRONG_TIGER", "MOB_CH_TIGERWOMAN_L2"} {
		if _, ok := byName[codename]; !ok {
			t.Fatalf("%s is not published", codename)
		}
	}
	for _, ref := range template.SpawnableRefs() {
		if !ref.Structure && !byID[ref.RefObjID] {
			t.Fatalf("populated monster %s is not published", ref.Codename)
		}
	}
	bandits := 0
	for _, ref := range template.CreatableRefs() {
		row, published := byName[ref.Codename]
		if !published {
			t.Fatalf("creatable monster %s is not published", ref.Codename)
		}
		if ref.TradeAppearance() {
			bandits++
			if row.CountryByte9C == nil || *row.CountryByte9C != ref.Country {
				t.Fatalf("bandit %s row lacks its country byte: %+v", ref.Codename, row)
			}
		} else if row.CountryByte9C != nil {
			t.Fatalf("monster %s carries a country byte only bandits need", ref.Codename)
		}
	}
	if bandits == 0 {
		t.Fatal("the shipped data has no thief or hunter")
	}
}
