/*
===========================================================================

spminted_test.go - the SP-mint observation the guild GP accrual counts

===========================================================================
*/

package progression

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestSPMintedHookCountsWholeSP

The hook sees the whole SP each skill-EXP conversion mints (the guild GP
accrual's counting input, issue #4051): 950 skill EXP mints 2 SP; a grant
that mints nothing stays silent; and EXP-only grants never report.
================
*/
func TestSPMintedHookCountsWholeSP(t *testing.T) {
	character := levelupTestCharacter()
	runtime := newTestRuntime(character)
	var minted []int64
	runtime.SPMintedHook = func(c *enterworld.Character, spMinted int64) {
		if c != character {
			t.Errorf("hook character = %p, want the granted character", c)
		}
		minted = append(minted, spMinted)
	}

	runtime.GrantExperience(character, 0, 399, 0)
	if len(minted) != 0 {
		t.Fatalf("399 skill EXP minted %v, want no SP yet", minted)
	}
	// The pool carried 399; 551 more crosses two whole SP (950 total).
	runtime.GrantExperience(character, 0, 551, 0)
	if len(minted) != 1 || minted[0] != 2 {
		t.Fatalf("minted = %v, want [2]", minted)
	}
	runtime.GrantExperience(character, 600, 0, 0)
	if len(minted) != 1 {
		t.Fatalf("EXP-only grant reported %v, want still [2]", minted)
	}
	if character.SkillPoints == nil || *character.SkillPoints != 2 {
		t.Fatalf("SP balance = %v, want 2 (the hook counts what landed)", character.SkillPoints)
	}
}

/*
================
TestSPMintedHookNilIsSilent

No hook wired (tests, tools) changes nothing about the mint itself.
================
*/
func TestSPMintedHookNilIsSilent(t *testing.T) {
	character := levelupTestCharacter()
	runtime := newTestRuntime(character)
	runtime.GrantExperience(character, 0, 800, 0)
	if character.SkillPoints == nil || *character.SkillPoints != 2 {
		t.Fatalf("SP balance = %v, want 2", character.SkillPoints)
	}
}
