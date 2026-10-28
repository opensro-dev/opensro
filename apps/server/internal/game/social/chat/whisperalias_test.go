/*
===========================================================================

whisperalias_test.go - the port-only job-alias whisper rules (off = native)

Every test here runs the switch explicitly: SRO_WHISPER_JOB_ALIAS on
arms dressed-alias resolution, the own-alias self arm and the
alias-aware block match; off (or unset) keeps the native name-only
behaviour byte for byte.

===========================================================================
*/
package chat

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestWhisperJobAliasSwitchParsing

The switch ships on: unset, "on", "1" and "true" arm it; only an
explicit "off", "0" or "false" restores the native behaviour.
================
*/
func TestWhisperJobAliasSwitchParsing(t *testing.T) {
	for value, want := range map[string]bool{
		"": true, "on": true, "ON": true, "1": true, "true": true,
		"off": false, "OFF": false, "0": false, "false": false,
	} {
		t.Setenv(EnvWhisperJobAlias, value)
		if got := WhisperJobAliasEnabled(); got != want {
			t.Errorf("SRO_WHISPER_JOB_ALIAS=%q: got %v, want %v", value, got, want)
		}
	}
}

/*
================
dressedAliasCharacter

A character in job mode wearing its suit: the thief suit's TypeIDs
(3,1,7,2) packed as 0x3ac | 2<<11 in the job suit slot, alias set.
================
*/
func dressedAliasCharacter(id int64, name, alias string) *enterworld.Character {
	return &enterworld.Character{ID: id, Name: name,
		Job:              domain.CharacterJob{Type: domain.JobThief, Grade: 1, Alias: alias},
		MissionInventory: []domain.InventoryRow{{Slot: enterworld.JobSuitSlot, TypeFlags: 0x3ac | 2<<11}}}
}

/*
================
TestWhisperJobAliasResolvesDressedOwner

Switch on: a whisper typed to a dressed player's alias - any casing -
is delivered to the character behind it. The delivery keys on the real
character name (internal routing only); the line itself carries the
sender's display name, and the sender's ack is the plain success ack.
A GM sender resolves the same way (no special path, no bypass).
================
*/
func TestWhisperJobAliasResolvesDressedOwner(t *testing.T) {
	t.Setenv(EnvWhisperJobAlias, "on")
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	target := dressedAliasCharacter(8, "Berk", "Shadow")
	deps := testDeps(sender, target)
	views := Views{Presence: stubPresence{"berk": true}, Parties: stubParties{}}

	outcome := HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "sHaDoW", "yo"))
	if !bytes.Equal(outcome.Ack, []byte{0x01, 0x02, 0xFF}) {
		t.Fatalf("alias-whisper ack = % X, want success 01 02 FF", outcome.Ack)
	}
	if len(outcome.Deliveries) != 1 || outcome.Deliveries[0].TargetName != "Berk" {
		t.Fatalf("deliveries = %+v, want one to Berk (the resolved character)", outcome.Deliveries)
	}
	want := append([]byte{0x02, 0x04, 0x00, 'A', 'l', 'f', 'a'}, utf16LE("yo")...)
	if !bytes.Equal(outcome.Deliveries[0].Payload, want) {
		t.Fatalf("whisper 0x3667 = % X, want % X", outcome.Deliveries[0].Payload, want)
	}

	sender.GMPrivilege = true
	outcome = HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Shadow", "yo"))
	if len(outcome.Deliveries) != 1 || outcome.Deliveries[0].TargetName != "Berk" {
		t.Fatalf("GM alias-whisper deliveries = %+v, want one to Berk", outcome.Deliveries)
	}
}

/*
================
TestWhisperJobAliasUndressedOrOfflineFails

Switch on: an alias resolves only while its owner is dressed AND
online. Undressed, offline, and dressed-then-undressed between two
lines all answer the same error 3 as a name that never existed - the
sender learns nothing about which case it was.
================
*/
func TestWhisperJobAliasUndressedOrOfflineFails(t *testing.T) {
	t.Setenv(EnvWhisperJobAlias, "on")
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	target := dressedAliasCharacter(8, "Berk", "Shadow")
	deps := testDeps(sender, target)
	cantFind := []byte{0x02, 0x03, 0x02, 0xFF}

	// Dressed but offline.
	outcome := HandleChat(deps, Views{Presence: stubPresence{}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Shadow", "yo"))
	if !bytes.Equal(outcome.Ack, cantFind) {
		t.Fatalf("offline alias ack = % X, want error 3 % X", outcome.Ack, cantFind)
	}

	// Dressed and online: delivered. Suit off before the next line:
	// error 3, indistinguishable from the offline case above.
	views := Views{Presence: stubPresence{"berk": true}, Parties: stubParties{}}
	outcome = HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Shadow", "yo"))
	if len(outcome.Deliveries) != 1 {
		t.Fatalf("dressed alias deliveries = %+v, want one", outcome.Deliveries)
	}
	target.MissionInventory = nil
	outcome = HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Shadow", "yo"))
	if !bytes.Equal(outcome.Ack, cantFind) || len(outcome.Deliveries) != 0 {
		t.Fatalf("undressed alias ack = % X deliveries %+v, want error 3 and none", outcome.Ack, outcome.Deliveries)
	}
}

/*
================
TestWhisperJobAliasSelfArm

Switch on: whispering your own dressed alias is the self-whisper arm -
success ack, no delivery - exactly as if you had typed your character
name (which keeps working too).
================
*/
func TestWhisperJobAliasSelfArm(t *testing.T) {
	t.Setenv(EnvWhisperJobAlias, "on")
	sender := dressedAliasCharacter(7, "Alfa", "Ghost")
	deps := testDeps(sender)
	views := Views{Presence: stubPresence{"alfa": true}, Parties: stubParties{}}

	for _, typed := range []string{"ghost", "GHOST", "Alfa"} {
		outcome := HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, typed, "yo"))
		if !bytes.Equal(outcome.Ack, []byte{0x01, 0x02, 0xFF}) {
			t.Fatalf("self %q ack = % X, want success 01 02 FF", typed, outcome.Ack)
		}
		if len(outcome.Deliveries) != 0 || outcome.Broadcast != nil {
			t.Fatalf("self %q delivered: %+v", typed, outcome)
		}
	}
}

/*
================
TestWhisperJobAliasNameBeatsAlias

Switch on, with a pre-existing collision (a character actually named
Shadow, and a dressed Berk whose alias is Shadow): the typed string
resolves as the NAME. An alias never shadows a character name.
================
*/
func TestWhisperJobAliasNameBeatsAlias(t *testing.T) {
	t.Setenv(EnvWhisperJobAlias, "on")
	sender := &enterworld.Character{ID: 7, Name: "Alfa"}
	nameOwner := &enterworld.Character{ID: 9, Name: "Shadow"}
	aliasOwner := dressedAliasCharacter(8, "Berk", "Shadow")
	deps := testDeps(sender, nameOwner, aliasOwner)
	views := Views{Presence: stubPresence{"shadow": true, "berk": true}, Parties: stubParties{}}

	outcome := HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Shadow", "yo"))
	if len(outcome.Deliveries) != 1 || outcome.Deliveries[0].TargetName != "Shadow" {
		t.Fatalf("deliveries = %+v, want one to the character named Shadow", outcome.Deliveries)
	}
}

/*
================
TestWhisperJobAliasBlockListMatchesAlias

Switch on: a blocked entry matches the sender's real name OR their
current dressed alias, and the refusal stays silent (success ack, no
delivery) - blocking an alias works, and blocking the real name still
catches the suited player. An unrelated entry changes nothing.
================
*/
func TestWhisperJobAliasBlockListMatchesAlias(t *testing.T) {
	t.Setenv(EnvWhisperJobAlias, "on")
	sender := dressedAliasCharacter(7, "Alfa", "Ghost")
	target := &enterworld.Character{ID: 8, Name: "Cale"}
	deps := testDeps(sender, target)
	views := Views{Presence: stubPresence{"cale": true}, Parties: stubParties{}}

	for _, blocked := range [][]string{{"ghost"}, {"ALFA"}} {
		target.BlockedWhisperers = blocked
		outcome := HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Cale", "yo"))
		if !bytes.Equal(outcome.Ack, []byte{0x01, 0x02, 0xFF}) {
			t.Fatalf("blocked %v ack = % X, want the silent success 01 02 FF", blocked, outcome.Ack)
		}
		if len(outcome.Deliveries) != 0 {
			t.Fatalf("blocked %v delivered: %+v", blocked, outcome.Deliveries)
		}
	}

	target.BlockedWhisperers = []string{"SomeoneElse"}
	outcome := HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Cale", "yo"))
	if len(outcome.Deliveries) != 1 || outcome.Deliveries[0].TargetName != "Cale" {
		t.Fatalf("unrelated block deliveries = %+v, want one to Cale", outcome.Deliveries)
	}
}

/*
================
TestWhisperJobAliasOffStaysNative

Switch off (the default): every alias behaviour is gone. A dressed
alias does not resolve, the own-alias self arm does not exist, and a
block entry naming the sender's alias matches nothing - the native
name-only answers, byte for byte.
================
*/
func TestWhisperJobAliasOffStaysNative(t *testing.T) {
	t.Setenv(EnvWhisperJobAlias, "off")
	sender := dressedAliasCharacter(7, "Alfa", "Ghost")
	target := dressedAliasCharacter(8, "Berk", "Shadow")
	deps := testDeps(sender, target)
	views := Views{Presence: stubPresence{"alfa": true, "berk": true}, Parties: stubParties{}}
	cantFind := []byte{0x02, 0x03, 0x02, 0xFF}

	outcome := HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Shadow", "yo"))
	if !bytes.Equal(outcome.Ack, cantFind) || len(outcome.Deliveries) != 0 {
		t.Fatalf("off: alias ack = % X deliveries %+v, want error 3 and none", outcome.Ack, outcome.Deliveries)
	}
	outcome = HandleChat(deps, views, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Ghost", "yo"))
	if !bytes.Equal(outcome.Ack, cantFind) || len(outcome.Deliveries) != 0 {
		t.Fatalf("off: own-alias ack = % X deliveries %+v, want error 3 and none", outcome.Ack, outcome.Deliveries)
	}

	plainTarget := &enterworld.Character{ID: 9, Name: "Cale", BlockedWhisperers: []string{"Ghost"}}
	deps = testDeps(sender, plainTarget)
	outcome = HandleChat(deps, Views{Presence: stubPresence{"cale": true}, Parties: stubParties{}}, testDivision, sender, chatFrame(ChatTypeWhisper, 0xFF, "Cale", "yo"))
	if len(outcome.Deliveries) != 1 || outcome.Deliveries[0].TargetName != "Cale" {
		t.Fatalf("off: alias block entry deliveries = %+v, want one to Cale (entry inert)", outcome.Deliveries)
	}
}
