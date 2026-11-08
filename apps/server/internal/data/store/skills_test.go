package store

import (
	"fmt"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

func TestCreateCharacterSeedsRacialBaseSkills(t *testing.T) {
	t.Parallel()

	t.Run("racial seeds are distinct", func(t *testing.T) {
		store := openTest(t, t.TempDir(), newTestClock())
		chinese := &enterworld.Character{Name: "chfresh", ModelCodename: "CHAR_CH_MAN_01"}
		european := &enterworld.Character{Name: "eufresh", ModelCodename: "CHAR_EU_WOMAN_01"}
		if err := store.CreateCharacter(testDivision, "test-account", chinese); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateCharacter(testDivision, "test-account", european); err != nil {
			t.Fatal(err)
		}
		if want := []uint32{1, 2, 40, 70}; !reflect.DeepEqual(chinese.Skills, want) {
			t.Fatalf("Chinese creation skills = %v, want %v", chinese.Skills, want)
		}
		if want := []uint32{1, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970}; !reflect.DeepEqual(european.Skills, want) {
			t.Fatalf("European creation skills = %v, want %v", european.Skills, want)
		}
	})

	t.Run("derived vitals stay unpersisted", func(t *testing.T) {
		store := openTest(t, t.TempDir(), newTestClock())
		character := &enterworld.Character{Name: "vitals", ModelCodename: "CHAR_CH_MAN_01"}
		if err := store.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
		if character.Strength == nil || *character.Strength != enterworld.BaseStat ||
			character.Intellect == nil || *character.Intellect != enterworld.BaseStat {
			t.Fatalf("creation stats = %v/%v, want %d/%d", character.Strength, character.Intellect, enterworld.BaseStat, enterworld.BaseStat)
		}
		if character.CurrentHP != nil || character.CurrentMP != nil {
			t.Fatal("creation persisted derived vitality fields")
		}
		if got := enterworld.DerivedMaxHP(character); got != 200 {
			t.Fatalf("derived max HP = %d, want 200", got)
		}
		if got := enterworld.DerivedMaxMP(character); got != 200 {
			t.Fatalf("derived max MP = %d, want 200", got)
		}
	})

	t.Run("seed failure leaves no record", func(t *testing.T) {
		broken := func(string, []uint32) ([]uint32, error) {
			return nil, fmt.Errorf("SKILL_PUNCH_01 does not resolve")
		}
		store, err := Open(t.TempDir(), Options{Now: newTestClock().Now, DefaultSkills: broken})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(store.Close)
		character := &enterworld.Character{Name: "refused", ModelCodename: "CHAR_CH_MAN_01"}
		if err := store.CreateCharacter(testDivision, "test-account", character); err == nil {
			t.Fatal("creation with an unresolved seed must refuse")
		}
		if got := len(store.Characters().CharactersForDivision(testDivision)); got != 0 {
			t.Fatalf("refused creation left %d record(s)", got)
		}
	})

	t.Run("pre-populated skills remain caller-owned", func(t *testing.T) {
		store := openTest(t, t.TempDir(), newTestClock())
		character := &enterworld.Character{Name: "preset", ModelCodename: "CHAR_CH_MAN_01", Skills: []uint32{6}}
		if err := store.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
		if want := []uint32{6}; !reflect.DeepEqual(character.Skills, want) {
			t.Fatalf("creation overwrote skills: %v", character.Skills)
		}
	})
}

func TestSkillsRoundTripThroughStore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first, err := Open(dir, Options{DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	character := &enterworld.Character{Name: "learner", ModelCodename: "CHAR_CH_MAN_01"}
	if err := first.CreateCharacter(testDivision, "test-account", character); err != nil {
		t.Fatal(err)
	}
	first.MutateCharacter(character, "skill-learn", func() {
		character.Skills = []uint32{2, 6, 0x01020304}
	})
	first.Close()

	second, err := Open(dir, Options{DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got, want := second.Characters().CharactersForDivision(testDivision)[0].Skills, []uint32{2, 6, 0x01020304}; !reflect.DeepEqual(got, want) {
		t.Fatalf("skills after restart = %v, want %v", got, want)
	}
}
