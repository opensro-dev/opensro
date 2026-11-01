// Package textguard preserves the native shard's byte-oriented text admission.
// It is gameplay compatibility, not a replacement for parameterized DB queries.
package textguard

import "bytes"

// Rejected reports the native rejected-text predicate for the web port's Windows-1252 lane.
// Input is encoded bytes, not UTF-8. The native copy lowercases ASCII, copying each
// high-bit byte together with its following byte without case conversion.
func Rejected(input []byte) bool {
	if end := bytes.IndexByte(input, 0); end >= 0 {
		input = input[:end]
	}
	normalized := append([]byte(nil), input...)
	for i := 0; i < len(normalized); i++ {
		if normalized[i] >= 0x80 {
			i++
			continue
		}
		if normalized[i] >= 'A' && normalized[i] <= 'Z' {
			normalized[i] += 'a' - 'A'
		}
	}
	// CharNextA advances one byte in CP1252. Quote rejection precedes the
	// substring scan, even for a quote copied after a high-bit byte.
	if bytes.IndexByte(normalized, '\'') >= 0 || bytes.IndexByte(normalized, '"') >= 0 {
		return true
	}
	for _, token := range rejectedSubstrings {
		if bytes.Contains(normalized, []byte(token)) {
			return true
		}
	}
	return false
}
