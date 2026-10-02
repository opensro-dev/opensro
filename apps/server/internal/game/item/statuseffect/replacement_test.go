package statuseffect

import "testing"

func TestReplacementNativeBranches(t *testing.T) {
	base := ReplacementDescriptor{Category: 3, Group: 7, Rank: 2, BasicCode: "same"}
	candidate := func(d ReplacementDescriptor) []ReplacementCandidate {
		return []ReplacementCandidate{{Descriptor: d, Mode: 2}}
	}
	conflict := CastingConflictSnapshot{CurrentPacked: 5}
	tests := []struct {
		name     string
		in       ReplacementDescriptor
		old      []ReplacementCandidate
		self     bool
		conflict CastingConflictSnapshot
		want     ReplacementDecision
	}{
		{name: "new family", in: base, want: ReplacementDecision{true, -1, false}},
		{name: "same rank retires", in: base, old: candidate(base), want: ReplacementDecision{true, 0, false}},
		{name: "higher rank retires", in: base, old: candidate(ReplacementDescriptor{Category: 3, Group: 7, Rank: 1}), want: ReplacementDecision{true, 0, false}},
		{name: "lower rank rejected", in: base, old: candidate(ReplacementDescriptor{Category: 3, Group: 7, Rank: 3}), want: ReplacementDecision{false, -1, false}},
		{name: "protected old skipped", in: base, old: candidate(ReplacementDescriptor{Category: 3, Group: 7, Rank: 3, Cbuf: true}), want: ReplacementDecision{true, -1, false}},
		{name: "non buff old skipped", in: base, old: candidate(ReplacementDescriptor{Category: 4, Group: 7, Rank: 3}), want: ReplacementDecision{true, -1, false}},
		{name: "zero group never matches", in: ReplacementDescriptor{Category: 3}, old: candidate(ReplacementDescriptor{Category: 3}), want: ReplacementDecision{true, -1, false}},
		{name: "new lks2 skips group", in: ReplacementDescriptor{Category: 3, Group: 7, Lks2: true}, old: candidate(base), want: ReplacementDecision{true, -1, false}},
		{name: "old source link skips group", in: base, old: []ReplacementCandidate{{Descriptor: ReplacementDescriptor{Category: 3, Group: 7, Lnks: true, Rank: 3}, Mode: 1}}, want: ReplacementDecision{true, -1, false}},
		{name: "old recipient link compares rank", in: base, old: candidate(ReplacementDescriptor{Category: 3, Group: 7, Lnks: true, Rank: 3}), want: ReplacementDecision{false, -1, false}},
		{name: "conflict rejects when no replacement", in: ReplacementDescriptor{Category: 3, PackedStates: 5}, conflict: conflict, want: ReplacementDecision{false, -1, false}},
		{name: "replacement bypasses conflict", in: ReplacementDescriptor{Category: 3, Group: 7, Rank: 2, PackedStates: 5}, old: candidate(base), conflict: conflict, want: ReplacementDecision{true, 0, false}},
		{name: "msch one skips replacement", in: ReplacementDescriptor{Category: 3, Group: 7, Rank: 2, MschPresent: true, MschMode: 1, PackedStates: 5}, old: candidate(base), conflict: conflict, want: ReplacementDecision{false, -1, false}},
		{name: "present msch zero still replaces", in: ReplacementDescriptor{Category: 3, Group: 7, Rank: 2, MschPresent: true}, old: candidate(base), want: ReplacementDecision{true, 0, false}},
		{name: "first decisive row wins", in: base, old: []ReplacementCandidate{{Descriptor: base}, {Descriptor: ReplacementDescriptor{Category: 3, Group: 7, Rank: 3}}}, want: ReplacementDecision{true, 0, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideReplacement(tt.in, tt.old, tt.self, tt.conflict); got != tt.want {
				t.Fatalf("got %+v want %+v", got, tt.want)
			}
		})
	}
}

func TestReplacementDttpAndAreaBranches(t *testing.T) {
	incoming := ReplacementDescriptor{Category: 3, DttpPresent: true, DttpKind: 0, DttpRank: 2, PackedStates: 5}
	old := ReplacementCandidate{Descriptor: ReplacementDescriptor{Category: 3, DttpPresent: true, DttpKind: 0, DttpRank: 3}, Mode: 2}
	blocked := CastingConflictSnapshot{CurrentPacked: 5}
	if got := DecideReplacement(incoming, []ReplacementCandidate{old}, true, blocked); got != (ReplacementDecision{true, -1, false}) {
		t.Fatal("self Dttp must accept without retiring", got)
	}
	if got := DecideReplacement(incoming, []ReplacementCandidate{old}, false, blocked); got.Allowed {
		t.Fatal("lower Dttp rank accepted")
	}
	incoming.DttpRank = 3
	if got := DecideReplacement(incoming, []ReplacementCandidate{old}, false, blocked); got != (ReplacementDecision{true, 0, false}) {
		t.Fatal("equal Dttp rank", got)
	}
	old.Mode = 1
	if got := DecideReplacement(incoming, []ReplacementCandidate{old}, false, blocked); got.Allowed {
		t.Fatal("mode 1 must skip to conflict check")
	}
	incoming = ReplacementDescriptor{Category: 3, Group: 8, Rank: 2, BasicCode: "area", Efr2: true}
	old = ReplacementCandidate{Descriptor: incoming, Mode: 2, HasAreaLink: true, LinkedPeerFound: true}
	if got := DecideReplacement(incoming, []ReplacementCandidate{old}, false, blocked); got != (ReplacementDecision{true, 0, true}) {
		t.Fatal("linked pair not retired", got)
	}
	// Each failed area predicate skips the group replacement branch entirely.
	for _, mutate := range []func(*ReplacementCandidate){
		func(c *ReplacementCandidate) { c.Descriptor.BasicCode = "different" },
		func(c *ReplacementCandidate) { c.Descriptor.Rank = 3 },
		func(c *ReplacementCandidate) { c.Descriptor.Efr2 = false },
		func(c *ReplacementCandidate) { c.Descriptor.MatchesExecutionSelector = true },
		func(c *ReplacementCandidate) { c.HasAreaLink = false },
		func(c *ReplacementCandidate) { c.LinkedPeerFound = false },
	} {
		changed := old
		mutate(&changed)
		if got := DecideReplacement(incoming, []ReplacementCandidate{changed}, false, blocked); got != (ReplacementDecision{true, -1, false}) {
			t.Fatal("unresolved area link used group fallback", got)
		}
	}
	incoming.MatchesExecutionSelector = true
	if got := DecideReplacement(incoming, []ReplacementCandidate{old}, false, blocked); got != (ReplacementDecision{true, 0, false}) {
		t.Fatal("selector must use ordinary group branch", got)
	}
}

func TestCastingConflictPackedByteSemantics(t *testing.T) {
	for state := 0; state < 256; state++ {
		s := CastingConflictSnapshot{}
		s.Active[state/64] = uint64(1) << uint(state%64)
		want := state != 0 && state != 0x1d && state != 0x23
		for shift := uint(0); shift < 24; shift += 8 {
			if got := s.Conflicts(uint32(state) << shift); got != want {
				t.Fatalf("state %d byte %d: %v", state, shift/8, got)
			}
		}
		if s.Conflicts(uint32(state) << 24) {
			t.Fatal("high byte incorrectly consumed", state)
		}
		s.Active = [4]uint64{}
		s.CurrentOvl2 = uint32(state) << 16
		if got := s.Conflicts(uint32(state)); got != want {
			t.Fatal("current overlap not compared", state)
		}
	}
	// The high byte of current packed words is ignored too.
	if (CastingConflictSnapshot{CurrentPacked: 0x05000000}).Conflicts(5) {
		t.Fatal("high current byte consumed")
	}
}
