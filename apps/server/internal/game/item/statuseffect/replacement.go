package statuseffect

import "strings"

// ReplacementDescriptor contains only native 59D870 inputs. It describes the
// authored program, not an assertion that the entire skill is executable.
// Presence flags distinguish an absent instruction from a present zero word.
type ReplacementDescriptor struct {
	Activity                 uint8
	EventCancelMask          uint8 // skc second argument, native ref+490[1]
	Category                 uint32
	Group                    uint32
	Rank                     uint8
	BasicCode                string
	MschPresent              bool
	MschMode                 uint32
	Cbuf                     bool
	DttpPresent              bool
	DttpKind, DttpRank       uint32
	Efr2                     bool
	MatchesExecutionSelector bool
	Lnks, Lks2               bool
	Ovl2Present              bool
	Ovl2, PackedStates       uint32
}

// ReplacementCandidate is a detached active-list row. LinkedPeerFound is the
// result of resolving context+6C's source actor and FindActiveBuffBySkillID for
// this old skill. The transaction owner must resolve and retire both halves;
// neither a missing actor nor a missing peer is an automatic replacement.
type ReplacementCandidate struct {
	Descriptor                   ReplacementDescriptor
	Mode                         uint8 // execution context +20, distinct from effect lifecycle state
	HasAreaLink, LinkedPeerFound bool
}

// CastingConflictSnapshot is the native manager's 256-bit state set and the
// current command's packed states. Only the low three bytes of packed words
// participate. The owner supplies zeros when no current descriptor exists.
type CastingConflictSnapshot struct {
	Active                     [4]uint64
	CurrentPacked, CurrentOvl2 uint32
}

func (s CastingConflictSnapshot) Conflicts(packed uint32) bool {
	for shift := uint(0); shift < 24; shift += 8 {
		state := uint8(packed >> shift)
		if state == 0 || state == 0x1d || state == 0x23 {
			continue
		}
		if s.Active[state/64]&(uint64(1)<<(state%64)) != 0 {
			return true
		}
		for other := uint(0); other < 24; other += 8 {
			if uint8(s.CurrentPacked>>other) == state || uint8(s.CurrentOvl2>>other) == state {
				return true
			}
		}
	}
	return false
}

// ReplacementDecision preserves native's first-decisive-row behavior. A
// successful replacement bypasses the later conflict check; an accepted Dttp
// cast by the recipient itself does not retire the old row. RetireIndex is -1
// when no retirement was requested. Retirement is deferred, never list erasure.
type ReplacementDecision struct {
	Allowed          bool
	RetireIndex      int
	RetireLinkedPeer bool
}

func DecideReplacement(in ReplacementDescriptor, existing []ReplacementCandidate, casterIsRecipient bool, conflicts CastingConflictSnapshot) ReplacementDecision {
	accept := ReplacementDecision{Allowed: true, RetireIndex: -1}
	reject := ReplacementDecision{RetireIndex: -1}
	if in.Category == 3 && !(in.MschPresent && in.MschMode == 1) {
		for index, row := range existing {
			old := row.Descriptor
			if old.Category != 3 || old.Cbuf {
				continue
			}
			if in.DttpPresent && old.DttpPresent && in.DttpKind == old.DttpKind {
				if casterIsRecipient {
					return accept
				}
				if row.Mode == 1 {
					continue
				}
				if in.DttpRank < old.DttpRank {
					return reject
				}
				return ReplacementDecision{Allowed: true, RetireIndex: index}
			}
			if in.Efr2 && !in.MatchesExecutionSelector {
				if old.BasicCode == in.BasicCode && in.Rank >= old.Rank && old.Efr2 && !old.MatchesExecutionSelector && row.HasAreaLink && row.LinkedPeerFound {
					return ReplacementDecision{Allowed: true, RetireIndex: index, RetireLinkedPeer: true}
				}
				continue
			}
			switch tierOrder(in, old) {
			case 1:
				return ReplacementDecision{Allowed: true, RetireIndex: index}
			case -1:
				// The casting-state bits are not reference counted (59DC00):
				// the weaker tier's retirement cleared the shared bits, so the
				// conflict check below cannot be relied on to refuse it.
				return reject
			}
			if in.Lks2 || old.Lnks && row.Mode == 1 || in.Group == 0 || in.Group != old.Group {
				continue
			}
			if in.Rank < old.Rank {
				return reject
			}
			return ReplacementDecision{Allowed: true, RetireIndex: index}
		}
	}
	if in.Ovl2Present && conflicts.Conflicts(in.Ovl2) || conflicts.Conflicts(in.PackedStates) {
		return reject
	}
	return accept
}

/*
================
tierOrder

DELIBERATE DEVIATION from 59D870. Native replacement stays inside one skill
group, so a higher tier of the same buff line (Life Turnover over Life
Control, Earth Fence over Earth Barrier) is refused by their shared casting
states while the lower tier lasts. The port lets the stronger tier replace
the weaker one: same line (basic code minus its tier letter), the same
casting states, and a later tier letter. A weaker tier over a stronger
one is refused. Returns 1 to replace, -1 to refuse, 0 when unrelated.
================
*/
func tierOrder(in, old ReplacementDescriptor) int {
	if in.Group == 0 || in.Group == old.Group || in.PackedStates != old.PackedStates ||
		in.Ovl2Present != old.Ovl2Present || in.Ovl2 != old.Ovl2 {
		return 0
	}
	inLine, inTier, ok := buffTier(in.BasicCode)
	oldLine, oldTier, oldOK := buffTier(old.BasicCode)
	switch {
	case !ok || !oldOK || inLine != oldLine:
		return 0
	case inTier > oldTier:
		return 1
	case inTier < oldTier:
		return -1
	}
	return 0
}

/*
================
buffTier

Split a basic code such as SKILL_EU_WIZARD_MENTALA_DAMAGEUP_B into its line
and its single-letter tier.
================
*/
func buffTier(code string) (string, byte, bool) {
	cut := strings.LastIndexByte(code, '_')
	if cut <= 0 || len(code)-cut != 2 || code[cut+1] < 'A' || code[cut+1] > 'Z' {
		return "", 0, false
	}
	return code[:cut], code[cut+1], true
}
