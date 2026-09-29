/*
===========================================================================

effectsnapshot.go - immutable linked-effect identities in monster snapshots

The action registry owns clocks and source/recipient pairing. Monsters retain
only the skill/token projection needed when a viewer enters their interest.

===========================================================================
*/

package monster

import "iter"

/*
================
AttachedSkill

These linked programs have neither an efta status byte nor a remaining-time
word in a remote spawn row (v1.150 85FB20).
================
*/
type AttachedSkill struct {
	SkillID, Token uint32
}

/*
================
EffectSnapshot

Rows are private and immutable after construction. Instance remains comparable
and copying it cannot give a reader mutable access to authoritative effects.
================
*/
type EffectSnapshot struct {
	rows []AttachedSkill
}

/*
================
Len

Nil is the compact representation of an actor without linked effects.
================
*/
func (s *EffectSnapshot) Len() int {
	if s == nil {
		return 0
	}
	return len(s.rows)
}

/*
================
Entries

Yield detached values without exposing the backing slice.
================
*/
func (s *EffectSnapshot) Entries() iter.Seq[AttachedSkill] {
	return func(yield func(AttachedSkill) bool) {
		if s == nil {
			return
		}
		for _, row := range s.rows {
			if !yield(row) {
				return
			}
		}
	}
}

/*
================
With

Return a new snapshot. Existing tokens cannot change skill identity.
================
*/
func (s *EffectSnapshot) With(row AttachedSkill) *EffectSnapshot {
	out := &EffectSnapshot{rows: make([]AttachedSkill, 0, s.Len()+1)}
	for old := range s.Entries() {
		if old.Token == row.Token {
			return s
		}
		out.rows = append(out.rows, old)
	}
	out.rows = append(out.rows, row)
	return out
}

/*
================
Without

Removing a token never mutates snapshots already held by interest readers.
================
*/
func (s *EffectSnapshot) Without(token uint32) *EffectSnapshot {
	if s == nil {
		return nil
	}
	var out EffectSnapshot
	for _, row := range s.rows {
		if row.Token != token {
			out.rows = append(out.rows, row)
		}
	}
	if len(out.rows) == len(s.rows) {
		return s
	}
	if len(out.rows) == 0 {
		return nil
	}
	return &out
}
