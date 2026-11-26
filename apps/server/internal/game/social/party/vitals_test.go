/*
===========================================================================

vitals_test.go - party rows use the keeper maximum

===========================================================================
*/

package party

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

type vitalsDeps struct{}

func (vitalsDeps) CharactersForDivision(string) []*domain.Character { return nil }
func (vitalsDeps) Read(string, func())                              {}
func (vitalsDeps) CharacterModelRef(*domain.Character) uint32       { return 1 }

func TestPartyRowUsesKeeperMaximum(t *testing.T) {
	hp := int64(1100)
	character := &enterworld.Character{
		Name: "asd2", Level: testInt64(1), Strength: testInt64(20), Intellect: testInt64(20),
		CurrentHP: &hp,
	}
	rt := NewRuntime(vitalsDeps{}, nil)
	closed := rt.memberRowFor(testDivision, character)
	if closed.StatusNibbles&0x0F != 11 {
		t.Fatalf("closed form nibble %d, want full against the old maximum", closed.StatusNibbles&0x0F)
	}
	rt.UseMemberVitals(func(string, *enterworld.Character) (int64, int64, int64, int64) {
		return 1100, 1300, 200, 200
	})
	row := rt.memberRowFor(testDivision, character)
	if got := row.StatusNibbles & 0x0F; got != 9 {
		t.Fatalf("keeper nibble %d, want 9 for 1100/1300", got)
	}
}

func testInt64(v int64) *int64 { return &v }
