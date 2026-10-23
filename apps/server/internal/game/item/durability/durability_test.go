package durability

import (
	"bufio"
	"errors"
	"fmt"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestNativeSequences(t *testing.T) {
	f, e := licensed.ServerFixtureReader(t, "internal/game/item/durability/testdata/native-equipment-durability-20260921.txt")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	count := 0
	for scan.Scan() {
		var tid, current, maximum, delta, broken, flags, result, wantCurrent, wantBroken, wantFlags uint32
		if _, e := fmt.Sscan(scan.Text(), &tid, &current, &maximum, &delta, &broken, &flags, &result, &wantCurrent, &wantBroken, &wantFlags); e != nil {
			t.Fatal(e)
		}
		s := State{current, flags, broken != 0}
		got, e := s.Offset(uint16(tid), maximum, int32(delta), true)
		if e != nil || got != result || s.Current != wantCurrent || s.Broken != (wantBroken != 0) || s.Flags != wantFlags {
			t.Fatalf("row %d got=%d state=%+v error=%v native=%s", count, got, s, e, scan.Text())
		}
		count++
	}
	if e := scan.Err(); e != nil {
		t.Fatal(e)
	}
	if count != 768 {
		t.Fatalf("rows=%d", count)
	}
}

func TestWriteAuthorityAndAssertionDomains(t *testing.T) {
	s := State{Current: 1}
	v, e := s.Offset(0x32c, 100, -1, false)
	if !errors.Is(e, ErrWriteDenied) || v != 0 || s.Current != 1 || !s.Broken || s.Flags != 0 {
		t.Fatalf("gate: %d %+v %v", v, s, e)
	}
	s = State{Current: 0}
	_, e = s.Offset(0x32c, 100, -1, true)
	if !errors.Is(e, ErrNativeDomain) || s != (State{}) {
		t.Fatalf("negative: %+v %v", s, e)
	}
	_, e = s.Offset(0x32c, 0x80000000, 1, true)
	if !errors.Is(e, ErrNativeDomain) {
		t.Fatal(e)
	}
	s = State{Current: 3}
	_, e = s.Offset(0x32c, 100, 0, false)
	if e != nil || s.Flags != 0 {
		t.Fatalf("unchanged value requires no authority: %+v %v", s, e)
	}
}
