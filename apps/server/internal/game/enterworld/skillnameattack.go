package enterworld

// nativeNameAttackContent projects client 7F85A0. 84B2F0 installs pointers for
// these blocks even when their numeric parameters are zero.
func nativeNameAttackContent(fields []string) bool {
	if len(fields) <= 68 {
		return false
	}
	if len(fields) > 118 {
		fields = fields[:118]
	} // exactly fifty native tail words
	mode, modeOK := textdataInt(fields[68])     // info+194 / record+828
	target, targetOK := textdataInt(fields[22]) // record+72C
	if !modeOK || !targetOK || mode == 4 || target == 0 || encodedTailContainsTag(fields, 0x61626e62) {
		return false
	}
	for _, tag := range []int64{
		0x617474, 0x667a, 0x6662, 0x6573, 0x6275, 0x7073, 0x7a62,
		0x7365, 0x7274, 0x736c, 0x6665, 0x6d79, 0x626c, 0x646e,
		0x7374, 0x6473, 0x6361, 0x63737372, 0x63736974, 0x63737064,
		0x63736d64, 0x63736870, 0x63736d70, 0x6c667374, 0x7462,
		0x636b, 0x70646d67, 0x74657264, 0x74687264, 0x6869746d, 0x746e7432,
	} {
		if encodedTailContainsTag(fields, tag) {
			return true
		}
	}
	return false
}
