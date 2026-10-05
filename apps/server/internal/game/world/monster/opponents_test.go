package monster

import "testing"

func TestOpponentPoliciesAndScores(t *testing.T) {
	candidates := [3]OpponentCandidate{{GID: 1, Eligible: true, Distance: 10}, {GID: 2, Eligible: true, Distance: 20}, {GID: 3, Eligible: true, Distance: 30}}
	for _, policy := range []uint8{0, 1, 2} {
		var rows [2]Opponent
		if got := RecordOpponentHit(&rows, policy, 1, 10, 100, 0, 1000, 1000, candidates); got != 1 {
			t.Fatal(got)
		}
		got := RecordOpponentHit(&rows, policy, 2, 20, 50, 0, 1100, 1000, candidates)
		want := uint32(1)
		if policy == 1 {
			want = 2
		}
		if got != want {
			t.Fatalf("policy %d: %d", policy, got)
		}
		got = RecordOpponentHit(&rows, policy, 2, 20, 100, 0, 1200, 1000, candidates)
		if policy != 0 {
			want = 2
		}
		if got != want {
			t.Fatalf("policy %d stronger: %d", policy, got)
		}
		if policy == 2 && (rows[0].GID != 2 || rows[0].Damage != 40 || rows[0].Aggression != 150 || rows[0].HitCount != 2) {
			t.Fatalf("record %+v", rows)
		}
	}
}

func TestOpponentExpiryInvalidationAndThirdAttacker(t *testing.T) {
	candidates := [3]OpponentCandidate{{GID: 1, Eligible: true, Distance: 10}, {GID: 2, Eligible: true, Distance: 20}, {GID: 3, Eligible: true, Distance: 30}}
	rows := [2]Opponent{{GID: 1, Aggression: 100, LastHitMs: 1000}, {GID: 2}}
	if got := RecordOpponentHit(&rows, 2, 2, 1, 1, 0, 2999, 1000, candidates); got != 1 {
		t.Fatal("early expiry")
	}
	if got := RecordOpponentHit(&rows, 2, 2, 1, 1, 0, 3000, 1000, candidates); got != 2 {
		t.Fatal("missed exact expiry")
	}
	before := rows
	if got := RecordOpponentHit(&rows, 2, 3, 1, 999, 0, 3001, 1000, candidates); got != 2 || rows != before {
		t.Fatal("third distant attacker displaced records")
	}
	candidates[2].Distance = 5
	if got := RecordOpponentHit(&rows, 2, 3, 1, 999, 0, 3002, 1000, candidates); got != 3 || rows[0].Aggression != 0 || rows[1].GID != 2 {
		t.Fatalf("third near %+v", rows)
	}
	candidates[2].Eligible = false
	if got := RecordOpponentHit(&rows, 2, 2, 1, 1, 0, 3003, 1000, candidates); got != 2 || rows[1] != (Opponent{}) {
		t.Fatalf("invalid primary %+v", rows)
	}
}

func TestOpponentSignedArithmeticAndClockWrap(t *testing.T) {
	rows := [2]Opponent{{GID: 1, Aggression: 0x7fffffff, HitCount: 255}}
	RecordOpponentHit(&rows, 2, 1, 1, 1, 0, 0, 1000, [3]OpponentCandidate{})
	if rows[0].Aggression != 0 || rows[0].HitCount != 0 {
		t.Fatal(rows)
	}
	rows = [2]Opponent{{GID: 1, Aggression: 100, LastHitMs: 0xfffffff0}, {GID: 2}}
	if got := RecordOpponentHit(&rows, 2, 2, 1, 1, 0, 16, 16, [3]OpponentCandidate{{GID: 1, Eligible: true, Distance: 1}, {GID: 2, Eligible: true, Distance: 2}}); got != 2 {
		t.Fatal("clock rollover", got)
	}
}

/*
================
TestImmobileUniqueIgnoresHateFromBeyondSight

5473C0's first branch needs a unique, both live speeds at most zero, and an
attacker strictly beyond the row's SightRange.
================
*/
func TestImmobileUniqueIgnoresHateFromBeyondSight(t *testing.T) {
	unique := Instance{Ref: MonsterRef{MonsterType: gradeUnique}}
	unique.Nest.HasControls, unique.Nest.Controls.SightRange = true, 140
	if !unique.IgnoresDistantHate(141) || unique.IgnoresDistantHate(140) {
		t.Fatal("the sight edge is not strict")
	}
	moving := unique
	moving.Ref.RunSpeed = 80
	if moving.IgnoresDistantHate(500) {
		t.Fatal("a unique that can run ignored distant hate")
	}
	ordinary := unique
	ordinary.Ref.MonsterType = 0
	if ordinary.IgnoresDistantHate(500) {
		t.Fatal("an ordinary monster ignored distant hate")
	}
	rows := [2]Opponent{{GID: 7, Aggression: 50, Damage: 9}, {GID: 8, Aggression: 30}}
	ZeroOpponentAggression(&rows, 8)
	if rows[1].Aggression != 0 || rows[0].Aggression != 50 {
		t.Fatal("the secondary's aggression was not the one zeroed")
	}
	ZeroOpponentAggression(&rows, 7)
	if rows[0].Aggression != 0 || rows[0].Damage != 9 {
		t.Fatal("the primary lost more than its aggression")
	}
}
