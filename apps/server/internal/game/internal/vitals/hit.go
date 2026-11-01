package vitals

// HitDebit is the numeric gate in the server's hit handlers. The wire dword
// is interpreted as signed damage before HP subtraction; its high bit must
// not turn an overflowed or negative amount into an unsigned killing blow.
// Attribution receives the original amount separately. This is not the full
// hit dispatcher (world policy, callbacks and death ownership are separate).
func HitDebit(current, damage uint32) uint32 {
	if int32(damage) <= 0 {
		return 0
	}
	return min(current, damage)
}
