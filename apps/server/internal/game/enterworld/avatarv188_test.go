/*
===========================================================================

avatarv188_test.go - tests for the always-loaded v1.188 avatar rows

Covers NewTextdataMagicOptions and LoadTextdataCatalogs loading the v1.188
avatar Lucky and 4x stacking rows (ids 370..376) absent from v1.150 tables,
and refusing base-table id collisions.

===========================================================================
*/

package enterworld

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestTextdataMagicOptionsLoadsV188AvatarRows
================
*/
func TestTextdataMagicOptionsLoadsV188AvatarRows(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)

	loader := NewTextdataMagicOptions(dir)
	if err := loader.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := map[uint32]struct {
		name string
		tag  uint32
	}{
		370: {"MATTR_AVATAR_MDIA_2", avatarV188TagMDIA},
		371: {"MATTR_AVATAR_MDIA_3", avatarV188TagMDIA},
		372: {"MATTR_AVATAR_MDIA_4", avatarV188TagMDIA},
		373: {"MATTR_AVATAR_LUCK", avatarV188TagLUCK},
		374: {"MATTR_AVATAR_LUCK_2", avatarV188TagLUCK},
		375: {"MATTR_AVATAR_LUCK_3", avatarV188TagLUCK},
		376: {"MATTR_AVATAR_LUCK_4", avatarV188TagLUCK},
	}
	for id, exp := range want {
		row, ok := loader.MagicOptionByParamID(id)
		if !ok || row == nil {
			t.Fatalf("missing id %d (%s)", id, exp.name)
		}
		if row.OptionName != exp.name || row.Tag != exp.tag || row.ParamName != "+" || row.Degree != 1 {
			t.Fatalf("id %d = %+v, want %+v", id, row, exp)
		}
		if row.RangeWords == nil || *row.RangeWords != [3]uint32{1, 1, 1} {
			t.Fatalf("id %d RangeWords = %v, want [1 1 1]", id, row.RangeWords)
		}
		if len(row.Descriptors) != 1 || row.Descriptors[0] != "avatar_mail" {
			t.Fatalf("id %d Descriptors = %v, want [avatar_mail]", id, row.Descriptors)
		}
	}

	catalogs, err := LoadTextdataCatalogs(dir)
	if err != nil {
		t.Fatalf("LoadTextdataCatalogs: %v", err)
	}
	for id := AvatarV188OptionIDFirst; id <= AvatarV188OptionIDLast; id++ {
		if _, ok := catalogs.MagicOptions.MagicOptionByParamID(id); !ok {
			t.Fatalf("catalogs missing v1.188 avatar row %d", id)
		}
	}
}

/*
================
TestTextdataMagicOptionsRefusesAvatarV188IDCollision
================
*/
func TestTextdataMagicOptionsRefusesAvatarV188IDCollision(t *testing.T) {
	dir := t.TempDir()
	line := strings.Join([]string{
		"1", "373", "MATTR_AVATAR_LUCK", "+", "1", "1.0", "0", "1819632481",
		"1", "1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1819632481",
		"0", "0", "0", "0", "0", "avatar_mail", "1", "xxx", "0", "xxx", "0", "xxx", "0", "xxx", "0",
		"xxx", "0", "xxx", "0", "xxx", "0", "xxx", "0", "xxx", "0",
	}, "\t") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "magicoption.txt"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := NewTextdataMagicOptions(dir)
	if err := loader.Load(); err == nil {
		t.Fatal("expected id 373 collision error, got nil")
	}
	// A caller that skipped Load() must not see a half-applied table.
	if n := loader.Len(); n != 0 {
		t.Fatalf("table after collision has %d row(s), want 0", n)
	}
	if _, ok := loader.MagicOptionByParamID(370); ok {
		t.Fatal("overlay row 370 served after a collision")
	}
}
