/*
===========================================================================

zerocap_evidence.go - ISRO-R live caps for the nests the v1.188 shard zeroed

Generated evidence (scripts/build/import_isror_zero_caps.py) keyed like the
population evidence: codename, region and float32 anchor. restoreZeroCapNests
(catalog.go) reads it.

===========================================================================
*/
package monster

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed data/isror_zero_cap_caps.tsv
var zeroCapCapsTSV string

/*
==================
loadZeroCapCaps
==================
*/
func loadZeroCapCaps(text string) map[populationEvidenceKey]int {
	caps := make(map[populationEvidenceKey]int)
	for lineNumber, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 7 {
			panic(fmt.Sprintf("zero-cap evidence line %d: expected seven columns", lineNumber+1))
		}
		key := evidenceKey(c[0], uint16(mustEvidenceUint(c[1], 16, lineNumber)), mustEvidenceFloat(c[2], lineNumber), mustEvidenceFloat(c[3], lineNumber), mustEvidenceFloat(c[4], lineNumber))
		cap := mustEvidenceInt(c[5], lineNumber)
		if cap <= 0 {
			panic("zero-cap evidence holds a non-positive cap")
		}
		if old, ok := caps[key]; ok && old != cap {
			panic("conflicting zero-cap evidence for " + c[0])
		}
		caps[key] = cap
	}
	return caps
}
