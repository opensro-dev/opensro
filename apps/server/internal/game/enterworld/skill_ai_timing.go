package enterworld

// AICommandDurationMs is the server's command-result duration, not
// the selector's repeating interval. Parameter 140's native default is 100
// (the static default table); the explicit input also preserves its modifier
// boundary. Float32 spills occur before and after percentage multiplication.
func (r SkillRow) AICommandDurationMs(percent float32) uint32 {
	if r.ActionKind != 2 || r.ActionDurationMs == 0 {
		return r.CoolTimeMs
	}
	sum := float32(float64(r.ActionCastingTimeMs) + float64(r.ActionDurationMs))
	return uint32(int64(float32(float64(sum) * float64(percent) / 100)))
}
