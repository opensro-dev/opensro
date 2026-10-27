package monster

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed data/v1188_hive_caps.tsv
var hiveCapsTSV string

type hiveCap struct {
	WorldCode string
	Key       string
	Limit     int
	// Order is the member's rank in native hive order (ascending dwNestID,
	// the reference server's hive ordering). Overwrite hives fill and pick
	// alternate locations in this order.
	Order   int
	Density HiveDensityPolicy
}

func loadHiveCaps(text string) map[populationEvidenceKey]hiveCap {
	rows := make(map[populationEvidenceKey]hiveCap)
	limits := make(map[string]int)
	policies := make(map[string]HiveDensityPolicy)
	worlds := make(map[string]string)
	for lineNumber, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 13 {
			panic(fmt.Sprintf("hive evidence line %d: expected thirteen columns", lineNumber+1))
		}
		key := evidenceKey(c[0], uint16(mustEvidenceUint(c[1], 16, lineNumber)), mustEvidenceFloat(c[2], lineNumber), mustEvidenceFloat(c[3], lineNumber), mustEvidenceFloat(c[4], lineNumber))
		cap := hiveCap{Key: c[5], Limit: mustEvidenceInt(c[6], lineNumber), Order: mustEvidenceInt(c[7], lineNumber)}
		cap.WorldCode = c[12]
		if cap.WorldCode == "" {
			panic("hive evidence has no world owner")
		}
		if old, ok := worlds[cap.Key]; ok && old != cap.WorldCode {
			panic("hive crosses world definitions")
		}
		worlds[cap.Key] = cap.WorldCode
		cap.Density = HiveDensityPolicy{Kind: uint8(mustEvidenceUint(c[8], 8, lineNumber)), MonstersPerPC: float32(mustEvidenceFloat(c[9], lineNumber)), Step: uint32(mustEvidenceUint(c[10], 32, lineNumber)), Maximum: uint32(mustEvidenceUint(c[11], 32, lineNumber))}
		if len(cap.Key) != 64 || cap.Limit < 0 || cap.Order < 0 {
			panic("invalid hive identity/limit/order")
		}
		if old, ok := rows[key]; ok && old != cap {
			panic("conflicting natural-key hive evidence")
		}
		if old, ok := limits[cap.Key]; ok && old != cap.Limit {
			panic("inconsistent hive cap")
		}
		if old, ok := policies[cap.Key]; ok && old != cap.Density {
			panic("inconsistent hive density policy")
		}
		rows[key] = cap
		limits[cap.Key] = cap.Limit
		policies[cap.Key] = cap.Density
	}
	return rows
}
