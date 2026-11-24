/*
===========================================================================

plusfail_test.go - the /plusfail switch: parse, standing choice, save

Port-only, not native (#3682): the line is the command only when it
leads with /plusfail; a choice is saved before it is reported, one
already in force is reported without a save, and a refused save keeps
the old choice.

===========================================================================
*/
package chat_test

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/chat"
)

/*
================
TestPlusFailCommandParsesAndSaves
================
*/
func TestPlusFailCommandParsesAndSaves(t *testing.T) {
	for _, tc := range []struct {
		name     string
		optedOut bool
		message  string
		want     string // a phrase of the answer; "" wants no answer
		handled  bool
		saves    int
	}{
		{"ordinary chat", false, "hello world", "", false, 0},
		{"another command", false, "/contributor off", "", false, 0},
		{"bare reports on", false, "/plusfail", "are on", true, 0},
		{"bare reports off", true, "  /PLUSFAIL  ", "are off", true, 0},
		{"on already on", false, "/plusfail on", "are on", true, 0},
		{"off already off", true, "/plusfail off", "are off", true, 0},
		{"turn off", false, "/plusfail off", "are off", true, 1},
		{"turn on", true, "/plusfail ON", "are on", true, 1},
		{"unknown word", false, "/plusfail maybe", "Usage:", true, 0},
		{"too many words", false, "/plusfail off please", "Usage:", true, 0},
	} {
		character := &enterworld.Character{}
		character.PlusFailOptOut = tc.optedOut
		saves := 0
		answer, handled := chat.PlusFailCommand(character, tc.message, func(c *enterworld.Character, on bool) bool {
			saves++
			c.PlusFailOptOut = !on
			return true
		})
		if handled != tc.handled {
			t.Fatalf("%s: handled = %v, want %v", tc.name, handled, tc.handled)
		}
		if saves != tc.saves {
			t.Fatalf("%s: %d saves, want %d", tc.name, saves, tc.saves)
		}
		if tc.want == "" {
			if answer != "" {
				t.Fatalf("%s: answer %q, want none", tc.name, answer)
			}
			continue
		}
		if !strings.Contains(answer, tc.want) {
			t.Fatalf("%s: answer %q, want it to say %q", tc.name, answer, tc.want)
		}
	}
}

/*
================
TestPlusFailCommandKeepsTheOldChoiceWhenTheSaveRefuses
================
*/
func TestPlusFailCommandKeepsTheOldChoiceWhenTheSaveRefuses(t *testing.T) {
	character := &enterworld.Character{}
	answer, handled := chat.PlusFailCommand(character, "/plusfail off", func(c *enterworld.Character, on bool) bool {
		return false
	})
	if !handled || !strings.Contains(answer, "could not be saved") {
		t.Fatalf("answer %q handled %v, want the refused-save line", answer, handled)
	}
	if character.PlusFailOptOut {
		t.Fatal("a refused save changed the standing choice")
	}
}
