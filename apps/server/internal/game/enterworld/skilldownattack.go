package enterworld

// SkillDownAttack is the optional da block. Presence is separate from its
// unsigned value: an authored zero suppresses damage against a downed target.
// Client 7F9F7D reads the +14 block; the server applies it only
// when the target's motion-state accessor returns 8.
type SkillDownAttack struct {
	Present bool
	Percent uint32
}
