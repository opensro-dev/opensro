package monster

import "testing"

func TestHomingCandidateIgnoresVerticalDeltaAndConsumesExactAnchorDraw(t *testing.T) {
	home := Pose{RegionID: 25000, X: 1000, Y: 20, Z: 1000}
	current := home
	current.X += 600
	current.Y += 800
	calls := 0
	random := func() uint32 { calls++; return 0 }
	got := HomingCandidate(home, current, 300, MonsterRef{TidWord: 0xc6}, random)
	if got.X != 1100 || got.Y != 20 || got.Z != 1000 || calls != 1 {
		t.Fatalf("planar home candidate: %+v calls=%d", got, calls)
	}
	got = HomingCandidate(home, current, 300, MonsterRef{TidWord: 0x246, TypeID4: 4}, random)
	if got != home || calls != 2 {
		t.Fatalf("exact anchor branch changed its draw: %+v %d", got, calls)
	}
	// Neither an ordinary unique nor the adjacent actor subtype is exact-home.
	for _, ref := range []MonsterRef{{TidWord: 0xc6, TypeID4: 4, MonsterType: 3}, {TidWord: 0x246, TypeID4: 1}} {
		if HomingUsesExactAnchor(ref) {
			t.Fatalf("wrong class bypass: %+v", ref)
		}
	}
	if got := HomingCandidate(home, home, 300, MonsterRef{}, random); got != home {
		t.Fatal("zero-length normalization")
	}
	if got := HomingCandidate(home, current, 0, MonsterRef{}, random); got != home {
		t.Fatal("zero radius")
	}
}

func TestNavigationPathOwnsAllLiveReadsAndExpiresOnReplacement(t *testing.T) {
	from, to := Pose{RegionID: 25000, X: 10, Y: 20}, Pose{RegionID: 25000, X: 100, Y: 20}
	m := MoverState{From: from, To: to, DepartMs: 1000, ArriveMs: 2000}
	m.AdoptNavigation(NewNavigationPath(from, to, to, 0, func(f float64, _ Pose) (float64, bool) { return 20 + 40*f*(1-f), true }))
	poison := func(Pose) (float64, bool) {
		t.Fatal("terrain replaced owned deck")
		return 0, false
	}
	if m.LivePoseAt(1500, nil).Y != 30 || m.LivePoseAt(1500, poison).Y != 30 {
		t.Fatal("owned cell plane was replaced by chord")
	}
	m.To.Y = 60
	if m.LivePoseAt(1500, nil).Y != 40 {
		t.Fatal("replaced segment reused old surface")
	}
	m.From, m.To = Pose{}, Pose{}
	m.DepartMs, m.ArriveMs = 0, 0
	m.Pose = from
	if m.LivePoseAt(1600, poison) != from {
		t.Fatal("settle retained old surface")
	}
}
