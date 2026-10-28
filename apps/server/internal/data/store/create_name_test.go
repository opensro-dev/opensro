package store

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// Character creation accepts exactly the native [A-Za-z0-9_]+ shape.
// The underscore acceptance
// is the load-bearing half - the native abusefilter.txt allows 0x5F, and a
// stricter alphanumeric-only shape would refuse retail-legal names.
func TestCreateCharacterEnforcesNativeNameShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: "hey_Hey"}); err != nil {
		t.Fatalf("underscore names are native-legal and must create: %v", err)
	}

	for _, name := range []string{"hey-Hey", "hey Hey", "héllo", "asd!", "名前", "a.b"} {
		if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: name}); err == nil {
			t.Errorf("name %q violates the native shape and must refuse", name)
		}
	}
}

// The native name-length bound is 2..12 inclusive (three v1.150 client
// sites gate len-2 > 0xa, and the retail server has the same bound).
// The boundary is exact: 2 and 12 are both legal, 1 and 13 both refuse.
func TestCreateCharacterEnforcesNativeNameLength(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	// Inputs chosen so length cannot coincide with anything else: the
	// two-char and twelve-char names are distinct legal lengths, and the
	// one/thirteen-char names sit one step outside each boundary.
	legal := []string{"Ab", "abcdefghijkl", "tester123"} // boundaries + reported regression
	for _, name := range legal {
		if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: name}); err != nil {
			t.Fatalf("name %q (len %d) is inside the native 2..12 range and must create: %v", name, len(name), err)
		}
	}

	illegal := []string{"a", "abcdefghijklm"} // len 1 and len 13
	for _, name := range illegal {
		if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: name}); err == nil {
			t.Errorf("name %q (len %d) is outside the native 2..12 range and must refuse", name, len(name))
		}
	}
}

func TestCreateCharacterReturnsTypedNameErrors(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: "tester123"}); err != nil {
		t.Fatalf("tester123 is a legal native name: %v", err)
	}
	if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: "tester123"}); !errors.Is(err, ErrCharacterNameConflict) {
		t.Fatalf("duplicate error = %v, want ErrCharacterNameConflict", err)
	}
	if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: "tester-123"}); !errors.Is(err, ErrCharacterNameInvalid) {
		t.Fatalf("invalid-shape error = %v, want ErrCharacterNameInvalid", err)
	}
}

// A new character name may not equal an existing character's job
// alias: the alias is what its owner speaks and appears under while
// suited, so a registered twin would impersonate them (and make typed
// whisper targets ambiguous). The reference server never refuses the
// clash - a native bug we do not copy (owner, 2026-10-28) - so the
// refusal is unconditional and reuses the duplicate-name error class.
func TestCreateCharacterRefusesExistingJobAlias(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	holder := &enterworld.Character{Name: "Holder", Job: domain.CharacterJob{Type: domain.JobThief, Alias: "Shadow"}}
	if err := s.CreateCharacter(testDivision, "test-account", holder); err != nil {
		t.Fatalf("holder create: %v", err)
	}
	if err := s.CreateCharacter(testDivision, "other-account", &enterworld.Character{Name: "shadow"}); !errors.Is(err, ErrCharacterNameConflict) {
		t.Fatalf("name equal to an alias error = %v, want ErrCharacterNameConflict", err)
	}
	if err := s.CreateCharacter(testDivision, "other-account", &enterworld.Character{Name: "Shadowfax"}); err != nil {
		t.Fatalf("an unrelated name must still create: %v", err)
	}
}

// TestNamePrecheckAgreesWithCreateOnLength probes the pre-check/create
// disagreement: CharacterNameShapeValid used to validate CHARSET ONLY, so a
// 1-char or 13-char name passed the name-overlap pre-check ("name is OK")
// and then refused at CreateCharacter on the native 2..12 length rule. The
// exported helper must give the same verdict as the create path for every
// shape and length case.
func TestNamePrecheckAgreesWithCreateOnLength(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)

	cases := []string{
		"a",             // 1 char: under the native 2..12 range
		"Ab",            // 2 chars: boundary legal
		"abcdefghijkl",  // 12 chars: boundary legal
		"abcdefghijklm", // 13 chars: over the range
		"Berk_01",       // mid-length legal (underscore included)
		"asd!",          // illegal charset
	}
	for _, name := range cases {
		precheck := CharacterNameShapeValid(name)
		err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: name})
		created := err == nil
		if precheck != created {
			t.Errorf("name %q: pre-check valid=%v but create accepted=%v (err %v) - the availability check and the create disagree", name, precheck, created, err)
		}
	}
}
