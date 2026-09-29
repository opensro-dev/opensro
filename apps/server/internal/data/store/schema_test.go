package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

func rewriteDatabaseMeta(t *testing.T, dir, key string, value int) {
	t.Helper()
	db, err := openDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, fmt.Sprintf("%d", value)); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseVersionMismatchRefusesInPlace(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value int
	}{
		{name: "older character schema", key: metaKeySchemaVersion, value: CurrentVersion - 1},
		{name: "newer character schema", key: metaKeySchemaVersion, value: CurrentVersion + 1},
		{name: "older table layout", key: metaKeyLayoutVersion, value: CurrentLayoutVersion - 1},
		{name: "newer table layout", key: metaKeyLayoutVersion, value: CurrentLayoutVersion + 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			clock := newTestClock()
			store := openTest(t, dir, clock)
			if err := store.CreateCharacter(testDivision, "test-account", seededCharacter()); err != nil {
				t.Fatal(err)
			}
			store.Close()
			openTest(t, dir, clock).Close() // establish a valid recovery copy

			rewriteDatabaseMeta(t, dir, test.key, test.value)
			_, err := Open(dir, Options{Now: clock.Now, DefaultSkills: testSkillSeeder})
			if err == nil {
				t.Fatal("incompatible database must refuse")
			}
			if !isVersionMismatch(err) {
				t.Fatalf("refusal must classify as a version mismatch: %v", err)
			}
			dbPath := filepath.Join(dir, DBFileName)
			if _, statErr := os.Stat(dbPath); statErr != nil {
				t.Fatalf("incompatible database must stay in place: %v", statErr)
			}
			if quarantines, _ := filepath.Glob(dbPath + corruptSuffix + "*"); len(quarantines) != 0 {
				t.Fatalf("incompatible database must not be quarantined: %v", quarantines)
			}
		})
	}
}

func TestLoadRefusesGroundGIDCounterOutsideAllocatableRange(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"-1",
		fmt.Sprintf("%d", uint64(^uint32(0)-domain.GroundItemGIDBase)+1),
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			s := openTest(t, t.TempDir(), newTestClock())
			defer s.Close()

			if _, err := s.db.Exec(
				"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
				metaKeyGidCounter,
				value,
			); err != nil {
				t.Fatal(err)
			}

			if _, err := loadDB(s.db, CurrentVersion, CurrentLayoutVersion); err == nil ||
				!strings.Contains(err.Error(), "ground gid counter") {
				t.Fatalf("loadDB ground gid counter %s error = %v", value, err)
			}
		})
	}
}

func TestLoadRefusesIdentityAliasingAndExhaustedWatermark(t *testing.T) {
	t.Parallel()

	t.Run("character id beyond player object space", func(t *testing.T) {
		s := openTest(t, t.TempDir(), newTestClock())
		defer s.Close()
		character := seededCharacter()
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
		corrupt := character.Snapshot()
		corrupt.ID = domain.MaxCharacterID + 1
		record, err := json.Marshal(corrupt)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(
			"UPDATE characters SET id = ?, record = ? WHERE division = ? AND id = ?",
			corrupt.ID,
			string(record),
			testDivision,
			character.ID,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := loadDB(s.db, CurrentVersion, CurrentLayoutVersion); err == nil ||
			!strings.Contains(err.Error(), "outside") {
			t.Fatalf("loadDB aliasing character id error = %v", err)
		}
	})

	t.Run("character watermark beyond exhaustion sentinel", func(t *testing.T) {
		s := openTest(t, t.TempDir(), newTestClock())
		defer s.Close()
		if _, err := s.db.Exec(
			"INSERT INTO next_char_id (division, next_id) VALUES (?, ?)",
			testDivision,
			domain.MaxCharacterID+2,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := loadDB(s.db, CurrentVersion, CurrentLayoutVersion); err == nil ||
			!strings.Contains(err.Error(), "next character id") {
			t.Fatalf("loadDB exhausted character watermark error = %v", err)
		}
	})
}

func TestDecodeCharacterStrictRefusesUnknownField(t *testing.T) {
	_, err := decodeCharacterStrict([]byte(`{"id":1,"accountId":"test-account","name":"asd2","mysteryKey":true}`))
	if err == nil {
		t.Fatal("unknown character field must refuse")
	}
	if !strings.Contains(err.Error(), "mysteryKey") {
		t.Fatalf("refusal must name the unknown field: %v", err)
	}
}

/*
================
TestDecodeCharacterStrictDropsEmptyRetiredFields

Records written before the item-based loadout, or by a rolled-back release,
carry empty dressSetKeys/weaponSetKeys. They still load; a populated value
is data this binary cannot represent and refuses.
================
*/
func TestDecodeCharacterStrictDropsEmptyRetiredFields(t *testing.T) {
	for _, record := range []string{
		`{"id":1,"accountId":"test-account","name":"asd2","dressSetKeys":[],"weaponSetKeys":[]}`,
		`{"id":1,"accountId":"test-account","name":"asd2","dressSetKeys":null,"weaponSetKeys":null}`,
	} {
		character, err := decodeCharacterStrict([]byte(record))
		if err != nil || character.Name != "asd2" {
			t.Fatalf("retired empty fields must load: %v (%s)", err, record)
		}
	}
	_, err := decodeCharacterStrict([]byte(`{"id":1,"accountId":"test-account","name":"asd2","weaponSetKeys":["CH_M_SPEAR_01"]}`))
	if err == nil || !strings.Contains(err.Error(), "weaponSetKeys") {
		t.Fatalf("a populated retired field must refuse by name: %v", err)
	}
	if _, err := decodeCharacterStrict([]byte(`{"id":1,"accountId":"test-account","name":"asd2","dressSetKeys":[],"mysteryKey":true}`)); err == nil {
		t.Fatal("stripping retired fields must not admit other unknown fields")
	}
}
