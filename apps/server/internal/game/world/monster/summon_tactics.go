package monster

import (
	"strings"
)

// 54BDE0 partitions default tactics by ChampionTacticsID (runtime +8C).
// 54C0E0/54C1C0: own reference, then original reference; grade zero uses
// the normal list, nonzero the other list; missing list flips preference.
// Multiple candidates consume one CRT modulo draw; singleton lists do not.

// SummonTacticsTable maps a reference codename to its normal and other
// tactics lists (Evidence.SummonTactics). A nil table resolves nothing.
type SummonTacticsTable map[string][2][]SummonTactics

type SummonTactics struct {
	TargetPolicy      uint8
	SightRange        float64
	NativeFlags       uint32
	Controls          TacticsControls
	HasControls       bool
	ConditionalSkills [8]ConditionalSkill
}

// A catalog-owned AI-skill binding resolved by codename against the
// target version. Source tactics IDs never become target skill IDs.
type ConditionalSkill struct {
	TacticsID, SkillID              uint32
	Codename                        string
	ConditionType, Minimum, Maximum uint8
	Option, Data                    uint32
	SourceOffset                    uint64
}

func loadSummonTactics(evidence string, supplement []byte) SummonTacticsTable {
	out := SummonTacticsTable{}
	for line, text := range strings.Split(evidence, "\n") {
		text = strings.TrimSpace(text)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		cols := strings.Split(text, "\t")
		if len(cols) != 5 {
			panic("invalid summon tactics evidence")
		}
		group := 1
		if mustEvidenceBool(cols[1], line+1) {
			group = 0
		}
		rows := out[cols[0]]
		rows[group] = append(rows[group], SummonTactics{
			TargetPolicy: uint8(mustEvidenceUint(cols[4], 8, line+1)),
			SightRange:   mustEvidenceFloat(cols[2], line+1),
			NativeFlags:  uint32(mustEvidenceUint(cols[3], 32, line+1)),
		})
		out[cols[0]] = rows
	}
	loadSupplementalSummonTactics(out, supplement)
	return out
}

func (t SummonTacticsTable) SummonedSightRange(ref MonsterRef, grade uint8, random func() float64) (float64, bool) {
	row, ok := t.Resolve(ref, grade, random)
	return row.SightRange, ok
}

// ResolveSummonTactics selects one whole record, consuming at most one draw.
func (t SummonTacticsTable) Resolve(ref MonsterRef, grade uint8, random func() float64) (SummonTactics, bool) {
	rows, ok := t[ref.Codename]
	if !ok {
		rows, ok = t[ref.OriginalCodename]
	}
	if !ok {
		return SummonTactics{}, false
	}
	group := 0
	if grade&15 != 0 {
		group = 1
	}
	candidates := rows[group]
	if len(candidates) == 0 {
		candidates = rows[1-group]
	}
	if len(candidates) == 0 {
		return SummonTactics{}, false
	}
	index := uint32(0)
	if len(candidates) > 1 {
		index = SummonRandomWord(random()) % uint32(len(candidates))
	}
	chosen := candidates[index]
	chosen.SightRange = float64(float32(chosen.SightRange + ref.BodyRadius))
	return chosen, true
}
