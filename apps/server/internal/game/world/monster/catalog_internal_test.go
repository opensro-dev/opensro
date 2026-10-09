package monster

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPopulationEvidenceSemantics(t *testing.T) {
	if got := len(v1188PopulationEvidence); got != 4449 {
		t.Fatalf("population evidence rows = %d, want 4449", got)
	}

	mangnyang, ok := v1188PopulationEvidence[evidenceKey(
		"MOB_CH_MANGNYANG", 25258, 812.679993, 75.080002, 392.899994,
	)]
	if !ok {
		t.Fatal("Mangnyang natural-key evidence is missing")
	}
	if mangnyang.Aggressive {
		t.Fatal("Mangnyang evidence is aggressive; btAggressType=1 must be passive")
	}
	if mangnyang.SightRange != 115 || mangnyang.MaxCount != 17 ||
		mangnyang.Radius != 500 || mangnyang.GenerateRadius != 400 ||
		mangnyang.ChampionGenPercentage != 10 ||
		mangnyang.RespawnDelayMinSec != 1 ||
		mangnyang.RespawnDelayMaxSec != 3 || !mangnyang.Respawn {
		t.Fatalf("Mangnyang evidence = %+v", mangnyang)
	}

	stoneGhost, ok := v1188PopulationEvidence[evidenceKey(
		"MOB_CH_STONEGHOST", 24236, 703.539978, 194.589996, 437.940002,
	)]
	if !ok || !stoneGhost.Aggressive {
		t.Fatalf("Stone Ghost evidence = %+v, found=%v; btAggressType=0 must be aggressive", stoneGhost, ok)
	}
}

func TestLoadMonsterRideMetadataModes(t *testing.T) {
	dir := t.TempDir()
	const skillEffect = "#section\tcharacterInfo\n" +
		"MOB_CH_TIGERWOMAN\tMOB_TIGERWOMAN\t2.8\tnone\tres\\mob\\china\\bluetiger.bsr\n" +
		"MOB_FIXED\tMOB_FIXED\t1\tRT_FIXED\tres\\mob\\fixed.bsr\n" +
		"MOB_DUMMY\tMOB_DUMMY\t1\tRT_DUMMY\tres\\mob\\dummy.bsr\n" +
		"MOB_UNKNOWN\tMOB_UNKNOWN\t1\tRT_GUESSED\tres\\mob\\guessed.bsr\n" +
		"#section\tskillInfo\n"
	if err := os.WriteFile(filepath.Join(dir, "skilleffect.txt"), []byte(skillEffect), 0o644); err != nil {
		t.Fatal(err)
	}

	metadata := loadMonsterRideMetadata(dir)
	if got := metadata.byCodename["MOB_CH_TIGERWOMAN"]; got.modelPath != `res\mob\china\bluetiger.bsr` || got.transformMode != 0 {
		t.Fatalf("Tiger Girl ride metadata = %+v", got)
	}
	if got := metadata.byCodename["MOB_FIXED"]; got.transformMode != 1 {
		t.Fatalf("RT_FIXED mode = %+v, want 1", got)
	}
	if got := metadata.byCodename["MOB_DUMMY"]; got.transformMode != 2 {
		t.Fatalf("RT_DUMMY mode = %+v, want 2", got)
	}
	if _, ok := metadata.byCodename["MOB_UNKNOWN"]; ok {
		t.Fatal("unknown Ride Type was silently accepted")
	}
}

/*
================
TestCharacterInfoRideLookupFollows9171B0

Own record, then the original reference's, then the first record naming a
known reference. A record without a ride still ends the lookup, and a
numbered range registers every name in it.
================
*/
func TestCharacterInfoRideLookupFollows9171B0(t *testing.T) {
	dir := t.TempDir()
	const skillEffect = "#section\tcharacterInfo\n" +
		"MOB_UNKNOWN_FIRST\tX\t1\tRT_FIXED\tres\\mob\\never.bsr\n" +
		"CHAR_DEFAULT\tX\t1\tnone\tnone\n" +
		"MOB_CH_TIGERWOMAN\tX\t2.8\tnone\tres\\mob\\china\\bluetiger.bsr\n" +
		"MOB_WALKER\tX\t1\tnone\tnone\n" +
		"MOB_RIDER001~003\tX\t1\tRT_DUMMY\tres\\mob\\horse.bsr\n" +
		"MOB_TWICE\tX\t1\tnone\tnone\n" +
		"MOB_TWICE\tX\t1\tRT_FIXED\tres\\mob\\later.bsr\n"
	if err := os.WriteFile(filepath.Join(dir, "skilleffect.txt"), []byte(skillEffect), 0o644); err != nil {
		t.Fatal(err)
	}
	rides := loadMonsterRideMetadata(dir)
	known := map[string]bool{"CHAR_DEFAULT": true, "MOB_CH_TIGERWOMAN": true, "MOB_WALKER": true}
	if got := rides.resolve("MOB_CH_TIGERWOMAN_L2", "MOB_CH_TIGERWOMAN", known); got.modelPath != `res\mob\china\bluetiger.bsr` {
		t.Fatalf("a variant without a record rides %+v, want its original's mount", got)
	}
	if got := rides.resolve("MOB_WALKER", "MOB_CH_TIGERWOMAN", known); got.modelPath != "" {
		t.Fatalf("an own record without a ride fell through to %+v", got)
	}
	if got := rides.resolve("MOB_RIDER002", "", known); got.modelPath != `res\mob\horse.bsr` || got.transformMode != 2 {
		t.Fatalf("a ranged record resolved %+v", got)
	}
	// 91B7E0 keeps the last of two records for one name.
	if got := rides.resolve("MOB_TWICE", "", known); got.modelPath != `res\mob\later.bsr` || got.transformMode != 1 {
		t.Fatalf("a repeated record resolved %+v, want the later one", got)
	}
	// The default skips MOB_UNKNOWN_FIRST: it names no known reference.
	if got := rides.resolve("MOB_ORPHAN", "MOB_NOBODY", known); got.modelPath != "" {
		t.Fatalf("the default resolved %+v, want CHAR_DEFAULT's none", got)
	}
}

func TestPopulationAnchorUsesNativeFloatBeforeQuantization(t *testing.T) {
	for _, pair := range [][2]float64{{1476.85, 1476.849976}, {101.45, 101.449997}, {1258.45, 1258.449951}, {-1476.85, -1476.849976}} {
		a, b := evidenceKey("MOB", 1, pair[0], pair[0], pair[0]), evidenceKey("MOB", 1, pair[1], pair[1], pair[1])
		if a != b {
			t.Fatalf("same native anchor split: %v versus %v", a, b)
		}
	}
	if evidenceKey("MOB", 1, 1, 2, 3) == evidenceKey("MOB", 2, 1, 2, 3) || evidenceKey("MOB", 1, 1, 2, 3) == evidenceKey("OTHER", 1, 1, 2, 3) || evidenceKey("MOB", 1, 1, 2, 3) == evidenceKey("MOB", 1, 1.2, 2, 3) {
		t.Fatal("distinct anchor identity merged")
	}
	for _, code := range []string{"MOB_EU_KERBEROS", "MOB_OA_URUCHI"} {
		for key := range v1188PopulationEvidence {
			if key.Codename == code {
				if _, ok := hiveCaps[key]; !ok {
					t.Fatalf("population/hive normalization diverged: %+v", key)
				}
			}
		}
	}
}
