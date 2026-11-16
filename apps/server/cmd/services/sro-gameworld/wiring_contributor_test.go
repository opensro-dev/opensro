/*
===========================================================================

wiring_contributor_test.go - the Contributor roster hook on the ticker

Off installs no hook (native); on, the hook sends the division the
roster of its online contributors and then only new arrivals.

===========================================================================
*/
package main

import (
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/social/contributor"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
contributorHook

The installed contributor hook, or nil.
================
*/
func contributorHook(ticker *simulation.Ticker) *simulation.NamedTickHook {
	for i := range ticker.Hooks {
		if ticker.Hooks[i].Name == "contributor-titles" {
			return &ticker.Hooks[i]
		}
	}
	return nil
}

/*
================
TestContributorHookInstallsOnlyWhenEnabled
================
*/
func TestContributorHookInstallsOnlyWhenEnabled(t *testing.T) {
	game, _, character := betaWiringFixture()
	game.betaSilk = nil
	if contributorHook(game.newMissionTicker(&action.PeerReferenceCatalog{})) != nil {
		t.Fatal("contributor hook installed with the title off")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, contributor.MembersFileName), []byte(`{"accounts":["`+character.AccountID+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := contributor.Open(dir, filepath.Join(dir, contributor.MembersFileName), func(path string, body []byte) error { return os.WriteFile(path, body, 0o600) })
	if err != nil {
		t.Fatal(err)
	}
	game.contributors = registry
	ticker := game.newMissionTicker(&action.PeerReferenceCatalog{})
	hook := contributorHook(ticker)
	if hook == nil {
		t.Fatal("contributor hook missing with the title on")
	}
	source := &betaWiringSource{}
	source.set(
		simulation.SessionSnapshot{SessionID: "1:1", DivisionID: betaWiringDivision, CharacterID: character.ID},
		simulation.SessionSnapshot{SessionID: "1:2", DivisionID: "foreign", CharacterID: 2},
	)
	push := &betaWiringPusher{deliveries: make(chan betaWiringDelivery, 4)}
	ticker.Source, ticker.Push = source, push
	if out := hook.Hook(1); out != nil {
		t.Fatal("contributor hook leaked generic routing")
	}
	// The login prompt reaches the contributor's own session first: its
	// one account carries the title, so it is the holder's OK.
	prompt := <-push.deliveries
	if prompt.session != "1:1" || len(prompt.frames) != 1 || prompt.frames[0].Opcode != contributor.OpPrompt ||
		string(prompt.frames[0].Payload) != string([]byte{contributor.PromptHolder}) {
		t.Fatalf("first pass prompted %+v", prompt)
	}
	got := <-push.deliveries
	want := contributor.EncodeRoster([]string{character.Name})
	if got.session != "unexpected division:"+betaWiringDivision || len(got.frames) != 1 ||
		got.frames[0].Opcode != contributor.OpRoster || string(got.frames[0].Payload) != string(want) {
		t.Fatalf("first pass sent %+v", got)
	}
	// Nothing changed and nobody arrived: the next pass is silent.
	hook.Hook(1 + 5_000)
	select {
	case extra := <-push.deliveries:
		t.Fatalf("unchanged roster sent again: %+v", extra)
	default:
	}
}
