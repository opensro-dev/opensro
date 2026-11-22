/*
===========================================================================

character_mostwanted.go - a suited player's most-wanted streak record

The persisted half of the port-only most-wanted rule (#1738 family):
the counted-kill streak a job-suited character carries on its record.
action/mostwanted.go owns the rules that judge and decay it; this file
owns the record they judge.

===========================================================================
*/

package domain

/*
================
MostWanted

A suited player's streak of counted job kills (port-only, not native;
action/mostwanted.go). Death clears it; the wanted level also decays
one level per day without a counted kill (NextDecayAtMs). Taking the
suit off keeps it: only the bonuses shelve while the suit is off.
================
*/
type MostWanted struct {
	Streak int64 `json:"streak"`
	// ExpiresAtMs was the seven-day lapse stamp (schema 35), retired
	// by the decay at schema 40: no reader remains, and the field
	// stays only so records a rolled-back release writes still
	// decode. Dropping it is a later schema step's work.
	ExpiresAtMs int64 `json:"expiresAtMs,omitempty"`
	// NextDecayAtMs is when the next decay step lands (Unix
	// milliseconds), restamped by every counted kill. Streaks saved
	// before the field existed carry zero: the first door that judges
	// the streak stamps a fresh day instead of dropping a level, so
	// no saved streak ever decays early.
	NextDecayAtMs int64 `json:"nextDecayAtMs,omitempty"`
}
