/*
===========================================================================

announce_kill_test.go - a seal drop is announced whichever way the kill lands

The seal-drop notice (announce.go) is posted from each kill path that
commits drops. The area, periodic and credited-hit kills always posted
it; the single-target skill kill (which a basic attack also takes) did
not, so most seal drops went unannounced.

===========================================================================
*/
package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestSingleTargetKillAnnouncesASealDrop

A basic attack's fatal hit that drops a sealed item posts one notice
naming the looter, the item and the monster; with the notice off it posts
none.
================
*/
func TestSingleTargetKillAnnouncesASealDrop(t *testing.T) {
	for _, on := range []bool{true, false} {
		rt, _, character, target := newCombatTestRuntime(t, 1)
		installSmallGoldRef(rt)
		// Every equipment drop rolls rare; the sword's sealed row exists.
		rt.RareRate = rareEquipmentDomain
		items := rt.deps.ItemReferences().(staticItemSource)
		if plain, ok := items["ITEM_CH_SWORD_01_A"]; ok {
			sealed := *plain
			sealed.Codename = "ITEM_CH_SWORD_01_A" + sealCodenameSuffix
			sealed.RefObjID += 50000
			items[sealed.Codename] = &sealed
		}
		rt.DropRoll = constantDropRoll(0)
		rt.Announce.SoxDrop = on

		rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
			ActionId: 2, HasTarget: true, TargetGid: target.Gid,
		}.Encode())
		sealed := false
		for _, drop := range rt.Ground.All(testDivision) {
			sealed = sealed || strings.HasSuffix(drop.Codename, sealCodenameSuffix)
		}
		if !sealed {
			t.Fatalf("fixture: the kill dropped no sealed item: %+v", rt.Ground.All(testDivision))
		}
		got := drainedNotices(t, rt)
		if !on {
			if len(got) != 0 {
				t.Fatalf("notice off, posted %q", got)
			}
			continue
		}
		if len(got) != 1 || !strings.HasPrefix(got[0], character.Name+" has dropped ") ||
			!strings.HasSuffix(got[0], " from "+target.Ref.Name) {
			t.Fatalf("single-target kill posted %q, want one \"%s has dropped ... from %s\"", got, character.Name, target.Ref.Name)
		}
	}
}
