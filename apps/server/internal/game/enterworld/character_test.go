package enterworld

import (
	"math"
	"testing"
)

func TestResolveCharacterRaceKey(t *testing.T) {
	cases := []struct {
		name      string
		character *Character
		want      string
	}{
		{"nil", nil, RaceKeyEurope},
		{"empty", &Character{}, RaceKeyEurope},
		{"raceIndexChina", &Character{RaceIndex: i64(RaceChina)}, RaceKeyChina},
		{"raceIndexClampedHigh", &Character{RaceIndex: i64(7)}, RaceKeyChina},
		{"raceIndexClampedLow", &Character{RaceIndex: i64(-2)}, RaceKeyEurope},
		// The model codename prefix wins over raceIndex.
		{"codenameBeatsIndex", &Character{ModelCodename: "CHAR_CH_MAN_ADVENTURER", RaceIndex: i64(RaceEurope)}, RaceKeyChina},
		{"codenameEurope", &Character{ModelCodename: "CHAR_EU_WOMAN_ADVENTURER", RaceIndex: i64(RaceChina)}, RaceKeyEurope},
	}
	for _, tc := range cases {
		if got := ResolveCharacterRaceKey(tc.character); got != tc.want {
			t.Errorf("%s: raceKey = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRaceGenderKey(t *testing.T) {
	cases := []struct {
		name          string
		character     *Character
		modelCodename string
		want          string
	}{
		{"chinaMan", &Character{}, "CHAR_CH_MAN_ADVENTURER", "CH_M"},
		{"chinaWoman", &Character{}, "CHAR_CH_WOMAN_ADVENTURER", "CH_W"},
		{"europeWoman", &Character{}, "CHAR_EU_WOMAN_ADVENTURER", "EU_W"},
		// Codename race beats the character's raceIndex.
		{"codenameBeatsRace", &Character{RaceIndex: i64(RaceEurope)}, "CHAR_CH_MAN_ADVENTURER", "CH_M"},
		// No codename: character fields decide.
		{"fieldsFemaleChina", &Character{RaceIndex: i64(RaceChina), Gender: i64(GenderFemale)}, "", "CH_W"},
		{"fieldsDefaultMaleEurope", &Character{}, "", "EU_M"},
		// Gender token in the codename beats the gender field.
		{"codenameGenderBeatsField", &Character{Gender: i64(GenderFemale)}, "CHAR_CH_MAN_ADVENTURER", "CH_M"},
	}
	for _, tc := range cases {
		if got := RaceGenderKey(tc.character, tc.modelCodename); got != tc.want {
			t.Errorf("%s: key = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func scaleApproximately(got, want float64) bool {
	return math.Abs(got-want) < 1e-9
}

func TestResolveCharacterHeightScale(t *testing.T) {
	cases := []struct {
		name      string
		character *Character
		want      float64
	}{
		{"nilDefaults", nil, 1.0}, // index default 2 -> 0.94 + 2*0.03
		{"heightIndex0", &Character{HeightIndex: i64(0)}, 0.94},
		{"heightIndex4", &Character{HeightIndex: i64(4)}, 1.06},
		{"heightIndexClamped", &Character{HeightIndex: i64(9)}, 1.06},
		// bodyShapeByte low nibble wins over heightIndex.
		{"bodyShapeLowNibble", &Character{BodyShapeByte: i64(0x42), HeightIndex: i64(0)}, 1.0},
		// A nibble above 4 clamps to 4.
		{"bodyShapeNibbleClamped", &Character{BodyShapeByte: i64(0x0f)}, 1.06},
		// Explicit positive scale wins, clamped into 0.5..2.
		{"explicit", &Character{HeightScale: f64(1.5), BodyShapeByte: i64(0)}, 1.5},
		{"explicitClampedHigh", &Character{HeightScale: f64(9)}, 2},
		{"explicitClampedLow", &Character{HeightScale: f64(0.1)}, 0.5},
		// Non-positive explicit scale falls through to the indexes.
		{"explicitZeroIgnored", &Character{HeightScale: f64(0), HeightIndex: i64(0)}, 0.94},
	}
	for _, tc := range cases {
		if got := ResolveCharacterHeightScale(tc.character); !scaleApproximately(got, tc.want) {
			t.Errorf("%s: heightScale = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolveCharacterVolumeScale(t *testing.T) {
	cases := []struct {
		name      string
		character *Character
		want      float64
	}{
		{"nilDefaults", nil, 1.0},
		// bodyShapeByte HIGH nibble.
		{"bodyShapeHighNibble", &Character{BodyShapeByte: i64(0x42)}, 1.06},
		{"volumeIndex0", &Character{VolumeIndex: i64(0)}, 0.94},
		{"explicit", &Character{VolumeScale: f64(0.8)}, 0.8},
	}
	for _, tc := range cases {
		if got := ResolveCharacterVolumeScale(tc.character); !scaleApproximately(got, tc.want) {
			t.Errorf("%s: volumeScale = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolveCharacterCameraHeight(t *testing.T) {
	character := &Character{HeightIndex: i64(4)}
	if got := ResolveCharacterCameraHeight(character); !scaleApproximately(got, 1.06*20) {
		t.Fatalf("cameraHeight = %v, want %v", got, 1.06*20)
	}
}

func TestStartProfiles(t *testing.T) {
	china := StartProfileForRace(RaceKeyChina)
	if china.RegionID != 0x62a8 {
		t.Errorf("china region = %#x, want 0x62a8", china.RegionID)
	}
	europe := StartProfileForRace(RaceKeyEurope)
	if europe.RegionID != 0x6b4f {
		t.Errorf("europe region = %#x, want 0x6b4f", europe.RegionID)
	}
	if got := StartProfileForRace("nosuch").RegionID; got != 0x6b4f {
		t.Errorf("unknown race degrades to europe, got region %#x", got)
	}
	cases := []struct {
		race   string
		gender int64
		want   uint32
	}{
		{RaceKeyEurope, GenderMale, 14726},
		{RaceKeyEurope, GenderFemale, 14738},
		{RaceKeyChina, GenderMale, 1907},
		{RaceKeyChina, GenderFemale, 1920},
		{RaceKeyChina, 42, 1907}, // unknown gender uses the native male default
	}
	for _, tc := range cases {
		if got := DefaultModelRefForRaceGender(tc.race, tc.gender); got != tc.want {
			t.Errorf("default model(%s, %d) = %d, want %d", tc.race, tc.gender, got, tc.want)
		}
	}
}

func TestCoerceHelpers(t *testing.T) {
	if got := coerceInt(nil, 0, 10, 7); got != 7 {
		t.Errorf("coerceInt(nil) = %d, want fallback 7", got)
	}
	if got := coerceInt(i64(-5), 0, 10, 7); got != 0 {
		t.Errorf("coerceInt(-5) = %d, want clamp 0", got)
	}
	if got := coerceInt(i64(50), 0, 10, 7); got != 10 {
		t.Errorf("coerceInt(50) = %d, want clamp 10", got)
	}
	if _, ok := coerceOptionalInt(nil, 0, 10); ok {
		t.Error("coerceOptionalInt(nil) must stay absent")
	}
	if got, ok := coerceOptionalInt(i64(300), 0, 0xff); !ok || got != 0xff {
		t.Errorf("coerceOptionalInt(300) = %d,%v, want 255,true", got, ok)
	}
}
