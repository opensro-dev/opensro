package combat

import (
	"bufio"
	"fmt"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestNativeResourceOffsets(t *testing.T) {
	f, err := licensed.ServerFixtureReader(t, "internal/game/combat/testdata/native-resource-offsets-20260920.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	count := 0
	for s.Scan() {
		var costs, life, hp, mp, oldHP, oldMP, wantHP, wantMP, cacheHP, cacheMP, dirty uint32
		var delta int32
		if _, err := fmt.Sscan(s.Text(), &costs, &life, &hp, &mp, &delta, &oldHP, &oldMP, &wantHP, &wantMP, &cacheHP, &cacheMP, &dirty); err != nil {
			t.Fatal(err)
		}
		// The action owner enforces the alive guard; these helpers implement
		// the arithmetic after that guard, without creating a second cache.
		if life != 1 {
			continue
		}
		got := mp
		if costs == 1 {
			got = ConsumeMana(mp, 80, delta)
		} else if delta != 0 {
			got = OffsetVital(mp, 80, delta)
		}
		if got != wantMP {
			t.Fatalf("row %s: MP %d, want %d", s.Text(), got, wantMP)
		}
		count++
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 112 {
		t.Fatalf("incomplete evidence: %d", count)
	}
}
