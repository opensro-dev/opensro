package monster

import (
	"math"
	"testing"
)

// words scripts rand() results and fails when a routine draws past them.
func words(t *testing.T, script ...uint32) (func() uint32, func() int) {
	t.Helper()
	drawn := 0
	return func() uint32 {
			if drawn >= len(script) {
				t.Fatalf("drew rand() #%d past a %d-word script", drawn+1, len(script))
			}
			drawn++
			return script[drawn-1]
		}, func() int {
			return drawn
		}
}

var nativeMonster = MonsterRef{TidWord: 0x00C6, TypeID4: 1}

func TestNativeTypeWordPacksTID4AboveTheClientWord(t *testing.T) {
	if got := NativeTypeWord(MonsterRef{TidWord: 0x00C6, TypeID4: 4}); got != 0x20C6 {
		t.Fatalf("type word = %#04x, want 0x20c6", got)
	}
	if !partyRollExcluded(0x20C6) || partyRollExcluded(0x08C6) || partyRollExcluded(0x2146) {
		t.Fatal("561020 excludes exactly monster TID4 4")
	}
	for tid4, want := range map[uint8]bool{1: false, 2: true, 3: true, 4: false} {
		if got := TradeNpcMonster(MonsterRef{TidWord: 0x00C6, TypeID4: tid4}); got != want {
			t.Errorf("TID4 %d trade NPC = %v, want %v (482640 / 4826E0)", tid4, got, want)
		}
	}
	if TradeNpcMonster(MonsterRef{TidWord: 0x0146, TypeID4: 2}) {
		t.Error("an NPC with TID4 2 is not a thief monster")
	}
}

func TestRollNativeSpawnPartyAndPromotionGrades(t *testing.T) {
	nest := NestRow{HasChampionTactics: true, ChampionGenPercentage: 20,
		ChampionTactics: ChampionTactics{Aggressive: true, SightRange: 150}}
	cases := []struct {
		name   string
		armed  bool
		script []uint32
		want   uint8
	}{
		// party roll, promotion roll, split roll, heading, replacement
		{name: "party giant", armed: true, script: []uint32{49, 19, 14, 0, 0}, want: 0x14},
		{name: "party champion", armed: true, script: []uint32{101 + 49, 0, 15, 0, 0}, want: 0x11},
		{name: "party only", armed: true, script: []uint32{0, 20, 0, 0}, want: 0x10},
		{name: "armed but lost", armed: true, script: []uint32{50, 20, 0, 0}, want: 0},
		{name: "unarmed champion", script: []uint32{0, 100, 0, 0}, want: 0x01},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draw, drawn := words(t, tc.script...)
			roll := RollNativeSpawn(nativeMonster, nest, tc.armed, draw)
			if roll.Grade() != tc.want || !roll.Nest.HasRarityOverride || drawn() != len(tc.script) {
				t.Fatalf("grade = %#02x after %d draws, want %#02x after %d", roll.Grade(), drawn(), tc.want, len(tc.script))
			}
			promoted := tc.want&0x0f != 0
			if promoted != (roll.Nest.Aggressive && roll.Nest.SightRange == 150) {
				t.Fatalf("promotion tactics swap = %+v", roll.Nest)
			}
		})
	}
}

func TestRollNativeSpawnExcludesQuestMonstersAfterThePartyDraw(t *testing.T) {
	quest := MonsterRef{TidWord: 0x00C6, TypeID4: 4}
	draw, drawn := words(t, 0, 0, 0)
	if roll := RollNativeSpawn(quest, NestRow{}, true, draw); roll.Grade() != 0 || drawn() != 3 {
		t.Fatalf("quest monster grade %#02x after %d draws, want 0 after party, heading, replacement", roll.Grade(), drawn())
	}
}

func TestRollNativeSpawnStaticGradesAndHeadings(t *testing.T) {
	nest := NestRow{HasChampionTactics: true, ChampionGenPercentage: 100, InitialDir: 32768}
	for _, monsterType := range []uint8{1, 3, 4, 5, 6, 7, 8} {
		ref := nativeMonster
		ref.MonsterType = monsterType
		script := []uint32{16383, 0}
		if monsterType == 3 {
			script = script[1:]
		}
		draw, drawn := words(t, script...)
		roll := RollNativeSpawn(ref, nest, true, draw)
		if roll.Grade() != monsterType || drawn() != len(script) {
			t.Fatalf("type %d: grade %d after %d draws", monsterType, roll.Grade(), drawn())
		}
		want := float32(float64(float32(16383.0/32767.0)) * 6.2831854820251465)
		if monsterType == 3 {
			want = InitialDirRadians(32768)
		}
		if roll.HeadingRadians != want {
			t.Fatalf("type %d heading = %v, want %v", monsterType, roll.HeadingRadians, want)
		}
	}
	// A non-monster class keeps grade 0 and the authored heading; only the
	// replacement roll draws.
	draw, drawn := words(t, 0)
	roll := RollNativeSpawn(MonsterRef{TidWord: 0x0146, MonsterType: 3}, nest, true, draw)
	if roll.Grade() != 0 || roll.HeadingRadians != InitialDirRadians(32768) || drawn() != 1 {
		t.Fatalf("non-monster roll = %+v after %d draws", roll, drawn())
	}
}

func TestInitialDirRadiansKeepsFloat32Store(t *testing.T) {
	// fdiv 65535.0, fmul 360.0, fmul qword float32(pi/180), fstp dword.
	for _, word := range []uint16{0, 1, 16384, 32768, 65535} {
		want := float32(float64(word) / 65535 * 360 * float64(float32(math.Pi/180)))
		if got := InitialDirRadians(word); got != want {
			t.Fatalf("word %d = %v, want %v", word, got, want)
		}
	}
}

func TestNativeSpawnPositionBands(t *testing.T) {
	const radius = float32(30)
	third := float32(10)
	cases := []struct {
		band     uint32
		fraction uint32
		want     float32
	}{
		{band: 0, fraction: 0, want: 2 * third},
		{band: 69, fraction: 32767, want: 3 * third},
		{band: 70, fraction: 0, want: third},
		{band: 89, fraction: 32767, want: 2 * third},
		{band: 90, fraction: 0, want: 0},
		{band: 100, fraction: 32767, want: third},
		{band: 101 + 69, fraction: 0, want: 2 * third},
	}
	for _, tc := range cases {
		draw, drawn := words(t, tc.band, tc.fraction, 0)
		x, z, moved := NativeSpawnPosition(100, 200, radius, draw)
		if !moved || drawn() != 3 || x != 100+tc.want || z != 200 {
			t.Fatalf("band %d fraction %d -> (%v, %v) moved=%v after %d draws, want x=%v", tc.band, tc.fraction, x, z, moved, drawn(), 100+tc.want)
		}
	}
	// A quarter turn moves along +z by the same float32 distance.
	draw, _ := words(t, 0, 0, 8192)
	x, z, _ := NativeSpawnPosition(100, 200, radius, draw)
	angle := float32(float64(float32(8192.0/32767.0)) * 6.2831854820251465)
	wantX := float32(float64(float32(100)) + float64(float32(math.Cos(float64(angle))))*20)
	wantZ := float32(float64(float32(200)) + float64(float32(math.Sin(float64(angle))))*20)
	if x != wantX || z != wantZ {
		t.Fatalf("quarter turn = (%v, %v), want (%v, %v)", x, z, wantX, wantZ)
	}
}

func TestNativeSpawnPositionRadiusGuards(t *testing.T) {
	draw, drawn := words(t)
	if x, z, moved := NativeSpawnPosition(5, 6, 0, draw); moved || x != 5 || z != 6 || drawn() != 0 {
		t.Fatal("a zero generate radius must keep the centre without drawing")
	}
	// 5312E: radii at or below 1 use 1.
	draw, _ = words(t, 0, 32767, 0)
	if x, _, _ := NativeSpawnPosition(0, 0, 0.5, draw); x != 1 {
		t.Fatalf("sub-unit radius reached %v, want clamped radius 1", x)
	}
}

func TestNestDelayMatches560380(t *testing.T) {
	draw, drawn := words(t)
	if interval, reduce := NestDelay(10, 10, 0, draw); interval != 10000 || reduce != 0 || drawn() != 0 {
		t.Fatal("a fixed band must not draw")
	}
	draw, _ = words(t, 32767)
	if interval, _ := NestDelay(10, 30, 0, draw); interval != 30000 {
		t.Fatalf("full fraction = %d, want the inclusive maximum", interval)
	}
	draw, _ = words(t, 16384)
	fraction := float32(16384.0 / 32767.0)
	want := uint32(int64(float64(fraction)*20000)) + 10000
	if interval, _ := NestDelay(10, 30, 0, draw); interval != want {
		t.Fatalf("half fraction = %d, want %d", interval, want)
	}
	draw, _ = words(t)
	if interval, reduce := NestDelay(10, 10, 30, draw); interval != 7000 || reduce != 3000 {
		t.Fatalf("rate 30%% = %d/%d, want 7000/3000", interval, reduce)
	}
}

func TestHalvedNestIntervalFloorsAtOneSecond(t *testing.T) {
	for in, want := range map[uint32]uint32{30000: 15000, 2002: 1001, 2000: 1000, 1500: 1000, 0: 1000} {
		if got := HalvedNestInterval(in); got != want {
			t.Fatalf("halved %d = %d, want %d", in, got, want)
		}
	}
}

func TestHiveRespawnSelectionFoldsTheExtraBucket(t *testing.T) {
	for word, want := range map[uint32]int{0: 0, 1: 1, 2: 2, 3: 2, 4: 0, 7: 2} {
		draw, _ := words(t, word)
		if got := HiveRespawnSelection(3, draw); got != want {
			t.Fatalf("word %d over 3 members = %d, want %d", word, got, want)
		}
	}
	draw, _ := words(t, 1)
	if got := HiveRespawnSelection(1, draw); got != 0 {
		t.Fatalf("single member selection = %d", got)
	}
}

func TestSpawnCollisionFallbackClasses(t *testing.T) {
	if SpawnCollisionFallsBackToCentre(nativeMonster, 0x10) {
		t.Fatal("an ordinary party monster must fail a blocked creation")
	}
	for _, grade := range []uint8{0x01, 0x03, 0x04, 0x11} {
		if !SpawnCollisionFallsBackToCentre(nativeMonster, grade) {
			t.Fatalf("grade %#02x must fall back to the centre", grade)
		}
	}
	cos := MonsterRef{TidWord: 0x01C6, TypeID4: 3}
	if !SpawnCollisionFallsBackToCentre(cos, 0) || SpawnCollisionFallsBackToCentre(MonsterRef{TidWord: 0x01C6, TypeID4: 2}, 0) {
		t.Fatal("44B460 admits COS TID4 3/4 only")
	}
}
