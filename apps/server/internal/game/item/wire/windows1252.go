package wire

// DecodeWindows1252 matches the web ANSI decoder, including the five control
// code points that Windows retains in the nominally undefined byte slots.
func DecodeWindows1252(value []byte) string {
	const controls = "\u20ac\u0081\u201a\u0192\u201e\u2026\u2020\u2021\u02c6\u2030\u0160\u2039\u0152\u008d\u017d\u008f\u0090\u2018\u2019\u201c\u201d\u2022\u2013\u2014\u02dc\u2122\u0161\u203a\u0153\u009d\u017e\u0178"
	table := []rune(controls)
	text := make([]rune, len(value))
	for i, b := range value {
		text[i] = rune(b)
		if b >= 0x80 && b < 0xa0 {
			text[i] = table[b-0x80]
		}
	}
	return string(text)
}

// EncodeWindows1252 implements the measured Windows best-fit conversion for
// the web port's ANSI lane. Native code uses WideCharToMultiByte(CP_ACP, 0);
// the web client selects code page 1252 explicitly. This is not UTF-8, Latin-1
// truncation, or a claim that every native regional build uses this code page.
func EncodeWindows1252(value string) []byte {
	encoded := make([]byte, 0, len(value))
	for _, r := range value {
		if r < 256 {
			encoded = append(encoded, windows1252Low[r])
		} else if r > 0xffff {
			// Native flags-zero conversion defaults each UTF-16 surrogate unit.
			encoded = append(encoded, '?', '?')
		} else if b, ok := windows1252BestFit[r]; ok {
			encoded = append(encoded, b)
		} else {
			encoded = append(encoded, '?')
		}
	}
	return encoded
}
